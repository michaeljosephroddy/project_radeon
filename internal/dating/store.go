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

func (s *pgStore) ensureDatingProfile(ctx context.Context, userID uuid.UUID) error {
	var hasDating bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM users
			WHERE id = $1
				AND deleted_at IS NULL
				AND connection_intents @> ARRAY['dating']::text[]
		)`,
		userID,
	).Scan(&hasDating); err != nil {
		return err
	}
	if !hasDating {
		return ErrDatingDisabled
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO dating_profiles (user_id)
		VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING`,
		userID,
	)
	return err
}

func loadDatingProfileByUser(ctx context.Context, q querier, userID uuid.UUID) (*DatingProfile, error) {
	rows, err := q.Query(ctx, datingProfileSelectSQL+` WHERE u.id = $1 AND u.deleted_at IS NULL`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles, err := scanDatingProfiles(rows)
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, pgx.ErrNoRows
	}
	if err := attachDatingProfilePhotos(ctx, q, profiles); err != nil {
		return nil, err
	}
	if err := attachDatingProfilePromptAnswers(ctx, q, profiles); err != nil {
		return nil, err
	}
	return &profiles[0], nil
}

func (s *pgStore) ListInterests(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT name FROM interests ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	interests := make([]string, 0)
	for rows.Next() {
		var interest string
		if err := rows.Scan(&interest); err != nil {
			return nil, err
		}
		interests = append(interests, interest)
	}
	return interests, rows.Err()
}

func loadDatingProfileByID(ctx context.Context, q querier, profileID uuid.UUID) (*DatingProfile, error) {
	rows, err := q.Query(ctx, datingProfileSelectSQL+` WHERE dp.id = $1 AND u.deleted_at IS NULL`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles, err := scanDatingProfiles(rows)
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, pgx.ErrNoRows
	}
	if err := attachDatingProfilePhotos(ctx, q, profiles); err != nil {
		return nil, err
	}
	if err := attachDatingProfilePromptAnswers(ctx, q, profiles); err != nil {
		return nil, err
	}
	return &profiles[0], nil
}

func datingProfileIsComplete(ctx context.Context, q querier, userID uuid.UUID) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx,
		`SELECT
			NULLIF(dp.bio, '') IS NOT NULL
			AND dp.relationship_goal <> ''
			AND cardinality(dp.interested_in_genders) > 0
			AND EXISTS (SELECT 1 FROM dating_profile_interests dpi WHERE dpi.profile_id = dp.id)
			AND EXISTS (SELECT 1 FROM dating_profile_photos p WHERE p.profile_id = dp.id)
		FROM dating_profiles dp
		WHERE dp.user_id = $1`,
		userID,
	).Scan(&ok)
	return ok, err
}

func normalizeDatingPhotoPositions(ctx context.Context, q querier, profileID uuid.UUID) error {
	_, err := q.Exec(ctx,
		`WITH ordered AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY position, created_at) - 1 AS next_position
			FROM dating_profile_photos
			WHERE profile_id = $1
		)
		UPDATE dating_profile_photos p
		SET position = ordered.next_position
		FROM ordered
		WHERE p.id = ordered.id`,
		profileID,
	)
	return err
}

func (s *pgStore) GetMyProfile(ctx context.Context, userID uuid.UUID) (*DatingProfile, error) {
	if err := s.ensureDatingProfile(ctx, userID); err != nil {
		return nil, err
	}
	return loadDatingProfileByUser(ctx, s.pool, userID)
}

func (s *pgStore) GetProfile(ctx context.Context, viewerID, profileID uuid.UUID) (*DatingProfile, error) {
	profile, err := loadDatingProfileByID(ctx, s.pool, profileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if profile.CompletedAt == nil || profile.Paused {
		return nil, ErrNotFound
	}
	if profile.UserID == viewerID {
		return profile, nil
	}
	if err := validateDatingPair(ctx, s.pool, viewerID, profile.UserID); err != nil {
		return nil, err
	}
	return profile, nil
}

func (s *pgStore) UpdateMyProfile(ctx context.Context, userID uuid.UUID, input UpdateProfileInput) (*DatingProfile, error) {
	if err := s.ensureDatingProfile(ctx, userID); err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var profileID uuid.UUID
	var wasComplete bool
	if err := tx.QueryRow(ctx, `SELECT id, completed_at IS NOT NULL FROM dating_profiles WHERE user_id = $1`, userID).Scan(&profileID, &wasComplete); err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx,
		`UPDATE dating_profiles
		SET bio = CASE WHEN $2::text IS NULL THEN bio ELSE NULLIF($2::text, '') END,
			relationship_goal = CASE WHEN $3::text IS NULL THEN relationship_goal ELSE $3::text END,
			interested_in_genders = CASE WHEN NOT $4 THEN interested_in_genders ELSE $5::text[] END,
			age_min = COALESCE($6::int, age_min),
			age_max = COALESCE($7::int, age_max),
			distance_km = COALESCE($8::int, distance_km),
			paused = COALESCE($9::bool, paused),
			height_cm = CASE WHEN NOT $10 THEN height_cm ELSE $11::int END,
			job_title = CASE WHEN $12::text IS NULL THEN job_title ELSE NULLIF($12::text, '') END,
			company = CASE WHEN $13::text IS NULL THEN company ELSE NULLIF($13::text, '') END,
			work = CASE
				WHEN $12::text IS NULL AND $13::text IS NULL THEN work
				ELSE NULLIF(concat_ws(
					' @ ',
					NULLIF(COALESCE($12::text, job_title), ''),
					NULLIF(COALESCE($13::text, company), '')
				), '')
			END,
			school = CASE WHEN $14::text IS NULL THEN school ELSE NULLIF($14::text, '') END,
			course = CASE WHEN $15::text IS NULL THEN course ELSE NULLIF($15::text, '') END,
			education = CASE
				WHEN $14::text IS NULL AND $15::text IS NULL THEN education
				ELSE NULLIF(concat_ws(
					' @ ',
					NULLIF(COALESCE($15::text, course), ''),
					NULLIF(COALESCE($14::text, school), '')
				), '')
			END,
			kids_status = CASE WHEN $16::text IS NULL THEN kids_status ELSE $16::text END,
			children_status = CASE WHEN $17::text IS NULL THEN children_status ELSE $17::text END,
			relationship_type = CASE WHEN $18::text IS NULL THEN relationship_type ELSE $18::text END,
			gender = CASE WHEN $19::text IS NULL THEN gender ELSE $19::text END,
			sexuality = CASE WHEN $20::text IS NULL THEN sexuality ELSE $20::text END,
			pronouns = CASE WHEN $21::text IS NULL THEN pronouns ELSE $21::text END,
			ethnicity = CASE WHEN $22::text IS NULL THEN ethnicity ELSE $22::text END,
			pets = CASE WHEN $23::text IS NULL THEN pets ELSE $23::text END,
			religious_belief = CASE WHEN $24::text IS NULL THEN religious_belief ELSE $24::text END,
			languages_spoken = CASE WHEN NOT $25 THEN languages_spoken ELSE $26::text[] END,
			political_view = CASE WHEN $27::text IS NULL THEN political_view ELSE $27::text END,
			updated_at = NOW()
		WHERE user_id = $1`,
		userID,
		input.Bio,
		input.RelationshipGoal,
		input.ReplaceGenders,
		input.InterestedInGenders,
		input.AgeMin,
		input.AgeMax,
		input.DistanceKm,
		input.Paused,
		input.ReplaceHeight,
		input.HeightCm,
		input.JobTitle,
		input.Company,
		input.School,
		input.Course,
		input.KidsStatus,
		input.ChildrenStatus,
		input.RelationshipType,
		input.Gender,
		input.Sexuality,
		input.Pronouns,
		input.Ethnicity,
		input.Pets,
		input.ReligiousBelief,
		input.ReplaceLanguages,
		input.LanguagesSpoken,
		input.PoliticalView,
	)
	if err != nil {
		return nil, err
	}
	if input.ReplaceInterests {
		if _, err := tx.Exec(ctx, `DELETE FROM dating_profile_interests WHERE profile_id = $1`, profileID); err != nil {
			return nil, err
		}
		if len(input.Interests) > 0 {
			if _, err := tx.Exec(ctx,
				`INSERT INTO dating_profile_interests (profile_id, interest_id)
				SELECT $1, i.id
				FROM interests i
				WHERE i.name = ANY($2::text[])`,
				profileID, input.Interests,
			); err != nil {
				return nil, err
			}
		}
	}
	if input.ReplacePromptAnswers {
		if _, err := tx.Exec(ctx, `DELETE FROM dating_profile_prompt_answers WHERE profile_id = $1`, profileID); err != nil {
			return nil, err
		}
		for index, answer := range input.PromptAnswers {
			if _, err := tx.Exec(ctx,
				`INSERT INTO dating_profile_prompt_answers (profile_id, prompt_key, answer, position)
				VALUES ($1, $2, $3, $4)`,
				profileID, answer.PromptKey, answer.Answer, index,
			); err != nil {
				return nil, err
			}
		}
	}
	complete, err := datingProfileIsComplete(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if !complete && (input.Complete || wasComplete) {
		return nil, ErrProfileIncomplete
	}
	if !complete {
		if _, err := tx.Exec(ctx, `UPDATE dating_profiles SET completed_at = NULL, updated_at = NOW() WHERE user_id = $1`, userID); err != nil {
			return nil, err
		}
	}
	if input.Complete {
		if _, err := tx.Exec(ctx, `UPDATE dating_profiles SET completed_at = COALESCE(completed_at, NOW()), updated_at = NOW() WHERE user_id = $1`, userID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadDatingProfileByUser(ctx, s.pool, userID)
}

func (s *pgStore) AddPhoto(ctx context.Context, userID uuid.UUID, imageURL string, width, height int) (*DatingProfile, error) {
	if strings.TrimSpace(imageURL) == "" || width <= 0 || height <= 0 {
		return nil, ErrForbidden
	}
	if err := s.ensureDatingProfile(ctx, userID); err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var profileID uuid.UUID
	var count int
	if err := tx.QueryRow(ctx, `SELECT id, (SELECT COUNT(*) FROM dating_profile_photos WHERE profile_id = dating_profiles.id) FROM dating_profiles WHERE user_id = $1`, userID).Scan(&profileID, &count); err != nil {
		return nil, err
	}
	if count >= 6 {
		return nil, ErrConflict
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO dating_profile_photos (profile_id, image_url, width, height, position)
		VALUES ($1, $2, $3, $4, $5)`,
		profileID, strings.TrimSpace(imageURL), width, height, count,
	)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE dating_profiles SET updated_at = NOW() WHERE id = $1`, profileID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadDatingProfileByUser(ctx, s.pool, userID)
}

func (s *pgStore) DeletePhoto(ctx context.Context, userID, photoID uuid.UUID) (*DatingProfile, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var profileID uuid.UUID
	err = tx.QueryRow(ctx,
		`DELETE FROM dating_profile_photos p
		USING dating_profiles dp
		WHERE p.id = $2 AND p.profile_id = dp.id AND dp.user_id = $1
		RETURNING dp.id`,
		userID, photoID,
	).Scan(&profileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := normalizeDatingPhotoPositions(ctx, tx, profileID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE dating_profiles SET completed_at = NULL, updated_at = NOW() WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM dating_profile_photos WHERE profile_id = $1)`, profileID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadDatingProfileByUser(ctx, s.pool, userID)
}

func (s *pgStore) ReorderPhotos(ctx context.Context, userID uuid.UUID, photoIDs []uuid.UUID) (*DatingProfile, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS dating_profile_photos_profile_id_position_key DEFERRED`); err != nil {
		return nil, err
	}

	profile, err := loadDatingProfileByUser(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if len(photoIDs) != len(profile.Photos) {
		return nil, ErrForbidden
	}
	tag, err := tx.Exec(ctx,
		`WITH desired AS (
			SELECT id, ordinality::int - 1 AS position
			FROM unnest($2::uuid[]) WITH ORDINALITY AS input(id, ordinality)
		)
		UPDATE dating_profile_photos p
		SET position = desired.position
		FROM desired
		WHERE p.profile_id = $1 AND p.id = desired.id`,
		profile.ID, photoIDs,
	)
	if err != nil {
		return nil, err
	}
	if int(tag.RowsAffected()) != len(profile.Photos) {
		return nil, ErrForbidden
	}
	if _, err := tx.Exec(ctx, `UPDATE dating_profiles SET updated_at = NOW() WHERE id = $1`, profile.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadDatingProfileByUser(ctx, s.pool, userID)
}

func (s *pgStore) Discover(ctx context.Context, params DiscoverParams) ([]DatingProfile, error) {
	ranked, err := s.loadDatingRankedWindow(ctx, params, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return s.discoverProfilesFromRankedCandidates(ctx, params, ranked)
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

func (s *pgStore) discoverProfilesFromRankedCandidates(ctx context.Context, params DiscoverParams, candidates []datingCandidate) ([]DatingProfile, error) {
	offset := params.CursorOffset
	if offset == 0 {
		offset = parseOffset(params.Cursor)
	}
	if offset >= len(candidates) {
		return []DatingProfile{}, nil
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
	profiles := make([]DatingProfile, 0, end-offset)
	for _, candidate := range candidates[offset:end] {
		profiles = append(profiles, candidate.Profile)
	}
	return profiles, nil
}

func (s *pgStore) loadDatingViewerFeatures(ctx context.Context, userID uuid.UUID) (datingViewerFeatures, error) {
	viewer := datingViewerFeatures{UserID: userID}
	err := s.pool.QueryRow(ctx,
		`SELECT sobriety_band
		FROM users u
		JOIN dating_profiles dp ON dp.user_id = u.id
		WHERE u.id = $1
			AND u.deleted_at IS NULL
			AND u.connection_intents @> ARRAY['dating']::text[]
			AND dp.completed_at IS NOT NULL
			AND dp.paused = FALSE`,
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
			orderBy: "candidate_distance_km ASC NULLS LAST, u.last_active_at DESC NULLS LAST, u.id DESC",
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
	if err := attachDatingCandidatePhotos(ctx, s.pool, candidates); err != nil {
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
			candidate.Profile.UserID,
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
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*)
		FROM users u
		JOIN dating_profiles dp ON dp.user_id = u.id `+datingDiscoverWhereSQL, datingDiscoverArgs(params, 0)[:9]...).Scan(&count)
	return count, err
}

func (s *pgStore) ListLikes(ctx context.Context, userID uuid.UUID, before *string, limit int) ([]DatingLike, error) {
	if ok, err := s.userHasPlus(ctx, userID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrPlusRequired
	}

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
	likes, err := scanDatingLikes(rows)
	if err != nil {
		return nil, err
	}
	if err := attachDatingLikePhotos(ctx, s.pool, likes); err != nil {
		return nil, err
	}
	return likes, nil
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
		JOIN dating_profiles dp ON dp.user_id = u.id
		`+datingLikesWhereSQL,
		userID,
	).Scan(&count)
	return count, err
}

func (s *pgStore) RecordAction(ctx context.Context, actorID, targetProfileID uuid.UUID, action string) (*ActionResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	targetProfile, err := loadDatingProfileByID(ctx, tx, targetProfileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTargetUnavailable
	}
	if err != nil {
		return nil, err
	}
	targetID := targetProfile.UserID
	if actorID == targetID {
		return nil, ErrForbidden
	}
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
	matches, err := scanDatingMatches(rows)
	if err != nil {
		return nil, err
	}
	if err := attachDatingMatchPhotos(ctx, s.pool, matches); err != nil {
		return nil, err
	}
	return matches, nil
}

func (s *pgStore) CountUnseenMatches(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*)
		FROM dating_matches dm
		LEFT JOIN dating_match_views dmv ON dmv.user_id = $1
		WHERE dm.status = 'active'
			AND (dm.user_a_id = $1 OR dm.user_b_id = $1)
			AND dm.matched_at > COALESCE(dmv.seen_at, '-infinity'::timestamptz)`,
		userID,
	).Scan(&count)
	return count, err
}

func (s *pgStore) MarkMatchesSeen(ctx context.Context, userID uuid.UUID) (time.Time, error) {
	var seenAt time.Time
	err := s.pool.QueryRow(ctx,
		`INSERT INTO dating_match_views (user_id, seen_at, updated_at)
		VALUES ($1, NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET seen_at = EXCLUDED.seen_at,
			updated_at = EXCLUDED.updated_at
		RETURNING seen_at`,
		userID,
	).Scan(&seenAt)
	return seenAt, err
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

func (s *pgStore) LogEvents(ctx context.Context, userID uuid.UUID, events []DatingEventInput) error {
	if len(events) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	now := time.Now().UTC()
	for _, event := range events {
		if !event.EventType.Valid() {
			return ErrInvalidDatingEvent
		}
		eventAt := event.EventAt
		if eventAt.IsZero() {
			eventAt = now
		}
		payload := []byte("{}")
		if len(event.Payload) > 0 {
			payload = event.Payload
		}
		var position any
		if event.Position != nil {
			position = *event.Position
		}
		batch.Queue(
			`INSERT INTO dating_events (
				user_id,
				profile_id,
				match_id,
				event_type,
				position,
				event_at,
				payload
			) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`,
			userID,
			event.ProfileID,
			event.MatchID,
			string(event.EventType),
			position,
			eventAt.UTC(),
			payload,
		)
	}
	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range events {
		if _, err := results.Exec(); err != nil {
			return err
		}
	}
	return nil
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

func (s *pgStore) userHasPlus(ctx context.Context, userID uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM users
			WHERE id = $1
				AND deleted_at IS NULL
				AND subscription_tier = 'plus'
				AND subscription_status = 'active'
		)`,
		userID,
	).Scan(&ok)
	return ok, err
}

func validateDatingPair(ctx context.Context, q querier, actorID, targetID uuid.UUID) error {
	var actorDating, targetDating, actorComplete, targetComplete, actorPaused, targetPaused, mutualPreference, blocked, acceptedFriends bool
	err := q.QueryRow(ctx,
		`SELECT
			EXISTS(SELECT 1 FROM users WHERE id = $1 AND deleted_at IS NULL AND connection_intents @> ARRAY['dating']::text[]),
			EXISTS(SELECT 1 FROM users WHERE id = $2 AND deleted_at IS NULL AND connection_intents @> ARRAY['dating']::text[]),
			EXISTS(SELECT 1 FROM dating_profiles WHERE user_id = $1 AND completed_at IS NOT NULL),
			EXISTS(SELECT 1 FROM dating_profiles WHERE user_id = $2 AND completed_at IS NOT NULL),
			EXISTS(SELECT 1 FROM dating_profiles WHERE user_id = $1 AND paused = TRUE),
			EXISTS(SELECT 1 FROM dating_profiles WHERE user_id = $2 AND paused = TRUE),
			EXISTS(
				SELECT 1
				FROM users actor
				JOIN users target ON target.id = $2
				JOIN dating_profiles actor_dp ON actor_dp.user_id = actor.id
				JOIN dating_profiles target_dp ON target_dp.user_id = target.id
				WHERE actor.id = $1
					AND (
						actor.gender IS NULL
						OR cardinality(target_dp.interested_in_genders) = 0
						OR target_dp.interested_in_genders @> ARRAY[actor.gender]::text[]
					)
					AND (
						target.gender IS NULL
						OR cardinality(actor_dp.interested_in_genders) = 0
						OR actor_dp.interested_in_genders @> ARRAY[target.gender]::text[]
					)
			),
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
	).Scan(&actorDating, &targetDating, &actorComplete, &targetComplete, &actorPaused, &targetPaused, &mutualPreference, &blocked, &acceptedFriends)
	if err != nil {
		return err
	}
	if !actorDating {
		return ErrDatingDisabled
	}
	if !actorComplete || actorPaused {
		return ErrProfileIncomplete
	}
	if !targetDating || !targetComplete || targetPaused {
		return ErrTargetUnavailable
	}
	if !mutualPreference {
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

const datingProfileColumns = `
			dp.id,
			u.id,
			u.username,
			CASE
				WHEN u.birth_date IS NULL THEN NULL
				ELSE EXTRACT(YEAR FROM AGE(CURRENT_DATE, u.birth_date))::int
			END AS age,
			u.city,
			u.country,
			dp.bio,
			dp.relationship_goal,
			dp.interested_in_genders,
			dp.height_cm,
			dp.job_title,
			dp.company,
			CASE
				WHEN NULLIF(dp.job_title, '') IS NULL AND NULLIF(dp.company, '') IS NULL THEN dp.work
				ELSE NULLIF(concat_ws(' @ ', NULLIF(dp.job_title, ''), NULLIF(dp.company, '')), '')
			END AS work,
			dp.school,
			dp.course,
			CASE
				WHEN NULLIF(dp.course, '') IS NULL AND NULLIF(dp.school, '') IS NULL THEN dp.education
				ELSE NULLIF(concat_ws(' @ ', NULLIF(dp.course, ''), NULLIF(dp.school, '')), '')
			END AS education,
			dp.kids_status,
			dp.children_status,
			dp.relationship_type,
			dp.gender,
			dp.sexuality,
			dp.pronouns,
			dp.ethnicity,
			dp.pets,
			dp.religious_belief,
			dp.languages_spoken,
			dp.political_view,
			COALESCE(interest_names.items, '{}') AS interests,
			dp.age_min,
			dp.age_max,
			dp.distance_km,
			dp.paused,
			dp.completed_at,
			dp.created_at,
			dp.updated_at`

const datingProfileInterestNamesJoinSQL = `
		LEFT JOIN LATERAL (
			SELECT array_agg(i.name ORDER BY i.name) AS items
			FROM dating_profile_interests dpi
			JOIN interests i ON i.id = dpi.interest_id
			WHERE dpi.profile_id = dp.id
		) interest_names ON true`

const datingProfileSelectSQL = `SELECT` + datingProfileColumns + `
		FROM dating_profiles dp
		JOIN users u ON u.id = dp.user_id` + datingProfileInterestNamesJoinSQL

const datingDiscoverSelectSQL = `SELECT` + datingProfileColumns + `
		FROM users u
		JOIN dating_profiles dp ON dp.user_id = u.id
		LEFT JOIN friendships f
			ON ((f.user_a_id = $1 AND f.user_b_id = u.id) OR (f.user_b_id = $1 AND f.user_a_id = u.id))` + datingProfileInterestNamesJoinSQL + `
		`

const datingDistanceSQL = `CASE
				WHEN $6::float8 IS NULL OR $7::float8 IS NULL OR u.discover_lat IS NULL OR u.discover_lng IS NULL THEN NULL
				ELSE 2.0 * 6371.0 * ASIN(SQRT(
					POWER(SIN(RADIANS((u.discover_lat - $6::float8) / 2.0)), 2)
					+ COS(RADIANS($6::float8)) * COS(RADIANS(u.discover_lat))
					* POWER(SIN(RADIANS((u.discover_lng - $7::float8) / 2.0)), 2)
				))
			END`

const datingCandidateColumns = datingProfileColumns + `,
			` + datingDistanceSQL + ` AS candidate_distance_km,
			COALESCE(shared_interests.count, 0)::int AS shared_interest_count,
			u.sobriety_band,
			u.profile_completeness,
			u.last_active_at,
			recent_impression.shown_at`

const datingDiscoverCandidateSelectSQL = `SELECT` + datingCandidateColumns + `
		FROM users u
		JOIN dating_profiles dp ON dp.user_id = u.id
		LEFT JOIN friendships f
			ON ((f.user_a_id = $1 AND f.user_b_id = u.id) OR (f.user_b_id = $1 AND f.user_a_id = u.id))` + datingProfileInterestNamesJoinSQL + `
		LEFT JOIN LATERAL (
			SELECT COUNT(DISTINCT dpi.interest_id)::int AS count
			FROM dating_profile_interests dpi
			JOIN dating_profiles viewer_dp ON viewer_dp.user_id = $1
			JOIN dating_profile_interests viewer_dpi
				ON viewer_dpi.profile_id = viewer_dp.id
				AND viewer_dpi.interest_id = dpi.interest_id
			WHERE dpi.profile_id = dp.id
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
			AND dp.completed_at IS NOT NULL
			AND dp.paused = FALSE
			AND EXISTS (
				SELECT 1 FROM users viewer
				JOIN dating_profiles viewer_dp ON viewer_dp.user_id = viewer.id
				WHERE viewer.id = $1
					AND viewer.deleted_at IS NULL
					AND viewer.connection_intents @> ARRAY['dating']::text[]
					AND viewer_dp.completed_at IS NOT NULL
					AND viewer_dp.paused = FALSE
					AND (
						viewer.gender IS NULL
						OR cardinality(dp.interested_in_genders) = 0
						OR dp.interested_in_genders @> ARRAY[viewer.gender]::text[]
					)
					AND (
						u.gender IS NULL
						OR cardinality(viewer_dp.interested_in_genders) = 0
						OR viewer_dp.interested_in_genders @> ARRAY[u.gender]::text[]
					)
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
					FROM dating_profile_interests dpi
					JOIN interests i ON i.id = dpi.interest_id
					WHERE dpi.profile_id = dp.id
						AND i.name = ANY($9::text[])
				)
			)`

const datingLikesSelectSQL = `SELECT
			da.updated_at,
			` + datingProfileColumns + `
		FROM dating_actions da
		JOIN users u ON u.id = da.actor_id
		JOIN dating_profiles dp ON dp.user_id = u.id
		LEFT JOIN friendships f
			ON ((f.user_a_id = $1 AND f.user_b_id = u.id) OR (f.user_b_id = $1 AND f.user_a_id = u.id))` + datingProfileInterestNamesJoinSQL + `
		`

const datingLikesWhereSQL = `
		WHERE da.target_id = $1
			AND da.action = 'like'
			AND u.deleted_at IS NULL
			AND u.connection_intents @> ARRAY['dating']::text[]
			AND dp.completed_at IS NOT NULL
			AND dp.paused = FALSE
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
			` + datingProfileColumns + `
		FROM dating_matches dm
		JOIN users u ON u.id = CASE WHEN dm.user_a_id = $1 THEN dm.user_b_id ELSE dm.user_a_id END
		JOIN dating_profiles dp ON dp.user_id = u.id
		LEFT JOIN friendships f
			ON ((f.user_a_id = $1 AND f.user_b_id = u.id) OR (f.user_b_id = $1 AND f.user_a_id = u.id))` + datingProfileInterestNamesJoinSQL + `
		`

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
	if err := attachDatingMatchPhotos(ctx, q, matches); err != nil {
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
			&like.Profile.ID,
			&like.Profile.UserID,
			&like.Profile.Username,
			&like.Profile.Age,
			&like.Profile.City,
			&like.Profile.Country,
			&like.Profile.Bio,
			&like.Profile.RelationshipGoal,
			&like.Profile.InterestedInGenders,
			&like.Profile.HeightCm,
			&like.Profile.JobTitle,
			&like.Profile.Company,
			&like.Profile.Work,
			&like.Profile.School,
			&like.Profile.Course,
			&like.Profile.Education,
			&like.Profile.KidsStatus,
			&like.Profile.ChildrenStatus,
			&like.Profile.RelationshipType,
			&like.Profile.Gender,
			&like.Profile.Sexuality,
			&like.Profile.Pronouns,
			&like.Profile.Ethnicity,
			&like.Profile.Pets,
			&like.Profile.ReligiousBelief,
			&like.Profile.LanguagesSpoken,
			&like.Profile.PoliticalView,
			&like.Profile.Interests,
			&like.Profile.AgeMin,
			&like.Profile.AgeMax,
			&like.Profile.DistanceKm,
			&like.Profile.Paused,
			&like.Profile.CompletedAt,
			&like.Profile.CreatedAt,
			&like.Profile.UpdatedAt,
		); err != nil {
			return nil, err
		}
		likes = append(likes, like)
	}
	return likes, rows.Err()
}

func attachDatingLikePhotos(ctx context.Context, q querier, likes []DatingLike) error {
	profiles := make([]DatingProfile, 0, len(likes))
	for _, like := range likes {
		profiles = append(profiles, like.Profile)
	}
	if err := attachDatingProfilePhotos(ctx, q, profiles); err != nil {
		return err
	}
	if err := attachDatingProfilePromptAnswers(ctx, q, profiles); err != nil {
		return err
	}
	for index := range likes {
		likes[index].Profile.Photos = profiles[index].Photos
		likes[index].Profile.PromptAnswers = profiles[index].PromptAnswers
	}
	return nil
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
			&match.Profile.ID,
			&match.Profile.UserID,
			&match.Profile.Username,
			&match.Profile.Age,
			&match.Profile.City,
			&match.Profile.Country,
			&match.Profile.Bio,
			&match.Profile.RelationshipGoal,
			&match.Profile.InterestedInGenders,
			&match.Profile.HeightCm,
			&match.Profile.JobTitle,
			&match.Profile.Company,
			&match.Profile.Work,
			&match.Profile.School,
			&match.Profile.Course,
			&match.Profile.Education,
			&match.Profile.KidsStatus,
			&match.Profile.ChildrenStatus,
			&match.Profile.RelationshipType,
			&match.Profile.Gender,
			&match.Profile.Sexuality,
			&match.Profile.Pronouns,
			&match.Profile.Ethnicity,
			&match.Profile.Pets,
			&match.Profile.ReligiousBelief,
			&match.Profile.LanguagesSpoken,
			&match.Profile.PoliticalView,
			&match.Profile.Interests,
			&match.Profile.AgeMin,
			&match.Profile.AgeMax,
			&match.Profile.DistanceKm,
			&match.Profile.Paused,
			&match.Profile.CompletedAt,
			&match.Profile.CreatedAt,
			&match.Profile.UpdatedAt,
		); err != nil {
			return nil, err
		}
		matches = append(matches, match)
	}
	return matches, rows.Err()
}

func attachDatingMatchPhotos(ctx context.Context, q querier, matches []DatingMatch) error {
	profiles := make([]DatingProfile, 0, len(matches))
	for _, match := range matches {
		profiles = append(profiles, match.Profile)
	}
	if err := attachDatingProfilePhotos(ctx, q, profiles); err != nil {
		return err
	}
	if err := attachDatingProfilePromptAnswers(ctx, q, profiles); err != nil {
		return err
	}
	for index := range matches {
		matches[index].Profile.Photos = profiles[index].Photos
		matches[index].Profile.PromptAnswers = profiles[index].PromptAnswers
	}
	return nil
}

func attachDatingCandidatePhotos(ctx context.Context, q querier, candidates []datingCandidate) error {
	profiles := make([]DatingProfile, 0, len(candidates))
	for _, candidate := range candidates {
		profiles = append(profiles, candidate.Profile)
	}
	if err := attachDatingProfilePhotos(ctx, q, profiles); err != nil {
		return err
	}
	if err := attachDatingProfilePromptAnswers(ctx, q, profiles); err != nil {
		return err
	}
	for index := range candidates {
		candidates[index].Profile.Photos = profiles[index].Photos
		candidates[index].Profile.PromptAnswers = profiles[index].PromptAnswers
	}
	return nil
}

func attachDatingProfilePhotos(ctx context.Context, q querier, profiles []DatingProfile) error {
	if len(profiles) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(profiles))
	indexByID := make(map[uuid.UUID]int, len(profiles))
	for index, profile := range profiles {
		ids = append(ids, profile.ID)
		indexByID[profile.ID] = index
		profiles[index].Photos = []DatingPhoto{}
	}
	rows, err := q.Query(ctx,
		`SELECT id, profile_id, image_url, width, height, position, created_at
		FROM dating_profile_photos
		WHERE profile_id = ANY($1::uuid[])
		ORDER BY profile_id, position, created_at`,
		ids,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var profileID uuid.UUID
		var photo DatingPhoto
		if err := rows.Scan(&photo.ID, &profileID, &photo.ImageURL, &photo.Width, &photo.Height, &photo.Position, &photo.CreatedAt); err != nil {
			return err
		}
		if index, ok := indexByID[profileID]; ok {
			profiles[index].Photos = append(profiles[index].Photos, photo)
		}
	}
	return rows.Err()
}

func attachDatingProfilePromptAnswers(ctx context.Context, q querier, profiles []DatingProfile) error {
	if len(profiles) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(profiles))
	indexByID := make(map[uuid.UUID]int, len(profiles))
	for index, profile := range profiles {
		ids = append(ids, profile.ID)
		indexByID[profile.ID] = index
		profiles[index].PromptAnswers = []DatingPromptAnswer{}
	}
	rows, err := q.Query(ctx,
		`SELECT id, profile_id, prompt_key, answer, position, created_at, updated_at
		FROM dating_profile_prompt_answers
		WHERE profile_id = ANY($1::uuid[])
		ORDER BY profile_id, position, created_at`,
		ids,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var profileID uuid.UUID
		var answer DatingPromptAnswer
		if err := rows.Scan(&answer.ID, &profileID, &answer.PromptKey, &answer.Answer, &answer.Position, &answer.CreatedAt, &answer.UpdatedAt); err != nil {
			return err
		}
		if index, ok := indexByID[profileID]; ok {
			profiles[index].PromptAnswers = append(profiles[index].PromptAnswers, answer)
		}
	}
	return rows.Err()
}

func scanDatingProfiles(rows pgx.Rows) ([]DatingProfile, error) {
	profiles := []DatingProfile{}
	for rows.Next() {
		var profile DatingProfile
		if err := rows.Scan(
			&profile.ID,
			&profile.UserID,
			&profile.Username,
			&profile.Age,
			&profile.City,
			&profile.Country,
			&profile.Bio,
			&profile.RelationshipGoal,
			&profile.InterestedInGenders,
			&profile.HeightCm,
			&profile.JobTitle,
			&profile.Company,
			&profile.Work,
			&profile.School,
			&profile.Course,
			&profile.Education,
			&profile.KidsStatus,
			&profile.ChildrenStatus,
			&profile.RelationshipType,
			&profile.Gender,
			&profile.Sexuality,
			&profile.Pronouns,
			&profile.Ethnicity,
			&profile.Pets,
			&profile.ReligiousBelief,
			&profile.LanguagesSpoken,
			&profile.PoliticalView,
			&profile.Interests,
			&profile.AgeMin,
			&profile.AgeMax,
			&profile.DistanceKm,
			&profile.Paused,
			&profile.CompletedAt,
			&profile.CreatedAt,
			&profile.UpdatedAt,
		); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func scanDatingCandidates(rows pgx.Rows) ([]datingCandidate, error) {
	candidates := []datingCandidate{}
	for rows.Next() {
		var candidate datingCandidate
		if err := rows.Scan(
			&candidate.Profile.ID,
			&candidate.Profile.UserID,
			&candidate.Profile.Username,
			&candidate.Profile.Age,
			&candidate.Profile.City,
			&candidate.Profile.Country,
			&candidate.Profile.Bio,
			&candidate.Profile.RelationshipGoal,
			&candidate.Profile.InterestedInGenders,
			&candidate.Profile.HeightCm,
			&candidate.Profile.JobTitle,
			&candidate.Profile.Company,
			&candidate.Profile.Work,
			&candidate.Profile.School,
			&candidate.Profile.Course,
			&candidate.Profile.Education,
			&candidate.Profile.KidsStatus,
			&candidate.Profile.ChildrenStatus,
			&candidate.Profile.RelationshipType,
			&candidate.Profile.Gender,
			&candidate.Profile.Sexuality,
			&candidate.Profile.Pronouns,
			&candidate.Profile.Ethnicity,
			&candidate.Profile.Pets,
			&candidate.Profile.ReligiousBelief,
			&candidate.Profile.LanguagesSpoken,
			&candidate.Profile.PoliticalView,
			&candidate.Profile.Interests,
			&candidate.Profile.AgeMin,
			&candidate.Profile.AgeMax,
			&candidate.Profile.DistanceKm,
			&candidate.Profile.Paused,
			&candidate.Profile.CompletedAt,
			&candidate.Profile.CreatedAt,
			&candidate.Profile.UpdatedAt,
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
