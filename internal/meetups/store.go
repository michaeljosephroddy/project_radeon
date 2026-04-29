package meetups

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrForbidden         = errors.New("forbidden")
	ErrCapacityReached   = errors.New("capacity reached")
	ErrDeleteNotAllowed  = errors.New("delete not allowed")
	ErrInvalidTransition = errors.New("invalid transition")
)

type pgStore struct {
	pool                  *pgxpool.Pool
	recommendedPipelineV2 bool
}

type viewerContext struct {
	UserID        uuid.UUID
	Latitude      *float64
	Longitude     *float64
	Interests     map[string]struct{}
	InterestNames []string
}

func NewPgStore(pool *pgxpool.Pool) Querier {
	return NewPgStoreWithConfig(pool, StoreConfig{RecommendedPipelineV2: true})
}

type StoreConfig struct {
	RecommendedPipelineV2 bool
}

func NewPgStoreWithConfig(pool *pgxpool.Pool, cfg StoreConfig) Querier {
	return &pgStore{pool: pool, recommendedPipelineV2: cfg.RecommendedPipelineV2}
}

func (s *pgStore) ListCategories(ctx context.Context) ([]MeetupCategory, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT slug, label, sort_order
		FROM event_categories
		ORDER BY sort_order ASC, label ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []MeetupCategory
	for rows.Next() {
		var category MeetupCategory
		if err := rows.Scan(&category.Slug, &category.Label, &category.SortOrder); err != nil {
			return nil, err
		}
		categories = append(categories, category)
	}
	return categories, rows.Err()
}

func (s *pgStore) DiscoverMeetups(ctx context.Context, userID uuid.UUID, params DiscoverMeetupsParams) (*CursorPage[Meetup], error) {
	viewer, err := s.loadViewerContext(ctx, userID)
	if err != nil {
		return nil, err
	}
	if s.recommendedPipelineV2 && params.Sort == "recommended" {
		return s.discoverRecommendedMeetups(ctx, userID, params, viewer)
	}
	dateFrom, dateTo := resolveDateWindow(params)
	offset := decodeCursorToOffset(params.Cursor)
	meetups, err := s.loadDiscoverMeetups(ctx, userID, params, viewer, dateFrom, dateTo, offset)
	if err != nil {
		return nil, err
	}
	page := sliceLoadedMeetups(meetups, params.Limit, offset)
	s.decorateMeetups(ctx, page.Items, viewer)
	return page, nil
}

func (s *pgStore) ListMyMeetups(ctx context.Context, userID uuid.UUID, params MyMeetupsParams) (*CursorPage[Meetup], error) {
	viewer, err := s.loadViewerContext(ctx, userID)
	if err != nil {
		return nil, err
	}
	offset := decodeCursorToOffset(params.Cursor)
	meetups, err := s.loadMyMeetups(ctx, userID, params.Scope, params.Limit, offset)
	if err != nil {
		return nil, err
	}
	page := sliceLoadedMeetups(meetups, params.Limit, offset)
	s.decorateMeetups(ctx, page.Items, viewer)
	return page, nil
}

func (s *pgStore) GetMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error) {
	viewer, err := s.loadViewerContext(ctx, userID)
	if err != nil {
		return nil, err
	}

	meetup, err := s.loadMeetupByID(ctx, meetupID, userID)
	if err != nil {
		return nil, err
	}
	meetups := []Meetup{*meetup}
	meetups[0].CanManage = meetups[0].OrganizerID == viewer.UserID
	if viewer.Latitude != nil && viewer.Longitude != nil && meetups[0].Latitude != nil && meetups[0].Longitude != nil {
		distance := haversineKM(*viewer.Latitude, *viewer.Longitude, *meetups[0].Latitude, *meetups[0].Longitude)
		meetups[0].DistanceKM = &distance
	}
	if err := s.attachAttendeePreviews(ctx, meetups, 6); err != nil {
		return nil, err
	}
	if err := s.attachHosts(ctx, meetups); err != nil {
		return nil, err
	}
	*meetup = meetups[0]
	return meetup, nil
}

func (s *pgStore) CreateMeetup(ctx context.Context, userID uuid.UUID, input CreateMeetupInput) (*Meetup, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
	var meetup Meetup
	publishedAt := (*time.Time)(nil)
	if input.Status == "published" {
		publishedAt = &now
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO meetups (
			organiser_id, title, description, category_slug, event_type, status, visibility,
			city, country, venue_name, address_line_1, address_line_2, how_to_find_us,
			online_url, cover_image_url, starts_at, ends_at, timezone, lat, lng,
			capacity, waitlist_enabled, published_at, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19, $20,
			$21, $22, $23, $24, $24
		)
		RETURNING id`,
		userID, input.Title, input.Description, input.CategorySlug, input.EventType, input.Status, input.Visibility,
		input.City, input.Country, input.VenueName, input.AddressLine1, input.AddressLine2, input.HowToFindUs,
		input.OnlineURL, input.CoverImageURL, input.StartsAt, input.EndsAt, input.Timezone, input.Latitude, input.Longitude,
		input.Capacity, input.WaitlistEnabled, publishedAt, now,
	).Scan(&meetup.ID); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO event_hosts (meetup_id, user_id, role)
		VALUES ($1, $2, 'organizer')
		ON CONFLICT (meetup_id, user_id) DO NOTHING
	`, meetup.ID, userID); err != nil {
		return nil, err
	}
	if err := syncMeetupHosts(ctx, tx, meetup.ID, userID, input.CoHostIDs); err != nil {
		return nil, err
	}

	if input.Status == "published" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO meetup_attendees (meetup_id, user_id, rsvp_at)
			VALUES ($1, $2, $3)
			ON CONFLICT (meetup_id, user_id) DO NOTHING
		`, meetup.ID, userID, now); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE meetups SET attendee_count = 1 WHERE id = $1`, meetup.ID); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetMeetup(ctx, meetup.ID, userID)
}

func (s *pgStore) UpdateMeetup(ctx context.Context, meetupID, userID uuid.UUID, input UpdateMeetupInput) (*Meetup, error) {
	if err := s.ensureManagePermission(ctx, meetupID, userID); err != nil {
		return nil, err
	}
	currentStatus, err := s.loadManagedMeetupStatus(ctx, meetupID, userID)
	if err != nil {
		return nil, err
	}
	if currentStatus == "published" && input.Status != "published" {
		return nil, ErrInvalidTransition
	}
	if currentStatus != "draft" && currentStatus != "published" {
		return nil, ErrForbidden
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE meetups
		SET title = $3,
			description = $4,
			category_slug = $5,
			event_type = $6,
			status = $7,
			visibility = $8,
			city = $9,
			country = $10,
			venue_name = $11,
			address_line_1 = $12,
			address_line_2 = $13,
			how_to_find_us = $14,
			online_url = $15,
			cover_image_url = $16,
			starts_at = $17,
			ends_at = $18,
			timezone = $19,
			lat = $20,
			lng = $21,
			capacity = $22,
			waitlist_enabled = $23,
			updated_at = NOW()
		WHERE id = $1 AND organiser_id = $2
	`, meetupID, userID, input.Title, input.Description, input.CategorySlug, input.EventType, input.Status, input.Visibility,
		input.City, input.Country, input.VenueName, input.AddressLine1, input.AddressLine2, input.HowToFindUs,
		input.OnlineURL, input.CoverImageURL, input.StartsAt, input.EndsAt, input.Timezone, input.Latitude, input.Longitude,
		input.Capacity, input.WaitlistEnabled,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	if err := syncMeetupHosts(ctx, tx, meetupID, userID, input.CoHostIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetMeetup(ctx, meetupID, userID)
}

func (s *pgStore) PublishMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error) {
	if err := s.ensureManagePermission(ctx, meetupID, userID); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE meetups
		SET status = 'published',
			published_at = COALESCE(published_at, NOW()),
			updated_at = NOW()
		WHERE id = $1 AND organiser_id = $2
	`, meetupID, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO meetup_attendees (meetup_id, user_id, rsvp_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (meetup_id, user_id) DO NOTHING
	`, meetupID, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE meetups
		SET attendee_count = (SELECT COUNT(*) FROM meetup_attendees WHERE meetup_id = $1)
		WHERE id = $1
	`, meetupID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetMeetup(ctx, meetupID, userID)
}

func (s *pgStore) CancelMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error) {
	if err := s.ensureManagePermission(ctx, meetupID, userID); err != nil {
		return nil, err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE meetups
		SET status = 'cancelled',
			updated_at = NOW()
		WHERE id = $1 AND organiser_id = $2
	`, meetupID, userID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.GetMeetup(ctx, meetupID, userID)
}

func (s *pgStore) DeleteMeetup(ctx context.Context, meetupID, userID uuid.UUID) error {
	if err := s.ensureManagePermission(ctx, meetupID, userID); err != nil {
		return err
	}

	var status string
	var attendeeCount int
	err := s.pool.QueryRow(ctx, `
		SELECT status, attendee_count
		FROM meetups
		WHERE id = $1 AND organiser_id = $2
	`, meetupID, userID).Scan(&status, &attendeeCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "draft" && attendeeCount > 1 {
		return ErrDeleteNotAllowed
	}

	tag, err := s.pool.Exec(ctx, `DELETE FROM meetups WHERE id = $1 AND organiser_id = $2`, meetupID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgStore) GetAttendees(ctx context.Context, meetupID uuid.UUID, limit, offset int) ([]Attendee, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.username, u.avatar_url, u.city, ma.rsvp_at
		FROM meetup_attendees ma
		JOIN users u ON u.id = ma.user_id
		WHERE ma.meetup_id = $1
		ORDER BY ma.rsvp_at ASC, u.username ASC
		LIMIT $2 OFFSET $3
	`, meetupID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attendees []Attendee
	for rows.Next() {
		var attendee Attendee
		if err := rows.Scan(&attendee.ID, &attendee.Username, &attendee.AvatarURL, &attendee.City, &attendee.RSVPAt); err != nil {
			return nil, err
		}
		attendees = append(attendees, attendee)
	}
	return attendees, rows.Err()
}

func (s *pgStore) GetWaitlist(ctx context.Context, meetupID, userID uuid.UUID, limit, offset int) ([]Attendee, error) {
	if err := s.ensureManagePermission(ctx, meetupID, userID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.username, u.avatar_url, u.city, ew.joined_at
		FROM event_waitlist ew
		JOIN users u ON u.id = ew.user_id
		WHERE ew.meetup_id = $1
		ORDER BY ew.joined_at ASC, u.username ASC
		LIMIT $2 OFFSET $3
	`, meetupID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var waitlist []Attendee
	for rows.Next() {
		var attendee Attendee
		if err := rows.Scan(&attendee.ID, &attendee.Username, &attendee.AvatarURL, &attendee.City, &attendee.RSVPAt); err != nil {
			return nil, err
		}
		waitlist = append(waitlist, attendee)
	}
	return waitlist, rows.Err()
}

func (s *pgStore) ToggleRSVP(ctx context.Context, meetupID, userID uuid.UUID) (*RSVPResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var organiserID uuid.UUID
	var status string
	var capacity *int
	var attendeeCount int
	var waitlistEnabled bool
	var waitlistCount int
	err = tx.QueryRow(ctx, `
		SELECT organiser_id, status, capacity, attendee_count, waitlist_enabled, waitlist_count
		FROM meetups
		WHERE id = $1
	`, meetupID).Scan(&organiserID, &status, &capacity, &attendeeCount, &waitlistEnabled, &waitlistCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if organiserID == userID {
		return nil, ErrForbidden
	}
	if status != "published" {
		return nil, ErrForbidden
	}

	var isAttending bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM meetup_attendees WHERE meetup_id = $1 AND user_id = $2)
	`, meetupID, userID).Scan(&isAttending); err != nil {
		return nil, err
	}

	var isWaitlisted bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM event_waitlist WHERE meetup_id = $1 AND user_id = $2)
	`, meetupID, userID).Scan(&isWaitlisted); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	result := &RSVPResult{}
	switch {
	case isAttending:
		if _, err := tx.Exec(ctx, `DELETE FROM meetup_attendees WHERE meetup_id = $1 AND user_id = $2`, meetupID, userID); err != nil {
			return nil, err
		}
		attendeeCount = maxInt(attendeeCount-1, 0)

		if waitlistEnabled {
			var promoteUserID uuid.UUID
			err := tx.QueryRow(ctx, `
				SELECT user_id
				FROM event_waitlist
				WHERE meetup_id = $1
				ORDER BY joined_at ASC
				LIMIT 1
			`, meetupID).Scan(&promoteUserID)
			if err == nil {
				if _, err := tx.Exec(ctx, `DELETE FROM event_waitlist WHERE meetup_id = $1 AND user_id = $2`, meetupID, promoteUserID); err != nil {
					return nil, err
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO meetup_attendees (meetup_id, user_id, rsvp_at)
					VALUES ($1, $2, $3)
					ON CONFLICT (meetup_id, user_id) DO NOTHING
				`, meetupID, promoteUserID, now); err != nil {
					return nil, err
				}
				waitlistCount = maxInt(waitlistCount-1, 0)
				attendeeCount++
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
		}

		result.State = "none"
		result.Attending = false
		result.Waitlisted = false

	case isWaitlisted:
		if _, err := tx.Exec(ctx, `DELETE FROM event_waitlist WHERE meetup_id = $1 AND user_id = $2`, meetupID, userID); err != nil {
			return nil, err
		}
		waitlistCount = maxInt(waitlistCount-1, 0)
		result.State = "none"
		result.Attending = false
		result.Waitlisted = false

	default:
		hasCapacity := capacity == nil || attendeeCount < *capacity
		if hasCapacity {
			if _, err := tx.Exec(ctx, `
				INSERT INTO meetup_attendees (meetup_id, user_id, rsvp_at)
				VALUES ($1, $2, $3)
				ON CONFLICT (meetup_id, user_id) DO NOTHING
			`, meetupID, userID, now); err != nil {
				return nil, err
			}
			attendeeCount++
			result.State = "going"
			result.Attending = true
			result.Waitlisted = false
		} else if waitlistEnabled {
			if _, err := tx.Exec(ctx, `
				INSERT INTO event_waitlist (meetup_id, user_id, joined_at)
				VALUES ($1, $2, $3)
				ON CONFLICT (meetup_id, user_id) DO NOTHING
			`, meetupID, userID, now); err != nil {
				return nil, err
			}
			waitlistCount++
			result.State = "waitlisted"
			result.Attending = false
			result.Waitlisted = true
		} else {
			return nil, ErrCapacityReached
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE meetups
		SET attendee_count = $2,
			waitlist_count = $3,
			updated_at = NOW()
		WHERE id = $1
	`, meetupID, attendeeCount, waitlistCount); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	result.AttendeeCount = attendeeCount
	result.WaitlistCount = waitlistCount
	return result, nil
}

func (s *pgStore) ensureManagePermission(ctx context.Context, meetupID, userID uuid.UUID) error {
	var organiserID uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT organiser_id FROM meetups WHERE id = $1`, meetupID).Scan(&organiserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if organiserID != userID {
		return ErrForbidden
	}
	return nil
}

func (s *pgStore) loadManagedMeetupStatus(ctx context.Context, meetupID, userID uuid.UUID) (string, error) {
	var status string
	err := s.pool.QueryRow(ctx, `
		SELECT status
		FROM meetups
		WHERE id = $1 AND organiser_id = $2
	`, meetupID, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return status, nil
}

func (s *pgStore) loadViewerContext(ctx context.Context, userID uuid.UUID) (viewerContext, error) {
	viewer := viewerContext{UserID: userID, Interests: map[string]struct{}{}}
	var lat, lng *float64
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(current_lat, lat), COALESCE(current_lng, lng)
		FROM users
		WHERE id = $1
	`, userID).Scan(&lat, &lng); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return viewer, err
	}
	viewer.Latitude = lat
	viewer.Longitude = lng

	rows, err := s.pool.Query(ctx, `
		SELECT LOWER(i.name)
		FROM user_interests ui
		JOIN interests i ON i.id = ui.interest_id
		WHERE ui.user_id = $1
	`, userID)
	if err != nil {
		return viewer, err
	}
	defer rows.Close()
	for rows.Next() {
		var interest string
		if err := rows.Scan(&interest); err != nil {
			return viewer, err
		}
		viewer.Interests[interest] = struct{}{}
	}
	viewer.InterestNames = sortedInterestNames(viewer.Interests)
	return viewer, rows.Err()
}

type meetupHostExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func syncMeetupHosts(ctx context.Context, execer meetupHostExecutor, meetupID, organizerID uuid.UUID, coHostIDs []uuid.UUID) error {
	if _, err := execer.Exec(ctx, `
		DELETE FROM event_hosts
		WHERE meetup_id = $1
			AND user_id <> $2
	`, meetupID, organizerID); err != nil {
		return err
	}

	for _, hostID := range coHostIDs {
		if hostID == organizerID {
			continue
		}
		if _, err := execer.Exec(ctx, `
			INSERT INTO event_hosts (meetup_id, user_id, role)
			VALUES ($1, $2, 'co_host')
			ON CONFLICT (meetup_id, user_id)
			DO UPDATE SET role = 'co_host'
		`, meetupID, hostID); err != nil {
			return err
		}
	}
	return nil
}

func (s *pgStore) loadDiscoverMeetups(ctx context.Context, userID uuid.UUID, params DiscoverMeetupsParams, viewer viewerContext, dateFrom, dateTo *time.Time, offset int) ([]Meetup, error) {
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
	args := []any{userID}
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	distanceExpr := ""
	if viewer.Latitude != nil && viewer.Longitude != nil {
		latArg := arg(*viewer.Latitude)
		lngArg := arg(*viewer.Longitude)
		distanceExpr = fmt.Sprintf(`(
			2.0 * 6371.0 * ASIN(SQRT(
				POWER(SIN(RADIANS((m.lat - %s::float8) / 2.0)), 2)
				+ COS(RADIANS(%s::float8)) * COS(RADIANS(m.lat))
				* POWER(SIN(RADIANS((m.lng - %s::float8) / 2.0)), 2)
			))
		)`, latArg, latArg, lngArg)
	}
	if params.Query != "" {
		like := "%" + params.Query + "%"
		placeholder := arg(like)
		query += fmt.Sprintf(`
			AND (
				m.title ILIKE %s
				OR COALESCE(m.description, '') ILIKE %s
				OR COALESCE(m.venue_name, '') ILIKE %s
				OR COALESCE(m.city, '') ILIKE %s
			)
		`, placeholder, placeholder, placeholder, placeholder)
	}
	if params.CategorySlug != "" {
		query += fmt.Sprintf(" AND COALESCE(m.category_slug, 'community') = %s", arg(params.CategorySlug))
	}
	if params.EventType != "" {
		query += fmt.Sprintf(" AND m.event_type = %s", arg(params.EventType))
	}
	if params.City != "" {
		like := "%" + params.City + "%"
		placeholder := arg(like)
		query += fmt.Sprintf(`
			AND (
				COALESCE(m.city, '') ILIKE %s
				OR COALESCE(m.country, '') ILIKE %s
				OR COALESCE(m.venue_name, '') ILIKE %s
			)
		`, placeholder, placeholder, placeholder)
	}
	if params.OpenSpotsOnly {
		query += " AND (m.capacity IS NULL OR m.attendee_count < m.capacity)"
	}
	if params.DistanceKM != nil {
		if distanceExpr == "" {
			query += " AND FALSE"
		} else {
			query += fmt.Sprintf(
				" AND m.lat IS NOT NULL AND m.lng IS NOT NULL AND %s <= %s::float8",
				distanceExpr,
				arg(float64(*params.DistanceKM)),
			)
		}
	}
	if dateFrom != nil {
		query += fmt.Sprintf(" AND m.starts_at >= %s", arg(*dateFrom))
	}
	if dateTo != nil {
		query += fmt.Sprintf(" AND m.starts_at < %s", arg(*dateTo))
	}
	if len(params.DayOfWeek) > 0 {
		query += fmt.Sprintf(" AND EXTRACT(DOW FROM m.starts_at)::int = ANY(%s)", arg(params.DayOfWeek))
	}
	if len(params.TimeOfDay) > 0 {
		var clauses []string
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
			query += " AND (" + strings.Join(clauses, " OR ") + ")"
		}
	}
	query += meetupDiscoverOrderBy(params.Sort, distanceExpr)
	query += fmt.Sprintf(" LIMIT %s OFFSET %s", arg(params.Limit+1), arg(offset))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMeetupRows(rows)
}

func (s *pgStore) loadMyMeetups(ctx context.Context, userID uuid.UUID, scope string, limit, offset int) ([]Meetup, error) {
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
		WHERE `
	switch scope {
	case "going":
		query += `
			EXISTS (SELECT 1 FROM meetup_attendees ma WHERE ma.meetup_id = m.id AND ma.user_id = $1)
			AND m.organiser_id <> $1
			AND m.status = 'published'
			AND m.starts_at >= NOW()
		`
	case "drafts":
		query += "m.organiser_id = $1 AND m.status = 'draft'"
	case "cancelled":
		query += `
			m.organiser_id = $1
			AND m.status = 'cancelled'
		`
	case "past":
		query += `
			(
				m.organiser_id = $1
				OR EXISTS (SELECT 1 FROM meetup_attendees ma WHERE ma.meetup_id = m.id AND ma.user_id = $1)
			)
			AND (
				m.starts_at < NOW()
				OR m.status IN ('cancelled', 'completed')
			)
		`
	default:
		query += `
				m.organiser_id = $1
				AND m.status = 'published'
				AND m.starts_at >= NOW()
			`
	}
	query += myMeetupsOrderBy(scope)
	query += ` LIMIT $2 OFFSET $3`

	rows, err := s.pool.Query(ctx, query, userID, limit+1, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMeetupRows(rows)
}

func (s *pgStore) loadMeetupByID(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error) {
	rows, err := s.pool.Query(ctx, `
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
			EXISTS(SELECT 1 FROM meetup_attendees ma WHERE ma.meetup_id = m.id AND ma.user_id = $2) AS is_attending,
			EXISTS(SELECT 1 FROM event_waitlist ew WHERE ew.meetup_id = m.id AND ew.user_id = $2) AS is_waitlisted,
			m.published_at,
			m.updated_at,
			m.created_at
		FROM meetups m
		JOIN users u ON u.id = m.organiser_id
		LEFT JOIN event_categories ec ON ec.slug = m.category_slug
		WHERE m.id = $1
	`, meetupID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	meetups, err := scanMeetupRows(rows)
	if err != nil {
		return nil, err
	}
	if len(meetups) == 0 {
		return nil, ErrNotFound
	}
	return &meetups[0], nil
}

func scanMeetupRows(rows pgx.Rows) ([]Meetup, error) {
	var meetups []Meetup
	for rows.Next() {
		var meetup Meetup
		if err := rows.Scan(
			&meetup.ID,
			&meetup.OrganizerID,
			&meetup.OrganizerUsername,
			&meetup.OrganizerAvatar,
			&meetup.Title,
			&meetup.Description,
			&meetup.CategorySlug,
			&meetup.CategoryLabel,
			&meetup.EventType,
			&meetup.Status,
			&meetup.Visibility,
			&meetup.City,
			&meetup.Country,
			&meetup.VenueName,
			&meetup.AddressLine1,
			&meetup.AddressLine2,
			&meetup.HowToFindUs,
			&meetup.OnlineURL,
			&meetup.CoverImageURL,
			&meetup.StartsAt,
			&meetup.EndsAt,
			&meetup.Timezone,
			&meetup.Latitude,
			&meetup.Longitude,
			&meetup.Capacity,
			&meetup.AttendeeCt,
			&meetup.WaitlistEnabled,
			&meetup.WaitlistCount,
			&meetup.SavedCount,
			&meetup.IsAttending,
			&meetup.IsWaitlisted,
			&meetup.PublishedAt,
			&meetup.UpdatedAt,
			&meetup.CreatedAt,
		); err != nil {
			return nil, err
		}
		meetups = append(meetups, meetup)
	}
	return meetups, rows.Err()
}

func (s *pgStore) decorateMeetups(ctx context.Context, meetups []Meetup, viewer viewerContext) {
	if len(meetups) == 0 {
		return
	}
	_ = s.attachAttendeePreviews(ctx, meetups, 3)
	_ = s.attachHosts(ctx, meetups)
	for index := range meetups {
		meetups[index].CanManage = meetups[index].OrganizerID == viewer.UserID
		if viewer.Latitude != nil && viewer.Longitude != nil && meetups[index].Latitude != nil && meetups[index].Longitude != nil {
			distance := haversineKM(*viewer.Latitude, *viewer.Longitude, *meetups[index].Latitude, *meetups[index].Longitude)
			meetups[index].DistanceKM = &distance
		}
	}
}

func (s *pgStore) attachAttendeePreviews(ctx context.Context, meetups []Meetup, previewLimit int) error {
	if len(meetups) == 0 {
		return nil
	}
	meetupIDs := make([]uuid.UUID, 0, len(meetups))
	lookup := make(map[uuid.UUID]*Meetup, len(meetups))
	for i := range meetups {
		meetupIDs = append(meetupIDs, meetups[i].ID)
		lookup[meetups[i].ID] = &meetups[i]
	}

	rows, err := s.pool.Query(ctx, `
		SELECT meetup_id, id, username, avatar_url
		FROM (
			SELECT
				ma.meetup_id,
				u.id,
				u.username,
				u.avatar_url,
				ROW_NUMBER() OVER (PARTITION BY ma.meetup_id ORDER BY ma.rsvp_at ASC, u.username ASC) AS rn
			FROM meetup_attendees ma
			JOIN users u ON u.id = ma.user_id
			WHERE ma.meetup_id = ANY($1)
		) preview
		WHERE rn <= $2
		ORDER BY meetup_id, rn
	`, meetupIDs, previewLimit)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var meetupID uuid.UUID
		var preview AttendeePreview
		if err := rows.Scan(&meetupID, &preview.ID, &preview.Username, &preview.AvatarURL); err != nil {
			return err
		}
		if meetup := lookup[meetupID]; meetup != nil {
			meetup.Attendees = append(meetup.Attendees, preview)
		}
	}
	return rows.Err()
}

func (s *pgStore) attachHosts(ctx context.Context, meetups []Meetup) error {
	if len(meetups) == 0 {
		return nil
	}
	meetupIDs := make([]uuid.UUID, 0, len(meetups))
	lookup := make(map[uuid.UUID]*Meetup, len(meetups))
	for i := range meetups {
		meetupIDs = append(meetupIDs, meetups[i].ID)
		lookup[meetups[i].ID] = &meetups[i]
	}
	rows, err := s.pool.Query(ctx, `
		SELECT eh.meetup_id, u.id, u.username, u.avatar_url, eh.role
		FROM event_hosts eh
		JOIN users u ON u.id = eh.user_id
		WHERE eh.meetup_id = ANY($1)
		ORDER BY eh.role ASC, u.username ASC
	`, meetupIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var meetupID uuid.UUID
		var host MeetupHost
		if err := rows.Scan(&meetupID, &host.ID, &host.Username, &host.AvatarURL, &host.Role); err != nil {
			return err
		}
		if meetup := lookup[meetupID]; meetup != nil {
			meetup.Hosts = append(meetup.Hosts, host)
		}
	}
	return rows.Err()
}

func resolveDateWindow(params DiscoverMeetupsParams) (*time.Time, *time.Time) {
	now := time.Now().UTC()
	if params.DatePreset == "custom" {
		return normalizeDateStart(params.DateFrom), normalizeDateEnd(params.DateTo)
	}
	switch params.DatePreset {
	case "today":
		start := dateFloor(now)
		end := start.Add(24 * time.Hour)
		return &start, &end
	case "tomorrow":
		start := dateFloor(now).Add(24 * time.Hour)
		end := start.Add(24 * time.Hour)
		return &start, &end
	case "this_week":
		start := dateFloor(now)
		end := start.Add(7 * 24 * time.Hour)
		return &start, &end
	case "this_weekend":
		start := nextWeekday(dateFloor(now), time.Saturday)
		end := start.Add(48 * time.Hour)
		return &start, &end
	default:
		return normalizeDateStart(params.DateFrom), normalizeDateEnd(params.DateTo)
	}
}

func normalizeDateStart(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	start := dateFloor(value.UTC())
	return &start
}

func normalizeDateEnd(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	end := dateFloor(value.UTC()).Add(24 * time.Hour)
	return &end
}

func dateFloor(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func nextWeekday(start time.Time, weekday time.Weekday) time.Time {
	value := start
	for value.Weekday() != weekday {
		value = value.Add(24 * time.Hour)
	}
	return value
}

func filterMeetups(meetups []Meetup, params DiscoverMeetupsParams, viewer viewerContext, includeNonPublic bool) []Meetup {
	filtered := make([]Meetup, 0, len(meetups))
	for _, meetup := range meetups {
		if !includeNonPublic && meetup.Visibility != "public" {
			continue
		}
		if params.DistanceKM != nil {
			if meetup.DistanceKM == nil || *meetup.DistanceKM > float64(*params.DistanceKM) {
				continue
			}
		}
		filtered = append(filtered, meetup)
	}
	return filtered
}

func meetupDiscoverOrderBy(sortKey, distanceExpr string) string {
	switch sortKey {
	case "distance":
		if distanceExpr == "" {
			return " ORDER BY m.starts_at ASC, m.title ASC, m.id ASC"
		}
		return " ORDER BY " + distanceExpr + " ASC, m.starts_at ASC, m.id ASC"
	case "popular":
		return " ORDER BY m.attendee_count DESC, m.starts_at ASC, m.id ASC"
	case "newest":
		return " ORDER BY COALESCE(m.published_at, m.created_at) DESC, m.starts_at ASC, m.id ASC"
	default:
		return " ORDER BY m.starts_at ASC, m.title ASC, m.id ASC"
	}
}

func myMeetupsOrderBy(scope string) string {
	if scope == "past" {
		return " ORDER BY m.starts_at DESC, m.title ASC, m.id ASC"
	}
	return " ORDER BY m.starts_at ASC, m.title ASC, m.id ASC"
}

func sortMeetups(meetups []Meetup, sortKey string, viewer viewerContext) {
	sort.SliceStable(meetups, func(i, j int) bool {
		left := meetups[i]
		right := meetups[j]
		switch sortKey {
		case "soonest":
			return compareSoonest(left, right)
		case "distance":
			return compareDistance(left, right)
		case "popular":
			if left.AttendeeCt != right.AttendeeCt {
				return left.AttendeeCt > right.AttendeeCt
			}
			return compareSoonest(left, right)
		case "newest":
			if left.PublishedAt != nil && right.PublishedAt != nil && !left.PublishedAt.Equal(*right.PublishedAt) {
				return left.PublishedAt.After(*right.PublishedAt)
			}
			if left.CreatedAt != right.CreatedAt {
				return left.CreatedAt.After(right.CreatedAt)
			}
			return compareSoonest(left, right)
		default:
			leftScore := recommendedScore(left, viewer)
			rightScore := recommendedScore(right, viewer)
			if math.Abs(leftScore-rightScore) > 0.001 {
				return leftScore > rightScore
			}
			return compareSoonest(left, right)
		}
	})
}

func sortMyMeetups(meetups []Meetup, scope string) {
	sort.SliceStable(meetups, func(i, j int) bool {
		if scope == "past" {
			if !meetups[i].StartsAt.Equal(meetups[j].StartsAt) {
				return meetups[i].StartsAt.After(meetups[j].StartsAt)
			}
			return meetups[i].Title < meetups[j].Title
		}
		return compareSoonest(meetups[i], meetups[j])
	})
}

func compareSoonest(left, right Meetup) bool {
	if !left.StartsAt.Equal(right.StartsAt) {
		return left.StartsAt.Before(right.StartsAt)
	}
	return left.Title < right.Title
}

func compareDistance(left, right Meetup) bool {
	if left.DistanceKM == nil && right.DistanceKM == nil {
		return compareSoonest(left, right)
	}
	if left.DistanceKM == nil {
		return false
	}
	if right.DistanceKM == nil {
		return true
	}
	if math.Abs(*left.DistanceKM-*right.DistanceKM) > 0.001 {
		return *left.DistanceKM < *right.DistanceKM
	}
	return compareSoonest(left, right)
}

func recommendedScore(meetup Meetup, viewer viewerContext) float64 {
	score := 100.0
	if meetup.DistanceKM != nil {
		score -= math.Min(*meetup.DistanceKM, 100.0) * 0.7
	}
	hoursUntil := meetup.StartsAt.Sub(time.Now().UTC()).Hours()
	if hoursUntil > 0 {
		score -= math.Min(hoursUntil, 336) * 0.12
	}
	score += math.Min(float64(meetup.AttendeeCt), 20) * 1.3
	if meetup.WaitlistEnabled && meetup.Capacity != nil && meetup.AttendeeCt >= *meetup.Capacity {
		score -= 8
	}
	if _, ok := viewer.Interests[strings.ToLower(meetup.CategoryLabel)]; ok {
		score += 18
	}
	if meetup.EventType == "online" {
		score += 2
	}
	return score
}

func sliceMeetups(meetups []Meetup, limit, offset int) *CursorPage[Meetup] {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(meetups) {
		return &CursorPage[Meetup]{
			Items:      []Meetup{},
			Limit:      limit,
			HasMore:    false,
			NextCursor: nil,
		}
	}
	end := offset + limit
	hasMore := end < len(meetups)
	if end > len(meetups) {
		end = len(meetups)
	}
	nextCursor := (*string)(nil)
	if hasMore {
		nextCursor = encodeOffsetCursor(end)
	}
	return &CursorPage[Meetup]{
		Items:      meetups[offset:end],
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}
}

func sliceLoadedMeetups(meetups []Meetup, limit, offset int) *CursorPage[Meetup] {
	if limit < 1 {
		limit = 20
	}
	hasMore := len(meetups) > limit
	items := meetups
	if hasMore {
		items = meetups[:limit]
	}
	nextCursor := (*string)(nil)
	if hasMore {
		nextCursor = encodeOffsetCursor(offset + limit)
	}
	return &CursorPage[Meetup]{
		Items:      items,
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}
}

func haversineKM(lat1, lng1, lat2, lng2 float64) float64 {
	const radiusKM = 6371.0
	dLat := radians(lat2 - lat1)
	dLng := radians(lng2 - lng1)
	start := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(radians(lat1))*math.Cos(radians(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return radiusKM * 2 * math.Atan2(math.Sqrt(start), math.Sqrt(1-start))
}

func radians(value float64) float64 {
	return value * math.Pi / 180
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
