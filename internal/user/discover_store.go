package user

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type StoreConfig struct {
	DiscoverPipelineV2 bool
}

func NewPgStoreWithConfig(pool *pgxpool.Pool, cfg StoreConfig) Querier {
	return &pgStore{
		pool:               pool,
		discoverPipelineV2: cfg.DiscoverPipelineV2,
	}
}

func (s *pgStore) discoverUsersV2(ctx context.Context, params DiscoverUsersParams) ([]User, error) {
	viewer, err := s.loadDiscoverViewerFeatures(ctx, params.CurrentUserID)
	if err != nil {
		return nil, err
	}

	filters := buildDiscoverFilters(params)
	poolLimit := discoverCandidatePoolLimit(params)

	nearby, err := s.discoverNearbyCandidates(ctx, params, filters, poolLimit)
	if err != nil {
		return nil, err
	}
	mutual, err := s.discoverMutualCandidates(ctx, params, filters, poolLimit)
	if err != nil {
		return nil, err
	}
	interest, err := s.discoverInterestCandidates(ctx, params, viewer, filters, poolLimit)
	if err != nil {
		return nil, err
	}
	sobriety, err := s.discoverSobrietyCandidates(ctx, params, viewer, filters, poolLimit)
	if err != nil {
		return nil, err
	}
	active, err := s.discoverActiveFallbackCandidates(ctx, params, filters, poolLimit)
	if err != nil {
		return nil, err
	}

	candidates := mergeDiscoverCandidates(nearby, mutual, interest, sobriety, active)
	if len(candidates) == 0 {
		return nil, nil
	}

	now := time.Now().UTC()
	for index := range candidates {
		candidates[index].Score = scoreDiscoverCandidate(viewer, candidates[index], now)
	}

	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].Score == candidates[right].Score {
			return candidates[left].ID.String() < candidates[right].ID.String()
		}
		return candidates[left].Score > candidates[right].Score
	})

	if shouldApplyDiscoverImpressionSuppression(params) {
		impressions, err := s.loadRecentDiscoverImpressions(ctx, params.CurrentUserID, candidateIDs(candidates))
		if err != nil {
			return nil, err
		}
		filtered, changed := filterDiscoverSuppressedCandidates(candidates, impressions, now)
		if changed && len(filtered) > 0 {
			candidates = filtered
		}
	}

	candidates = rerankDiscoverCandidates(candidates)

	start := params.Offset
	if start >= len(candidates) {
		return nil, nil
	}
	end := params.Offset + params.Limit
	if end > len(candidates) {
		end = len(candidates)
	}
	page := candidates[start:end]

	users, err := s.hydrateDiscoverUsers(ctx, page)
	if err != nil {
		return nil, err
	}
	visibleLimit := discoverVisibleLimit(params)
	if visibleLimit > 0 && len(page) > visibleLimit {
		page = page[:visibleLimit]
	}

	_ = s.recordDiscoverImpressions(ctx, params.CurrentUserID, page, now)
	return users, nil
}

func (s *pgStore) countDiscoverUsersV2(ctx context.Context, params DiscoverUsersParams) (int, error) {
	filters := buildDiscoverFilters(params)

	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*)
		FROM users u
		WHERE `+discoverEligibilitySQL("u")+`
			AND ($15 = '' OR u.username ILIKE '%' || $15 || '%')`,
		discoverBaseArgs(params, filters, &params.Query)...,
	).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *pgStore) loadDiscoverViewerFeatures(ctx context.Context, viewerID uuid.UUID) (discoverViewerFeatures, error) {
	viewer := discoverViewerFeatures{UserID: viewerID}

	var interestIDs []uuid.UUID
	var sobrietyBand *int
	var lat *float64
	var lng *float64
	err := s.pool.QueryRow(ctx,
		`SELECT
			COALESCE(array_agg(ui.interest_id) FILTER (WHERE ui.interest_id IS NOT NULL), '{}') AS interest_ids,
			u.sobriety_band,
			u.discover_lat,
			u.discover_lng
		FROM users u
		LEFT JOIN user_interests ui ON ui.user_id = u.id
		WHERE u.id = $1
		GROUP BY u.id, u.sobriety_band, u.discover_lat, u.discover_lng`,
		viewerID,
	).Scan(&interestIDs, &sobrietyBand, &lat, &lng)
	if err != nil {
		return discoverViewerFeatures{}, err
	}

	viewer.InterestIDs = interestIDs
	viewer.SobrietyBand = sobrietyBand
	viewer.Lat = lat
	viewer.Lng = lng
	return viewer, nil
}

func (s *pgStore) discoverNearbyCandidates(ctx context.Context, params DiscoverUsersParams, filters discoverFilters, limit int) ([]discoverCandidate, error) {
	if params.Lat == nil || params.Lng == nil {
		return nil, nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT
			u.id,
			`+discoverDistanceSQL("u")+` AS distance_km,
			0::int AS shared_interest_count,
			0::int AS mutual_friend_count,
			u.sobriety_band,
			u.last_active_at,
			u.profile_completeness
		FROM users u
		WHERE `+discoverEligibilitySQL("u")+`
		ORDER BY distance_km ASC NULLS LAST, u.last_active_at DESC, u.id
		LIMIT $15`,
		append(discoverBaseArgs(params, filters, nil), limit)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanDiscoverCandidates(rows, discoverSourceNearby)
}

func (s *pgStore) discoverMutualCandidates(ctx context.Context, params DiscoverUsersParams, filters discoverFilters, limit int) ([]discoverCandidate, error) {
	rows, err := s.pool.Query(ctx,
		`WITH viewer_friends AS (
			SELECT CASE
				WHEN f.user_a_id = $1 THEN f.user_b_id
				ELSE f.user_a_id
			END AS friend_id
			FROM friendships f
			WHERE (f.user_a_id = $1 OR f.user_b_id = $1)
				AND f.status = 'accepted'
		)
		SELECT
			u.id,
			`+discoverDistanceSQL("u")+` AS distance_km,
			0::int AS shared_interest_count,
			COUNT(DISTINCT vf.friend_id)::int AS mutual_friend_count,
			u.sobriety_band,
			u.last_active_at,
			u.profile_completeness
		FROM viewer_friends vf
		JOIN friendships f2
			ON f2.status = 'accepted'
			AND (f2.user_a_id = vf.friend_id OR f2.user_b_id = vf.friend_id)
		JOIN users u
			ON u.id = CASE
				WHEN f2.user_a_id = vf.friend_id THEN f2.user_b_id
				ELSE f2.user_a_id
			END
		WHERE `+discoverEligibilitySQL("u")+`
		GROUP BY u.id, distance_km, u.sobriety_band, u.last_active_at, u.profile_completeness
		ORDER BY mutual_friend_count DESC, u.last_active_at DESC, u.id
		LIMIT $15`,
		append(discoverBaseArgs(params, filters, nil), limit)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanDiscoverCandidates(rows, discoverSourceMutual)
}

func (s *pgStore) discoverInterestCandidates(ctx context.Context, params DiscoverUsersParams, viewer discoverViewerFeatures, filters discoverFilters, limit int) ([]discoverCandidate, error) {
	if len(viewer.InterestIDs) == 0 && len(params.Interests) == 0 {
		return nil, nil
	}

	baseArgs := discoverBaseArgs(params, filters, nil)
	var query string
	var args []any

	if len(viewer.InterestIDs) > 0 {
		query = `SELECT
			u.id,
			` + discoverDistanceSQL("u") + ` AS distance_km,
			COUNT(DISTINCT ui.interest_id)::int AS shared_interest_count,
			0::int AS mutual_friend_count,
			u.sobriety_band,
			u.last_active_at,
			u.profile_completeness
		FROM users u
		JOIN user_interests ui ON ui.user_id = u.id
		WHERE ` + discoverEligibilitySQL("u") + `
			AND ui.interest_id = ANY($15::uuid[])
		GROUP BY u.id, distance_km, u.sobriety_band, u.last_active_at, u.profile_completeness
		ORDER BY shared_interest_count DESC, u.last_active_at DESC, u.id
		LIMIT $16`
		args = append(baseArgs, viewer.InterestIDs, limit)
	} else {
		query = `SELECT
			u.id,
			` + discoverDistanceSQL("u") + ` AS distance_km,
			COUNT(DISTINCT i.id)::int AS shared_interest_count,
			0::int AS mutual_friend_count,
			u.sobriety_band,
			u.last_active_at,
			u.profile_completeness
		FROM users u
		JOIN user_interests ui ON ui.user_id = u.id
		JOIN interests i ON i.id = ui.interest_id
		WHERE ` + discoverEligibilitySQL("u") + `
			AND i.name = ANY($15::text[])
		GROUP BY u.id, distance_km, u.sobriety_band, u.last_active_at, u.profile_completeness
		ORDER BY shared_interest_count DESC, u.last_active_at DESC, u.id
		LIMIT $16`
		args = append(baseArgs, nullableTextArray(params.Interests), limit)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanDiscoverCandidates(rows, discoverSourceInterest)
}

func (s *pgStore) discoverSobrietyCandidates(ctx context.Context, params DiscoverUsersParams, viewer discoverViewerFeatures, filters discoverFilters, limit int) ([]discoverCandidate, error) {
	if viewer.SobrietyBand == nil && filters.SobrietyMinBand == nil {
		return nil, nil
	}

	targetBand := filters.SobrietyMinBand
	if targetBand == nil {
		targetBand = viewer.SobrietyBand
	}

	rows, err := s.pool.Query(ctx,
		`SELECT
			u.id,
			`+discoverDistanceSQL("u")+` AS distance_km,
			0::int AS shared_interest_count,
			0::int AS mutual_friend_count,
			u.sobriety_band,
			u.last_active_at,
			u.profile_completeness
		FROM users u
		WHERE `+discoverEligibilitySQL("u")+`
			AND u.sobriety_band IS NOT NULL
		ORDER BY ABS(u.sobriety_band - $15::int) ASC, u.last_active_at DESC, u.id
		LIMIT $16`,
		append(discoverBaseArgs(params, filters, nil), *targetBand, limit)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanDiscoverCandidates(rows, discoverSourceSobriety)
}

func (s *pgStore) discoverActiveFallbackCandidates(ctx context.Context, params DiscoverUsersParams, filters discoverFilters, limit int) ([]discoverCandidate, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			u.id,
			`+discoverDistanceSQL("u")+` AS distance_km,
			0::int AS shared_interest_count,
			0::int AS mutual_friend_count,
			u.sobriety_band,
			u.last_active_at,
			u.profile_completeness
		FROM users u
		WHERE `+discoverEligibilitySQL("u")+`
		ORDER BY u.last_active_at DESC, u.created_at DESC, u.id
		LIMIT $15`,
		append(discoverBaseArgs(params, filters, nil), limit)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanDiscoverCandidates(rows, discoverSourceActive)
}

func (s *pgStore) hydrateDiscoverUsers(ctx context.Context, candidates []discoverCandidate) ([]User, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	ids := candidateIDs(candidates)
	rows, err := s.pool.Query(ctx,
		`SELECT
			u.id,
			u.username,
			u.avatar_url,
			(u.subscription_tier = 'plus' AND u.subscription_status = 'active') AS is_plus,
			u.subscription_tier,
			u.subscription_status,
			COALESCE(u.current_city, u.city) AS city,
			u.country,
			u.bio,
			COALESCE(interest_names.items, '{}') AS interests,
			u.gender,
			CASE
				WHEN u.birth_date IS NULL THEN NULL
				ELSE TO_CHAR(u.birth_date, 'YYYY-MM-DD')
			END AS birth_date,
			u.sober_since,
			u.created_at,
			'none' AS friendship_status
		FROM users u
		LEFT JOIN LATERAL (
			SELECT array_agg(i.name ORDER BY i.name) AS items
			FROM user_interests ui
			JOIN interests i ON i.id = ui.interest_id
			WHERE ui.user_id = u.id
		) interest_names ON true
		WHERE u.id = ANY($1::uuid[])
		ORDER BY array_position($1::uuid[], u.id)`,
		ids,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUsers(rows)
}

func (s *pgStore) loadRecentDiscoverImpressions(ctx context.Context, viewerID uuid.UUID, candidateIDs []uuid.UUID) (map[uuid.UUID]time.Time, error) {
	if len(candidateIDs) == 0 {
		return nil, nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT candidate_id, MAX(shown_at) AS shown_at
		FROM discover_impressions
		WHERE viewer_id = $1
			AND candidate_id = ANY($2::uuid[])
			AND shown_at > NOW() - INTERVAL '45 minutes'
		GROUP BY candidate_id`,
		viewerID, candidateIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	impressions := make(map[uuid.UUID]time.Time, len(candidateIDs))
	for rows.Next() {
		var candidateID uuid.UUID
		var shownAt time.Time
		if err := rows.Scan(&candidateID, &shownAt); err != nil {
			return nil, err
		}
		impressions[candidateID] = shownAt
	}
	return impressions, rows.Err()
}

func (s *pgStore) recordDiscoverImpressions(ctx context.Context, viewerID uuid.UUID, candidates []discoverCandidate, shownAt time.Time) error {
	if len(candidates) == 0 {
		return nil
	}

	values := make([]string, 0, len(candidates))
	args := make([]any, 0, len(candidates)*4)
	for index, candidate := range candidates {
		start := index*4 + 1
		values = append(values, fmt.Sprintf("($%d, $%d, $%d, $%d)", start, start+1, start+2, start+3))
		args = append(args, viewerID, candidate.ID, discoverPrimarySource(candidate), shownAt)
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO discover_impressions (viewer_id, candidate_id, source, shown_at)
		VALUES `+strings.Join(values, ","),
		args...,
	)
	return err
}

func scanDiscoverCandidates(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}, source string) ([]discoverCandidate, error) {
	candidates := make([]discoverCandidate, 0)
	for rows.Next() {
		var candidate discoverCandidate
		var distanceKm *float64
		var sobrietyBand *int
		var lastActiveAt *time.Time
		if err := rows.Scan(
			&candidate.ID,
			&distanceKm,
			&candidate.SharedInterestCount,
			&candidate.MutualFriendCount,
			&sobrietyBand,
			&lastActiveAt,
			&candidate.ProfileCompleteness,
		); err != nil {
			return nil, err
		}
		candidate.DistanceKm = distanceKm
		candidate.SobrietyBand = sobrietyBand
		candidate.LastActiveAt = lastActiveAt
		candidate.Sources = []string{source}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func candidateIDs(candidates []discoverCandidate) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	return ids
}

func discoverBaseArgs(params DiscoverUsersParams, filters discoverFilters, query *string) []any {
	args := []any{
		params.CurrentUserID,
		params.City,
		params.Gender,
		params.AgeMin,
		params.AgeMax,
		filters.SobrietyMinBand,
		filters.Bounds.MinLat,
		filters.Bounds.MaxLat,
		filters.Bounds.MinLng,
		filters.Bounds.MaxLng,
		params.Lat,
		params.Lng,
		params.DistanceKm,
		nullableTextArray(params.Interests),
	}
	if query != nil {
		args = append(args, *query)
	}
	return args
}

func discoverEligibilitySQL(alias string) string {
	distanceExpr := discoverDistanceSQL(alias)
	return fmt.Sprintf(`%[1]s.id != $1
			AND NOT EXISTS (
				SELECT 1
				FROM friendships fx
				WHERE (fx.user_a_id = $1 AND fx.user_b_id = %[1]s.id)
					OR (fx.user_b_id = $1 AND fx.user_a_id = %[1]s.id)
			)
			AND NOT EXISTS (
				SELECT 1
				FROM discover_dismissals dd
				WHERE dd.viewer_id = $1
					AND dd.candidate_id = %[1]s.id
					AND dd.dismissed_at > NOW() - INTERVAL '14 days'
			)
			AND ($2 = '' OR COALESCE(%[1]s.current_city, %[1]s.city) ILIKE $2)
			AND ($3 = '' OR %[1]s.gender = $3)
			AND ($4::int IS NULL OR (%[1]s.birth_date IS NOT NULL AND %[1]s.birth_date <= CURRENT_DATE - make_interval(years => $4::int)))
			AND ($5::int IS NULL OR (%[1]s.birth_date IS NOT NULL AND %[1]s.birth_date > CURRENT_DATE - make_interval(years => ($5::int + 1))))
			AND ($6::int IS NULL OR (%[1]s.sobriety_band IS NOT NULL AND %[1]s.sobriety_band >= $6::int))
			AND (
				$13::int IS NULL
				OR $13::int <= 0
				OR $11::float8 IS NULL
				OR $12::float8 IS NULL
				OR (
					%[1]s.discover_lat IS NOT NULL
					AND %[1]s.discover_lng IS NOT NULL
					AND ($7::float8 IS NULL OR %[1]s.discover_lat BETWEEN $7::float8 AND $8::float8)
					AND ($9::float8 IS NULL OR %[1]s.discover_lng BETWEEN $9::float8 AND $10::float8)
					AND %[2]s <= $13::float8
				)
			)
			AND (
				$14::text[] IS NULL
				OR EXISTS (
					SELECT 1
					FROM user_interests ui
					JOIN interests i ON i.id = ui.interest_id
					WHERE ui.user_id = %[1]s.id
						AND i.name = ANY($14::text[])
				)
			)`, alias, distanceExpr)
}

func discoverDistanceSQL(alias string) string {
	return fmt.Sprintf(`CASE
			WHEN $11::float8 IS NOT NULL
				AND $12::float8 IS NOT NULL
				AND %[1]s.discover_lat IS NOT NULL
				AND %[1]s.discover_lng IS NOT NULL
			THEN 2.0 * 6371.0 * ASIN(SQRT(
				POWER(SIN(RADIANS((%[1]s.discover_lat - $11::float8) / 2.0)), 2)
				+ COS(RADIANS($11::float8)) * COS(RADIANS(%[1]s.discover_lat))
				* POWER(SIN(RADIANS((%[1]s.discover_lng - $12::float8) / 2.0)), 2)
			))
			ELSE NULL
		END`, alias)
}
