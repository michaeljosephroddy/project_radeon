package dating

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project_radeon/api/internal/user"
	"github.com/project_radeon/api/pkg/observability"
)

type pgStore struct {
	pool *pgxpool.Pool
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type datingCandidateSource struct {
	name         string
	join         string
	where        string
	orderBy      string
	limit        int
	incomingLike bool
}

func NewPgStore(pool *pgxpool.Pool) Querier {
	return &pgStore{pool: pool}
}

func (s *pgStore) Discover(ctx context.Context, params DiscoverParams) ([]user.User, error) {
	ranked, err := s.loadDatingRankedWindow(ctx, params, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return s.discoverUsersFromRankedCandidates(ctx, params, ranked)
}

func (s *pgStore) loadDatingRankedWindow(ctx context.Context, params DiscoverParams, now time.Time) ([]datingCandidate, error) {
	start := time.Now()
	viewer, err := s.loadDatingViewerFeatures(ctx, params.CurrentUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDatingDisabled
	}
	if err != nil {
		return nil, err
	}

	candidates, err := s.loadDatingCandidatePool(ctx, params, false)
	if err != nil {
		return nil, err
	}
	if len(candidates) < params.Limit+1 {
		candidates, err = s.loadDatingCandidatePool(ctx, params, true)
		if err != nil {
			return nil, err
		}
	}
	candidates = rankDatingCandidates(viewer, candidates, now)
	if len(candidates) > datingRankedWindowLimit(params) {
		candidates = candidates[:datingRankedWindowLimit(params)]
	}
	observability.IncrementCounter("dating.discover.rank_window.requests", 1)
	observability.IncrementCounter("dating.discover.rank_window.candidates", int64(len(candidates)))
	observability.ObserveDuration("dating.discover.rank_window", time.Since(start), nil)
	return candidates, nil
}

func (s *pgStore) discoverUsersFromRankedCandidates(ctx context.Context, params DiscoverParams, candidates []datingCandidate) ([]user.User, error) {
	offset := params.CursorOffset
	if offset == 0 {
		offset = parseOffset(params.Cursor)
	}
	if offset >= len(candidates) {
		return []user.User{}, nil
	}
	end := offset + params.Limit + 1
	if end > len(candidates) {
		end = len(candidates)
	}
	visibleEnd := offset + params.Limit
	if visibleEnd > len(candidates) {
		visibleEnd = len(candidates)
	}
	if visibleEnd > offset {
		_ = s.recordDatingImpressions(ctx, params, candidates[offset:visibleEnd])
	}
	users := make([]user.User, 0, end-offset)
	for _, candidate := range candidates[offset:end] {
		users = append(users, candidate.User)
	}
	return users, nil
}

func (s *pgStore) loadDatingViewerFeatures(ctx context.Context, userID uuid.UUID) (datingViewerFeatures, error) {
	viewer := datingViewerFeatures{UserID: userID}
	err := s.pool.QueryRow(ctx,
		`SELECT sobriety_band
		FROM users
		WHERE id = $1
			AND deleted_at IS NULL
			AND connection_intents @> ARRAY['dating']::text[]`,
		userID,
	).Scan(&viewer.SobrietyBand)
	return viewer, err
}

func (s *pgStore) loadDatingCandidatePool(ctx context.Context, params DiscoverParams, includeRecentImpressions bool) ([]datingCandidate, error) {
	limit := datingSourceWindowLimit(params)
	sources := []datingCandidateSource{
		{
			name:    "active",
			limit:   limit,
			orderBy: "u.last_active_at DESC NULLS LAST, u.profile_completeness DESC, u.id DESC",
		},
		{
			name:    "fresh",
			limit:   limit,
			orderBy: "u.created_at DESC, u.last_active_at DESC NULLS LAST, u.id DESC",
		},
	}
	if params.Lat != nil && params.Lng != nil {
		source := datingCandidateSource{
			name:    "nearby",
			limit:   limit,
			where:   "AND u.discover_lat IS NOT NULL AND u.discover_lng IS NOT NULL",
			orderBy: "distance_km ASC NULLS LAST, u.last_active_at DESC NULLS LAST, u.id DESC",
		}
		if params.DistanceKm != nil && *params.DistanceKm > 0 {
			source.where += datingBoundingBoxWhereSQL
		}
		sources = append([]datingCandidateSource{source}, sources...)
	}
	if len(params.Interests) > 0 {
		sources = append(sources, datingCandidateSource{
			name:    "shared_interests",
			limit:   limit,
			where:   "AND shared_interests.count > 0",
			orderBy: "shared_interests.count DESC, u.last_active_at DESC NULLS LAST, u.id DESC",
		})
	}
	sources = append(sources, datingCandidateSource{
		name:         "incoming_like",
		limit:        limit,
		join:         "JOIN dating_actions incoming_like ON incoming_like.actor_id = u.id AND incoming_like.target_id = $1 AND incoming_like.action = 'like'",
		orderBy:      "incoming_like.updated_at DESC, u.last_active_at DESC NULLS LAST, u.id DESC",
		incomingLike: true,
	})

	groups := make([][]datingCandidate, 0, len(sources))
	for _, source := range sources {
		candidates, err := s.loadDatingCandidatesFromSource(ctx, params, source, includeRecentImpressions)
		if err != nil {
			return nil, err
		}
		groups = append(groups, candidates)
	}
	return mergeDatingCandidates(groups...), nil
}

func (s *pgStore) loadDatingCandidatesFromSource(ctx context.Context, params DiscoverParams, source datingCandidateSource, includeRecentImpressions bool) ([]datingCandidate, error) {
	whereSQL := datingDiscoverWhereSQL
	if strings.TrimSpace(source.where) != "" {
		whereSQL += "\n\t\t\t" + source.where
	}
	if !includeRecentImpressions {
		whereSQL += datingRecentImpressionSuppressionSQL
	}
	orderBy := strings.TrimSpace(source.orderBy)
	if orderBy == "" {
		orderBy = "u.last_active_at DESC NULLS LAST, u.profile_completeness DESC, u.id DESC"
	}

	joinSQL := ""
	if strings.TrimSpace(source.join) != "" {
		joinSQL = "\n\t\t" + strings.TrimSpace(source.join)
	}

	rows, err := s.pool.Query(ctx, datingDiscoverCandidateSelectSQL+joinSQL+whereSQL+`
		ORDER BY `+orderBy+`
		LIMIT $10`,
		datingDiscoverCandidateArgs(params, source.limit)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates, err := scanDatingCandidates(rows)
	if err != nil {
		return nil, err
	}
	for index := range candidates {
		candidates[index].Source = source.name
		if source.incomingLike {
			candidates[index].IncomingLike = true
		}
	}
	return candidates, nil
}

func (s *pgStore) recordDatingImpressions(ctx context.Context, params DiscoverParams, candidates []datingCandidate) error {
	if len(candidates) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for index, candidate := range candidates {
		source := strings.TrimSpace(candidate.Source)
		if source == "" {
			source = "ranked"
		}
		batch.Queue(
			`INSERT INTO dating_impressions (viewer_id, candidate_id, source, rank_score, rank_position, request_id)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			params.CurrentUserID,
			candidate.User.ID,
			source,
			candidate.Score,
			params.CursorOffset+index,
			nullableUUIDString(params.CursorRequestID),
		)
	}
	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range candidates {
		if _, err := results.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func nullableUUIDString(raw string) *uuid.UUID {
	parsed, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil
	}
	return &parsed
}

func (s *pgStore) CountDiscover(ctx context.Context, params DiscoverParams) (int, error) {
	if ok, err := s.userHasDating(ctx, params.CurrentUserID); err != nil {
		return 0, err
	} else if !ok {
		return 0, ErrDatingDisabled
	}

	var count int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users u `+datingDiscoverWhereSQL, datingDiscoverArgs(params, 0)[:9]...).Scan(&count)
	return count, err
}

func (s *pgStore) ListLikes(ctx context.Context, userID uuid.UUID, before *string, limit int) ([]DatingLike, error) {
	var beforeTime *time.Time
	if before != nil {
		if parsed, err := time.Parse(time.RFC3339Nano, *before); err == nil {
			beforeTime = &parsed
		}
	}

	rows, err := s.pool.Query(ctx, datingLikesSelectSQL+`
		`+datingLikesWhereSQL+`
			AND ($2::timestamptz IS NULL OR da.updated_at < $2)
		ORDER BY da.updated_at DESC, da.id DESC
		LIMIT $3`,
		userID, beforeTime, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDatingLikes(rows)
}

func (s *pgStore) CountLikes(ctx context.Context, userID uuid.UUID) (int, error) {
	if ok, err := s.userHasDating(ctx, userID); err != nil {
		return 0, err
	} else if !ok {
		return 0, ErrDatingDisabled
	}

	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM dating_actions da
		JOIN users u ON u.id = da.actor_id
		`+datingLikesWhereSQL,
		userID,
	).Scan(&count)
	return count, err
}

func (s *pgStore) RecordAction(ctx context.Context, actorID, targetID uuid.UUID, action string) (*ActionResult, error) {
	if actorID == targetID {
		return nil, ErrForbidden
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if err := validateDatingPair(ctx, tx, actorID, targetID); err != nil {
		return nil, err
	}

	var existingAction string
	err = tx.QueryRow(ctx,
		`SELECT action FROM dating_actions WHERE actor_id = $1 AND target_id = $2`,
		actorID, targetID,
	).Scan(&existingAction)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err == nil && existingAction != action {
		return nil, ErrConflict
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := tx.Exec(ctx,
			`INSERT INTO dating_actions (actor_id, target_id, action)
			VALUES ($1, $2, $3)`,
			actorID, targetID, action,
		); err != nil {
			return nil, err
		}
	}

	result := &ActionResult{Action: action}
	if action == ActionLike {
		match, err := s.createMatchIfMutual(ctx, tx, actorID, targetID)
		if err != nil {
			return nil, err
		}
		if match != nil {
			result.Matched = true
			result.Match = match
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *pgStore) ListMatches(ctx context.Context, userID uuid.UUID, before *string, limit int) ([]DatingMatch, error) {
	var beforeTime *time.Time
	if before != nil {
		if parsed, err := time.Parse(time.RFC3339Nano, *before); err == nil {
			beforeTime = &parsed
		}
	}

	rows, err := s.pool.Query(ctx, datingMatchSelectSQL+`
		WHERE dm.status = 'active'
			AND (dm.user_a_id = $1 OR dm.user_b_id = $1)
			AND u.deleted_at IS NULL
			AND ($2::timestamptz IS NULL OR dm.matched_at < $2)
		ORDER BY dm.matched_at DESC, dm.id DESC
		LIMIT $3`,
		userID, beforeTime, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDatingMatches(rows)
}

func (s *pgStore) GetMatch(ctx context.Context, userID, matchID uuid.UUID) (*DatingMatch, error) {
	match, err := loadDatingMatch(ctx, s.pool, userID, matchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return match, err
}

func (s *pgStore) Unmatch(ctx context.Context, userID, matchID uuid.UUID) (*DatingMatch, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var chatID *uuid.UUID
	err = tx.QueryRow(ctx,
		`UPDATE dating_matches
		SET status = 'unmatched',
			unmatched_at = NOW(),
			unmatched_by = $1,
			updated_at = NOW()
		WHERE id = $2
			AND status = 'active'
			AND (user_a_id = $1 OR user_b_id = $1)
		RETURNING chat_id`,
		userID, matchID,
	).Scan(&chatID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if chatID != nil {
		if _, err := tx.Exec(ctx, `UPDATE chats SET status = 'closed' WHERE id = $1`, *chatID); err != nil {
			return nil, err
		}
	}

	match, err := loadDatingMatch(ctx, tx, userID, matchID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return match, nil
}

func (s *pgStore) userHasDating(ctx context.Context, userID uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM users
			WHERE id = $1
				AND deleted_at IS NULL
				AND connection_intents @> ARRAY['dating']::text[]
		)`,
		userID,
	).Scan(&ok)
	return ok, err
}

func validateDatingPair(ctx context.Context, q querier, actorID, targetID uuid.UUID) error {
	var actorDating, targetDating, blocked, acceptedFriends bool
	err := q.QueryRow(ctx,
		`SELECT
			EXISTS(SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL AND connection_intents @> ARRAY['dating']::text[]),
			EXISTS(SELECT 1 FROM users WHERE id = $2 AND deleted_at IS NULL AND connection_intents @> ARRAY['dating']::text[]),
			EXISTS(
				SELECT 1 FROM user_blocks
				WHERE (blocker_id = $1 AND blocked_id = $2)
					OR (blocker_id = $2 AND blocked_id = $1)
			),
			EXISTS(
				SELECT 1 FROM friendships
				WHERE status = 'accepted'
					AND ((user_a_id = $1 AND user_b_id = $2) OR (user_a_id = $2 AND user_b_id = $1))
			)`,
		actorID, targetID,
	).Scan(&actorDating, &targetDating, &blocked, &acceptedFriends)
	if err != nil {
		return err
	}
	if !actorDating {
		return ErrDatingDisabled
	}
	if !targetDating {
		return ErrTargetUnavailable
	}
	if blocked || acceptedFriends {
		return ErrForbidden
	}
	return nil
}

func (s *pgStore) createMatchIfMutual(ctx context.Context, q querier, actorID, targetID uuid.UUID) (*DatingMatch, error) {
	var reverseLike bool
	if err := q.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM dating_actions
			WHERE actor_id = $1 AND target_id = $2 AND action = 'like'
		)`,
		targetID, actorID,
	).Scan(&reverseLike); err != nil {
		return nil, err
	}
	if !reverseLike {
		return nil, nil
	}

	userAID, userBID := sortPair(actorID, targetID)
	var existingID uuid.UUID
	var existingStatus string
	err := q.QueryRow(ctx,
		`SELECT id, status FROM dating_matches WHERE user_a_id = $1 AND user_b_id = $2`,
		userAID, userBID,
	).Scan(&existingID, &existingStatus)
	if err == nil {
		if existingStatus == "active" {
			return loadDatingMatch(ctx, q, actorID, existingID)
		}
		return nil, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	chatID, err := findOrCreateDirectChat(ctx, q, actorID, targetID)
	if err != nil {
		return nil, err
	}

	var matchID uuid.UUID
	err = q.QueryRow(ctx,
		`INSERT INTO dating_matches (user_a_id, user_b_id, chat_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_a_id, user_b_id) DO NOTHING
		RETURNING id`,
		userAID, userBID, chatID,
	).Scan(&matchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return loadDatingMatch(ctx, q, actorID, matchID)
}

func findOrCreateDirectChat(ctx context.Context, q querier, userID, otherUserID uuid.UUID) (uuid.UUID, error) {
	var chatID uuid.UUID
	err := q.QueryRow(ctx,
		`SELECT ch.id
		FROM chats ch
		JOIN chat_members cm1 ON cm1.chat_id = ch.id AND cm1.user_id = $1
		JOIN chat_members cm2 ON cm2.chat_id = ch.id AND cm2.user_id = $2
		WHERE ch.is_group = FALSE
			AND ch.status != 'closed'
		ORDER BY ch.created_at DESC
		LIMIT 1`,
		userID, otherUserID,
	).Scan(&chatID)
	if err == nil {
		_, updateErr := q.Exec(ctx, `UPDATE chats SET status = 'active' WHERE id = $1`, chatID)
		return chatID, updateErr
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}

	if err := q.QueryRow(ctx,
		`INSERT INTO chats (is_group, name, status, support_request_id)
		VALUES (FALSE, NULL, 'active', NULL)
		RETURNING id`,
	).Scan(&chatID); err != nil {
		return uuid.Nil, err
	}
	if _, err := q.Exec(ctx,
		`INSERT INTO chat_members (chat_id, user_id, role)
		VALUES ($1, $2, 'requester'), ($1, $3, 'addressee')`,
		chatID, userID, otherUserID,
	); err != nil {
		return uuid.Nil, err
	}
	return chatID, nil
}

func sortPair(a, b uuid.UUID) (uuid.UUID, uuid.UUID) {
	if a.String() < b.String() {
		return a, b
	}
	return b, a
}

func datingDiscoverArgs(params DiscoverParams, limit int) []any {
	return []any{
		params.CurrentUserID,
		params.Gender,
		params.AgeMin,
		params.AgeMax,
		sobrietyMinimumDays(params.Sobriety),
		params.Lat,
		params.Lng,
		params.DistanceKm,
		params.Interests,
		parseOffset(params.Cursor),
		limit,
	}
}

func datingDiscoverCandidateArgs(params DiscoverParams, limit int) []any {
	return []any{
		params.CurrentUserID,
		params.Gender,
		params.AgeMin,
		params.AgeMax,
		sobrietyMinimumDays(params.Sobriety),
		params.Lat,
		params.Lng,
		params.DistanceKm,
		params.Interests,
		limit,
	}
}

func sobrietyMinimumDays(filter string) *int {
	var days int
	switch filter {
	case "days_30":
		days = 30
	case "days_90":
		days = 90
	case "years_1":
		days = 365
	case "years_5":
		days = 365 * 5
	default:
		return nil
	}
	return &days
}

const datingUserColumns = `
			u.id,
			u.username,
			u.avatar_url,
			(u.subscription_tier = 'plus' AND u.subscription_status = 'active') AS is_plus,
			u.subscription_tier,
			u.subscription_status,
			u.city,
			u.country,
			u.bio,
			COALESCE(interest_names.items, '{}') AS interests,
			u.connection_intents,
			u.gender,
			CASE
				WHEN u.birth_date IS NULL THEN NULL
				ELSE TO_CHAR(u.birth_date, 'YYYY-MM-DD')
			END AS birth_date,
			u.sober_since,
			u.created_at,
			CASE
				WHEN f.status = 'accepted' THEN 'friends'
				WHEN f.requester_id = $1 THEN 'outgoing'
				WHEN f.requester_id = u.id THEN 'incoming'
				ELSE 'none'
			END AS friendship_status,
			u.friend_count,
			0 AS incoming_friend_request_count,
			0 AS outgoing_friend_request_count,
			u.current_city,
			u.location_updated_at`

const datingDiscoverSelectSQL = `SELECT` + datingUserColumns + `
		FROM users u
		LEFT JOIN friendships f
			ON ((f.user_a_id = $1 AND f.user_b_id = u.id) OR (f.user_b_id = $1 AND f.user_a_id = u.id))
		LEFT JOIN LATERAL (
			SELECT array_agg(i.name ORDER BY i.name) AS items
			FROM user_interests ui
			JOIN interests i ON i.id = ui.interest_id
			WHERE ui.user_id = u.id
		) interest_names ON true`

const datingDistanceSQL = `CASE
				WHEN $6::float8 IS NULL OR $7::float8 IS NULL OR u.discover_lat IS NULL OR u.discover_lng IS NULL THEN NULL
				ELSE 2.0 * 6371.0 * ASIN(SQRT(
					POWER(SIN(RADIANS((u.discover_lat - $6::float8) / 2.0)), 2)
					+ COS(RADIANS($6::float8)) * COS(RADIANS(u.discover_lat))
					* POWER(SIN(RADIANS((u.discover_lng - $7::float8) / 2.0)), 2)
				))
			END`

const datingCandidateColumns = datingUserColumns + `,
			` + datingDistanceSQL + ` AS distance_km,
			COALESCE(shared_interests.count, 0)::int AS shared_interest_count,
			u.sobriety_band,
			u.profile_completeness,
			u.last_active_at,
			recent_impression.shown_at`

const datingDiscoverCandidateSelectSQL = `SELECT` + datingCandidateColumns + `
		FROM users u
		LEFT JOIN friendships f
			ON ((f.user_a_id = $1 AND f.user_b_id = u.id) OR (f.user_b_id = $1 AND f.user_a_id = u.id))
		LEFT JOIN LATERAL (
			SELECT array_agg(i.name ORDER BY i.name) AS items
			FROM user_interests ui
			JOIN interests i ON i.id = ui.interest_id
			WHERE ui.user_id = u.id
		) interest_names ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(DISTINCT ui.interest_id)::int AS count
			FROM user_interests ui
			JOIN user_interests viewer_ui
				ON viewer_ui.interest_id = ui.interest_id
				AND viewer_ui.user_id = $1
			WHERE ui.user_id = u.id
		) shared_interests ON true
		LEFT JOIN LATERAL (
			SELECT di.shown_at
			FROM dating_impressions di
			WHERE di.viewer_id = $1 AND di.candidate_id = u.id
			ORDER BY di.shown_at DESC
			LIMIT 1
		) recent_impression ON true`

const datingBoundingBoxWhereSQL = `
			AND u.discover_lat BETWEEN ($6::float8 - ($8::float8 / 111.0)) AND ($6::float8 + ($8::float8 / 111.0))
			AND u.discover_lng BETWEEN ($7::float8 - ($8::float8 / (111.0 * GREATEST(ABS(COS(RADIANS($6::float8))), 0.1))))
				AND ($7::float8 + ($8::float8 / (111.0 * GREATEST(ABS(COS(RADIANS($6::float8))), 0.1))))`

const datingRecentImpressionSuppressionSQL = `
			AND NOT EXISTS (
				SELECT 1 FROM dating_impressions recent_di
				WHERE recent_di.viewer_id = $1
					AND recent_di.candidate_id = u.id
					AND recent_di.shown_at >= NOW() - INTERVAL '72 hours'
			)`

const datingDiscoverWhereSQL = `
		WHERE u.id != $1
			AND u.deleted_at IS NULL
			AND u.connection_intents @> ARRAY['dating']::text[]
			AND NOT EXISTS (
				SELECT 1 FROM user_blocks ub
				WHERE (ub.blocker_id = $1 AND ub.blocked_id = u.id)
					OR (ub.blocker_id = u.id AND ub.blocked_id = $1)
			)
			AND NOT EXISTS (
				SELECT 1 FROM friendships fx
				WHERE fx.status = 'accepted'
					AND ((fx.user_a_id = $1 AND fx.user_b_id = u.id) OR (fx.user_b_id = $1 AND fx.user_a_id = u.id))
			)
			AND NOT EXISTS (
				SELECT 1 FROM dating_actions da
				WHERE da.actor_id = $1 AND da.target_id = u.id
			)
			AND NOT EXISTS (
				SELECT 1 FROM dating_matches dm
				WHERE dm.status = 'active'
					AND ((dm.user_a_id = $1 AND dm.user_b_id = u.id) OR (dm.user_b_id = $1 AND dm.user_a_id = u.id))
			)
			AND ($2 = '' OR u.gender = $2)
			AND ($3::int IS NULL OR (u.birth_date IS NOT NULL AND u.birth_date <= CURRENT_DATE - make_interval(years => $3::int)))
			AND ($4::int IS NULL OR (u.birth_date IS NOT NULL AND u.birth_date > CURRENT_DATE - make_interval(years => ($4::int + 1))))
			AND ($5::int IS NULL OR (u.sober_since IS NOT NULL AND EXTRACT(EPOCH FROM (NOW() - u.sober_since::timestamptz)) / 86400.0 >= $5::float8))
			AND (
				$8::int IS NULL
				OR $8::int <= 0
				OR $6::float8 IS NULL
				OR $7::float8 IS NULL
				OR (
					u.discover_lat IS NOT NULL
					AND u.discover_lng IS NOT NULL
					AND 2.0 * 6371.0 * ASIN(SQRT(
						POWER(SIN(RADIANS((u.discover_lat - $6::float8) / 2.0)), 2)
						+ COS(RADIANS($6::float8)) * COS(RADIANS(u.discover_lat))
						* POWER(SIN(RADIANS((u.discover_lng - $7::float8) / 2.0)), 2)
					)) <= $8::float8
				)
			)
			AND (
				$9::text[] IS NULL
				OR cardinality($9::text[]) = 0
				OR EXISTS (
					SELECT 1
					FROM user_interests ui
					JOIN interests i ON i.id = ui.interest_id
					WHERE ui.user_id = u.id
						AND i.name = ANY($9::text[])
				)
			)`

const datingLikesSelectSQL = `SELECT
			da.updated_at,
			` + datingUserColumns + `
		FROM dating_actions da
		JOIN users u ON u.id = da.actor_id
		LEFT JOIN friendships f
			ON ((f.user_a_id = $1 AND f.user_b_id = u.id) OR (f.user_b_id = $1 AND f.user_a_id = u.id))
		LEFT JOIN LATERAL (
			SELECT array_agg(i.name ORDER BY i.name) AS items
			FROM user_interests ui
			JOIN interests i ON i.id = ui.interest_id
			WHERE ui.user_id = u.id
		) interest_names ON true`

const datingLikesWhereSQL = `
		WHERE da.target_id = $1
			AND da.action = 'like'
			AND u.deleted_at IS NULL
			AND u.connection_intents @> ARRAY['dating']::text[]
			AND NOT EXISTS (
				SELECT 1 FROM dating_actions viewer_action
				WHERE viewer_action.actor_id = $1 AND viewer_action.target_id = u.id
			)
			AND NOT EXISTS (
				SELECT 1 FROM dating_matches dm
				WHERE dm.status = 'active'
					AND ((dm.user_a_id = $1 AND dm.user_b_id = u.id) OR (dm.user_b_id = $1 AND dm.user_a_id = u.id))
			)
			AND NOT EXISTS (
				SELECT 1 FROM user_blocks ub
				WHERE (ub.blocker_id = $1 AND ub.blocked_id = u.id)
					OR (ub.blocker_id = u.id AND ub.blocked_id = $1)
			)
			AND NOT EXISTS (
				SELECT 1 FROM friendships fx
				WHERE fx.status = 'accepted'
					AND ((fx.user_a_id = $1 AND fx.user_b_id = u.id) OR (fx.user_b_id = $1 AND fx.user_a_id = u.id))
			)`

const datingMatchSelectSQL = `SELECT
			dm.id,
			dm.chat_id,
			dm.status,
			dm.matched_at,
			dm.unmatched_at,
			` + datingUserColumns + `
		FROM dating_matches dm
		JOIN users u ON u.id = CASE WHEN dm.user_a_id = $1 THEN dm.user_b_id ELSE dm.user_a_id END
		LEFT JOIN friendships f
			ON ((f.user_a_id = $1 AND f.user_b_id = u.id) OR (f.user_b_id = $1 AND f.user_a_id = u.id))
		LEFT JOIN LATERAL (
			SELECT array_agg(i.name ORDER BY i.name) AS items
			FROM user_interests ui
			JOIN interests i ON i.id = ui.interest_id
			WHERE ui.user_id = u.id
		) interest_names ON true`

func loadDatingMatch(ctx context.Context, q querier, userID, matchID uuid.UUID) (*DatingMatch, error) {
	rows, err := q.Query(ctx, datingMatchSelectSQL+`
		WHERE dm.id = $2
			AND (dm.user_a_id = $1 OR dm.user_b_id = $1)
			AND u.deleted_at IS NULL`,
		userID, matchID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	matches, err := scanDatingMatches(rows)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &matches[0], nil
}

func scanDatingLikes(rows pgx.Rows) ([]DatingLike, error) {
	likes := []DatingLike{}
	for rows.Next() {
		var like DatingLike
		if err := rows.Scan(
			&like.LikedAt,
			&like.User.ID,
			&like.User.Username,
			&like.User.AvatarURL,
			&like.User.IsPlus,
			&like.User.SubscriptionTier,
			&like.User.SubscriptionStatus,
			&like.User.City,
			&like.User.Country,
			&like.User.Bio,
			&like.User.Interests,
			&like.User.ConnectionIntents,
			&like.User.Gender,
			&like.User.BirthDate,
			&like.User.SoberSince,
			&like.User.CreatedAt,
			&like.User.FriendshipStatus,
			&like.User.FriendCount,
			&like.User.IncomingFriendRequestCt,
			&like.User.OutgoingFriendRequestCt,
			&like.User.CurrentCity,
			&like.User.LocationUpdatedAt,
		); err != nil {
			return nil, err
		}
		likes = append(likes, like)
	}
	return likes, rows.Err()
}

func scanDatingMatches(rows pgx.Rows) ([]DatingMatch, error) {
	matches := []DatingMatch{}
	for rows.Next() {
		var match DatingMatch
		if err := rows.Scan(
			&match.ID,
			&match.ChatID,
			&match.Status,
			&match.MatchedAt,
			&match.UnmatchedAt,
			&match.User.ID,
			&match.User.Username,
			&match.User.AvatarURL,
			&match.User.IsPlus,
			&match.User.SubscriptionTier,
			&match.User.SubscriptionStatus,
			&match.User.City,
			&match.User.Country,
			&match.User.Bio,
			&match.User.Interests,
			&match.User.ConnectionIntents,
			&match.User.Gender,
			&match.User.BirthDate,
			&match.User.SoberSince,
			&match.User.CreatedAt,
			&match.User.FriendshipStatus,
			&match.User.FriendCount,
			&match.User.IncomingFriendRequestCt,
			&match.User.OutgoingFriendRequestCt,
			&match.User.CurrentCity,
			&match.User.LocationUpdatedAt,
		); err != nil {
			return nil, err
		}
		matches = append(matches, match)
	}
	return matches, rows.Err()
}

func scanDatingUsers(rows pgx.Rows) ([]user.User, error) {
	users := []user.User{}
	for rows.Next() {
		var u user.User
		if err := rows.Scan(
			&u.ID,
			&u.Username,
			&u.AvatarURL,
			&u.IsPlus,
			&u.SubscriptionTier,
			&u.SubscriptionStatus,
			&u.City,
			&u.Country,
			&u.Bio,
			&u.Interests,
			&u.ConnectionIntents,
			&u.Gender,
			&u.BirthDate,
			&u.SoberSince,
			&u.CreatedAt,
			&u.FriendshipStatus,
			&u.FriendCount,
			&u.IncomingFriendRequestCt,
			&u.OutgoingFriendRequestCt,
			&u.CurrentCity,
			&u.LocationUpdatedAt,
		); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func scanDatingCandidates(rows pgx.Rows) ([]datingCandidate, error) {
	candidates := []datingCandidate{}
	for rows.Next() {
		var candidate datingCandidate
		if err := rows.Scan(
			&candidate.User.ID,
			&candidate.User.Username,
			&candidate.User.AvatarURL,
			&candidate.User.IsPlus,
			&candidate.User.SubscriptionTier,
			&candidate.User.SubscriptionStatus,
			&candidate.User.City,
			&candidate.User.Country,
			&candidate.User.Bio,
			&candidate.User.Interests,
			&candidate.User.ConnectionIntents,
			&candidate.User.Gender,
			&candidate.User.BirthDate,
			&candidate.User.SoberSince,
			&candidate.User.CreatedAt,
			&candidate.User.FriendshipStatus,
			&candidate.User.FriendCount,
			&candidate.User.IncomingFriendRequestCt,
			&candidate.User.OutgoingFriendRequestCt,
			&candidate.User.CurrentCity,
			&candidate.User.LocationUpdatedAt,
			&candidate.DistanceKm,
			&candidate.SharedInterestCount,
			&candidate.SobrietyBand,
			&candidate.ProfileCompleteness,
			&candidate.LastActiveAt,
			&candidate.RecentImpressionAt,
		); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}
