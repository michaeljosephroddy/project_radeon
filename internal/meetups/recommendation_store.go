package meetups

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (s *pgStore) discoverRecommendedMeetups(ctx context.Context, userID uuid.UUID, params DiscoverMeetupsParams, viewer viewerContext) (*CursorPage[Meetup], error) {
	ranked, err := s.rankRecommendedMeetups(ctx, userID, params, viewer, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	page := sliceRecommendedMeetups(ranked, params.Limit, params.Cursor)
	if len(page.Items) > 0 {
		s.decorateMeetups(ctx, page.Items, viewer)
	}
	return page, nil
}

func (s *pgStore) rankRecommendedMeetups(ctx context.Context, userID uuid.UUID, params DiscoverMeetupsParams, viewer viewerContext, now time.Time) ([]recommendedCandidate, error) {
	dateFrom, dateTo := resolveDateWindow(params)
	limits := recommendedCandidatePoolLimits(params.Limit)
	candidates, err := s.loadRecommendedCandidates(ctx, userID, params, viewer, dateFrom, dateTo, limits)
	if err != nil {
		return nil, err
	}
	features, err := s.loadRecommendationFeatures(ctx, userID, candidates)
	if err != nil {
		return nil, err
	}
	ranked := hydrateRecommendedCandidates(candidates, features, viewer)
	return rankRecommendedCandidates(ranked, viewer, now), nil
}

func (s *pgStore) loadRecommendedCandidates(ctx context.Context, userID uuid.UUID, params DiscoverMeetupsParams, viewer viewerContext, dateFrom, dateTo *time.Time, limits candidatePoolLimits) ([]Meetup, error) {
	orderedSources := []recommendedSource{
		recommendedSourceNearby,
		recommendedSourceInterest,
		recommendedSourceSocial,
		recommendedSourcePopular,
	}
	seen := make(map[uuid.UUID]int)
	candidates := make([]Meetup, 0, limits.Total)
	sourceHits := make(map[uuid.UUID]map[recommendedSource]struct{})

	for _, source := range orderedSources {
		sourceMeetups, err := s.loadRecommendedSource(ctx, userID, params, viewer, dateFrom, dateTo, source, limits.PerSource)
		if err != nil {
			return nil, err
		}
		for _, meetup := range sourceMeetups {
			if _, ok := sourceHits[meetup.ID]; !ok {
				sourceHits[meetup.ID] = map[recommendedSource]struct{}{}
			}
			sourceHits[meetup.ID][source] = struct{}{}
			if index, exists := seen[meetup.ID]; exists {
				if candidates[index].PublishedAt == nil && meetup.PublishedAt != nil {
					candidates[index] = meetup
				}
				continue
			}
			seen[meetup.ID] = len(candidates)
			candidates = append(candidates, meetup)
			if len(candidates) >= limits.Total {
				break
			}
		}
		if len(candidates) >= limits.Total {
			break
		}
	}

	for index := range candidates {
		if candidates[index].PublishedAt == nil {
			now := candidates[index].CreatedAt
			candidates[index].PublishedAt = &now
		}
		if hits, ok := sourceHits[candidates[index].ID]; ok {
			_ = hits
		}
	}
	return candidates, nil
}

func (s *pgStore) loadRecommendedSource(ctx context.Context, userID uuid.UUID, params DiscoverMeetupsParams, viewer viewerContext, dateFrom, dateTo *time.Time, source recommendedSource, limit int) ([]Meetup, error) {
	switch source {
	case recommendedSourceNearby:
		if viewer.Latitude == nil || viewer.Longitude == nil {
			return nil, nil
		}
		query, args := buildRecommendedMeetupQuery(userID)
		appendCommonRecommendedFilters(&query, &args, params, viewer, dateFrom, dateTo, true)
		appendDistanceBounding(&query, &args, viewer, preferredSourceDistanceKM(params))
		query += `
			AND m.event_type <> 'online'
			ORDER BY m.starts_at ASC, m.attendee_count DESC, m.id ASC
			LIMIT ` + appendQueryArg(&args, limit)
		rows, err := s.pool.Query(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanMeetupRows(rows)

	case recommendedSourceInterest:
		if len(viewer.InterestNames) == 0 {
			return nil, nil
		}
		query, args := buildRecommendedMeetupQuery(userID)
		appendCommonRecommendedFilters(&query, &args, params, viewer, dateFrom, dateTo, true)
		query += `
			AND LOWER(COALESCE(ec.label, '')) = ANY(` + appendQueryArg(&args, viewer.InterestNames) + `)
			ORDER BY m.starts_at ASC, m.attendee_count DESC, m.id ASC
			LIMIT ` + appendQueryArg(&args, limit)
		rows, err := s.pool.Query(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanMeetupRows(rows)

	case recommendedSourceSocial:
		query, args := buildRecommendedSocialMeetupQuery(userID)
		appendCommonRecommendedFilters(&query, &args, params, viewer, dateFrom, dateTo, true)
		query += `
			ORDER BY social.friend_count DESC, m.starts_at ASC, m.id ASC
			LIMIT ` + appendQueryArg(&args, limit)
		rows, err := s.pool.Query(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanMeetupRows(rows)

	default:
		query, args := buildRecommendedMeetupQuery(userID)
		appendCommonRecommendedFilters(&query, &args, params, viewer, dateFrom, dateTo, true)
		query += `
			ORDER BY m.attendee_count DESC, COALESCE(m.published_at, m.created_at) DESC, m.starts_at ASC, m.id ASC
			LIMIT ` + appendQueryArg(&args, limit)
		rows, err := s.pool.Query(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanMeetupRows(rows)
	}
}

func (s *pgStore) loadRecommendationFeatures(ctx context.Context, userID uuid.UUID, meetups []Meetup) (recommendationFeatureSet, error) {
	features := recommendationFeatureSet{
		FriendAttendeeCount: make(map[uuid.UUID]int, len(meetups)),
		OrganizerMetrics:    make(map[uuid.UUID]organizerRecommendationMetrics),
	}
	if len(meetups) == 0 {
		return features, nil
	}
	meetupIDs := make([]uuid.UUID, 0, len(meetups))
	organizerSeen := make(map[uuid.UUID]struct{}, len(meetups))
	organizerIDs := make([]uuid.UUID, 0, len(meetups))
	for _, meetup := range meetups {
		meetupIDs = append(meetupIDs, meetup.ID)
		if _, exists := organizerSeen[meetup.OrganizerID]; exists {
			continue
		}
		organizerSeen[meetup.OrganizerID] = struct{}{}
		organizerIDs = append(organizerIDs, meetup.OrganizerID)
	}

	friendRows, err := s.pool.Query(ctx, `
		SELECT ma.meetup_id, COUNT(*)::int AS friend_count
		FROM meetup_attendees ma
		JOIN friendships f
			ON f.status = 'accepted'
			AND (
				(f.user_a_id = $1 AND f.user_b_id = ma.user_id)
				OR (f.user_b_id = $1 AND f.user_a_id = ma.user_id)
			)
		WHERE ma.meetup_id = ANY($2)
		GROUP BY ma.meetup_id
	`, userID, meetupIDs)
	if err != nil {
		return features, err
	}
	for friendRows.Next() {
		var meetupID uuid.UUID
		var friendCount int
		if err := friendRows.Scan(&meetupID, &friendCount); err != nil {
			friendRows.Close()
			return features, err
		}
		features.FriendAttendeeCount[meetupID] = friendCount
	}
	if err := friendRows.Err(); err != nil {
		friendRows.Close()
		return features, err
	}
	friendRows.Close()

	metricRows, err := s.pool.Query(ctx, `
		SELECT
			organiser_id,
			COUNT(*) FILTER (WHERE status = 'published')::int AS published_count,
			COUNT(*) FILTER (WHERE status = 'cancelled')::int AS cancelled_count,
			COALESCE(AVG(attendee_count) FILTER (WHERE status = 'published'), 0)::double precision AS average_audience
		FROM meetups
		WHERE organiser_id = ANY($1)
		GROUP BY organiser_id
	`, organizerIDs)
	if err != nil {
		return features, err
	}
	for metricRows.Next() {
		var organizerID uuid.UUID
		var metrics organizerRecommendationMetrics
		if err := metricRows.Scan(&organizerID, &metrics.PublishedCount, &metrics.CancelledCount, &metrics.AverageAudience); err != nil {
			metricRows.Close()
			return features, err
		}
		features.OrganizerMetrics[organizerID] = metrics
	}
	if err := metricRows.Err(); err != nil {
		metricRows.Close()
		return features, err
	}
	metricRows.Close()
	return features, nil
}

func hydrateRecommendedCandidates(meetups []Meetup, features recommendationFeatureSet, viewer viewerContext) []recommendedCandidate {
	candidates := make([]recommendedCandidate, 0, len(meetups))
	for _, meetup := range meetups {
		candidate := recommendedCandidate{
			Meetup:          meetup,
			SourceHits:      map[recommendedSource]struct{}{recommendedSourcePopular: {}},
			Score:           0,
			InterestMatched: false,
		}
		if viewer.Latitude != nil && viewer.Longitude != nil && meetup.Latitude != nil && meetup.Longitude != nil {
			distance := haversineKM(*viewer.Latitude, *viewer.Longitude, *meetup.Latitude, *meetup.Longitude)
			candidate.DistanceKMComputed = &distance
		}
		if _, ok := viewer.Interests[strings.ToLower(meetup.CategoryLabel)]; ok {
			candidate.InterestMatched = true
		}
		if friendCount, ok := features.FriendAttendeeCount[meetup.ID]; ok {
			candidate.FriendAttendeeCount = friendCount
		}
		if metrics, ok := features.OrganizerMetrics[meetup.OrganizerID]; ok {
			candidate.OrganizerPublishedCount = metrics.PublishedCount
			candidate.OrganizerCancelledCount = metrics.CancelledCount
			candidate.OrganizerAverageAudience = metrics.AverageAudience
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func buildRecommendedMeetupQuery(userID uuid.UUID) (string, []any) {
	query := `
		SELECT
			m.id,
			m.organiser_id,
			u.username,
			u.avatar_url,
			m.title,
			m.description,
			COALESCE(m.category_slug, 'community') AS category_slug,
			COALESCE(ec.label, 'Community') AS category_label,
			m.event_type,
			m.status,
			m.visibility,
			COALESCE(m.city, '') AS city,
			m.country,
			m.venue_name,
			m.address_line_1,
			m.address_line_2,
			m.how_to_find_us,
			m.online_url,
			m.cover_image_url,
			m.starts_at,
			m.ends_at,
			COALESCE(m.timezone, 'UTC') AS timezone,
			m.lat,
			m.lng,
			m.capacity,
			m.attendee_count,
			m.waitlist_enabled,
			m.waitlist_count,
			m.saved_count,
			EXISTS(SELECT 1 FROM meetup_attendees ma WHERE ma.meetup_id = m.id AND ma.user_id = $1) AS is_attending,
			EXISTS(SELECT 1 FROM event_waitlist ew WHERE ew.meetup_id = m.id AND ew.user_id = $1) AS is_waitlisted,
			m.published_at,
			m.updated_at,
			m.created_at
		FROM meetups m
		JOIN users u ON u.id = m.organiser_id
		LEFT JOIN event_categories ec ON ec.slug = m.category_slug
		WHERE m.status = 'published'
			AND m.visibility = 'public'
			AND m.starts_at >= NOW()
	`
	return query, []any{userID}
}

func buildRecommendedSocialMeetupQuery(userID uuid.UUID) (string, []any) {
	query := `
		WITH social AS (
			SELECT ma.meetup_id, COUNT(*)::int AS friend_count
			FROM meetup_attendees ma
			JOIN friendships f
				ON f.status = 'accepted'
				AND (
					(f.user_a_id = $1 AND f.user_b_id = ma.user_id)
					OR (f.user_b_id = $1 AND f.user_a_id = ma.user_id)
				)
			GROUP BY ma.meetup_id
		)
		SELECT
			m.id,
			m.organiser_id,
			u.username,
			u.avatar_url,
			m.title,
			m.description,
			COALESCE(m.category_slug, 'community') AS category_slug,
			COALESCE(ec.label, 'Community') AS category_label,
			m.event_type,
			m.status,
			m.visibility,
			COALESCE(m.city, '') AS city,
			m.country,
			m.venue_name,
			m.address_line_1,
			m.address_line_2,
			m.how_to_find_us,
			m.online_url,
			m.cover_image_url,
			m.starts_at,
			m.ends_at,
			COALESCE(m.timezone, 'UTC') AS timezone,
			m.lat,
			m.lng,
			m.capacity,
			m.attendee_count,
			m.waitlist_enabled,
			m.waitlist_count,
			m.saved_count,
			EXISTS(SELECT 1 FROM meetup_attendees ma WHERE ma.meetup_id = m.id AND ma.user_id = $1) AS is_attending,
			EXISTS(SELECT 1 FROM event_waitlist ew WHERE ew.meetup_id = m.id AND ew.user_id = $1) AS is_waitlisted,
			m.published_at,
			m.updated_at,
			m.created_at
		FROM social
		JOIN meetups m ON m.id = social.meetup_id
		JOIN users u ON u.id = m.organiser_id
		LEFT JOIN event_categories ec ON ec.slug = m.category_slug
		WHERE m.status = 'published'
			AND m.visibility = 'public'
			AND m.starts_at >= NOW()
	`
	return query, []any{userID}
}

func appendCommonRecommendedFilters(query *string, args *[]any, params DiscoverMeetupsParams, viewer viewerContext, dateFrom, dateTo *time.Time, applyDistance bool) {
	if params.Query != "" {
		like := "%" + params.Query + "%"
		placeholder := appendQueryArg(args, like)
		*query += fmt.Sprintf(`
			AND (
				m.title ILIKE %s
				OR COALESCE(m.description, '') ILIKE %s
				OR COALESCE(m.venue_name, '') ILIKE %s
				OR COALESCE(m.city, '') ILIKE %s
			)
		`, placeholder, placeholder, placeholder, placeholder)
	}
	if params.CategorySlug != "" {
		*query += " AND COALESCE(m.category_slug, 'community') = " + appendQueryArg(args, params.CategorySlug)
	}
	if params.EventType != "" {
		*query += " AND m.event_type = " + appendQueryArg(args, params.EventType)
	}
	if params.City != "" {
		like := "%" + params.City + "%"
		placeholder := appendQueryArg(args, like)
		*query += fmt.Sprintf(`
			AND (
				COALESCE(m.city, '') ILIKE %s
				OR COALESCE(m.country, '') ILIKE %s
				OR COALESCE(m.venue_name, '') ILIKE %s
			)
		`, placeholder, placeholder, placeholder)
	}
	if params.OpenSpotsOnly {
		*query += " AND (m.capacity IS NULL OR m.attendee_count < m.capacity)"
	}
	if dateFrom != nil {
		*query += " AND m.starts_at >= " + appendQueryArg(args, *dateFrom)
	}
	if dateTo != nil {
		*query += " AND m.starts_at < " + appendQueryArg(args, *dateTo)
	}
	if len(params.DayOfWeek) > 0 {
		*query += " AND EXTRACT(DOW FROM m.starts_at)::int = ANY(" + appendQueryArg(args, params.DayOfWeek) + ")"
	}
	if len(params.TimeOfDay) > 0 {
		clauses := make([]string, 0, len(params.TimeOfDay))
		for _, bucket := range params.TimeOfDay {
			switch bucket {
			case "morning":
				clauses = append(clauses, "(EXTRACT(HOUR FROM m.starts_at) >= 5 AND EXTRACT(HOUR FROM m.starts_at) < 12)")
			case "afternoon":
				clauses = append(clauses, "(EXTRACT(HOUR FROM m.starts_at) >= 12 AND EXTRACT(HOUR FROM m.starts_at) < 17)")
			case "evening":
				clauses = append(clauses, "(EXTRACT(HOUR FROM m.starts_at) >= 17 AND EXTRACT(HOUR FROM m.starts_at) < 22)")
			case "night":
				clauses = append(clauses, "(EXTRACT(HOUR FROM m.starts_at) >= 22 OR EXTRACT(HOUR FROM m.starts_at) < 5)")
			}
		}
		if len(clauses) > 0 {
			*query += " AND (" + strings.Join(clauses, " OR ") + ")"
		}
	}
	if applyDistance && params.DistanceKM != nil {
		appendDistanceBounding(query, args, viewer, *params.DistanceKM)
	}
}

func appendDistanceBounding(query *string, args *[]any, viewer viewerContext, distanceKM int) {
	if viewer.Latitude == nil || viewer.Longitude == nil || distanceKM <= 0 {
		return
	}
	latDelta := float64(distanceKM) / 111.0
	cosine := math.Cos(radians(*viewer.Latitude))
	if math.Abs(cosine) < 0.1 {
		cosine = 0.1
	}
	lngDelta := float64(distanceKM) / (111.0 * math.Abs(cosine))
	latMin := *viewer.Latitude - latDelta
	latMax := *viewer.Latitude + latDelta
	lngMin := *viewer.Longitude - lngDelta
	lngMax := *viewer.Longitude + lngDelta
	*query += `
		AND (
			m.event_type = 'online'
			OR (
				m.lat IS NOT NULL
				AND m.lng IS NOT NULL
				AND m.lat BETWEEN ` + appendQueryArg(args, latMin) + ` AND ` + appendQueryArg(args, latMax) + `
				AND m.lng BETWEEN ` + appendQueryArg(args, lngMin) + ` AND ` + appendQueryArg(args, lngMax) + `
			)
		)
	`
}

func appendQueryArg(args *[]any, value any) string {
	*args = append(*args, value)
	return fmt.Sprintf("$%d", len(*args))
}

func preferredSourceDistanceKM(params DiscoverMeetupsParams) int {
	if params.DistanceKM != nil && *params.DistanceKM > 0 {
		return *params.DistanceKM
	}
	return 80
}
