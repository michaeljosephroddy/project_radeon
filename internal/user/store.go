package user

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type pgStore struct {
	pool *pgxpool.Pool
}

const discoverProfileCompletenessExpr = `(
	CASE WHEN NULLIF(u.avatar_url, '') IS NOT NULL THEN 1 ELSE 0 END
	+ CASE WHEN NULLIF(u.city, '') IS NOT NULL THEN 1 ELSE 0 END
	+ CASE WHEN NULLIF(u.country, '') IS NOT NULL THEN 1 ELSE 0 END
	+ CASE WHEN NULLIF(u.bio, '') IS NOT NULL THEN 1 ELSE 0 END
	+ CASE WHEN NULLIF(u.gender, '') IS NOT NULL THEN 1 ELSE 0 END
	+ CASE WHEN u.birth_date IS NOT NULL THEN 1 ELSE 0 END
	+ CASE WHEN u.sober_since IS NOT NULL THEN 1 ELSE 0 END
	+ CASE
		WHEN EXISTS (SELECT 1 FROM user_interests ui WHERE ui.user_id = u.id) THEN 1
		ELSE 0
	  END
	+ CASE WHEN cardinality(u.connection_intents) > 0 THEN 1 ELSE 0 END
)::smallint`

// NewPgStore wraps a pgxpool.Pool as the production Querier implementation.
func NewPgStore(pool *pgxpool.Pool) Querier {
	return &pgStore{pool: pool}
}

func (s *pgStore) GetUser(ctx context.Context, viewerID, userID uuid.UUID) (*User, error) {
	var u User
	// Centralising the profile query keeps /users/me and /users/{id} in sync and
	// avoids subtly diverging response fields over time.
	err := s.pool.QueryRow(ctx,
		`SELECT
			u.id,
			u.username,
			u.avatar_url,
			u.banner_url,
			(u.subscription_tier = 'plus' AND u.subscription_status = 'active') AS is_plus,
			u.subscription_tier,
			u.subscription_status,
			u.onboarding_completed_at,
			u.identity_verification_status,
			u.identity_verified_at,
			u.identity_verification_last_error,
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
				WHEN u.id = $1 THEN 'self'
				WHEN f.status = 'accepted' THEN 'friends'
				WHEN f.requester_id = $1 THEN 'outgoing'
				WHEN f.requester_id = u.id THEN 'incoming'
				ELSE 'none'
			END AS friendship_status,
			u.friend_count,
			ic.cnt AS incoming_friend_request_count,
			oc.cnt AS outgoing_friend_request_count,
			u.current_city,
			u.current_country,
			u.location_updated_at
		FROM users u
		LEFT JOIN friendships f
			ON (
				(f.user_a_id = $1 AND f.user_b_id = u.id)
				OR (f.user_b_id = $1 AND f.user_a_id = u.id)
			)
		LEFT JOIN LATERAL (
			SELECT array_agg(i.name ORDER BY i.name) AS items
			FROM user_interests ui
			JOIN interests i ON i.id = ui.interest_id
			WHERE ui.user_id = u.id
		) interest_names ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS cnt
			FROM friendships f3
			WHERE (f3.user_a_id = u.id OR f3.user_b_id = u.id)
				AND f3.status = 'pending'
				AND u.id = $1
				AND f3.requester_id != u.id
		) ic ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS cnt
			FROM friendships f4
			WHERE (f4.user_a_id = u.id OR f4.user_b_id = u.id)
				AND f4.status = 'pending'
				AND u.id = $1
				AND f4.requester_id = u.id
		) oc ON true
		WHERE u.id = $2
			AND u.deleted_at IS NULL`,
		viewerID, userID,
	).Scan(
		&u.ID, &u.Username, &u.AvatarURL, &u.BannerURL, &u.IsPlus, &u.SubscriptionTier, &u.SubscriptionStatus,
		&u.OnboardingCompletedAt, &u.IdentityVerificationStatus, &u.IdentityVerifiedAt, &u.IdentityVerificationLastError,
		&u.City, &u.Country, &u.Bio, &u.Interests, &u.ConnectionIntents, &u.Gender, &u.BirthDate, &u.SoberSince, &u.CreatedAt,
		&u.FriendshipStatus, &u.FriendCount, &u.IncomingFriendRequestCt, &u.OutgoingFriendRequestCt,
		&u.CurrentCity, &u.CurrentCountry, &u.LocationUpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *pgStore) UsernameExistsForOthers(ctx context.Context, username string, userID uuid.UUID) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE username = $1 AND id != $2 AND deleted_at IS NULL)`,
		username, userID,
	).Scan(&exists)
	return exists, err
}

func (s *pgStore) UpdateUser(ctx context.Context, userID uuid.UUID, username, city, country, gender, bio *string, soberSince *time.Time, replaceSoberSince bool, birthDate *time.Time, replaceBirthDate bool, interests []string, replaceInterests bool, connectionIntents []string, replaceConnectionIntents bool, lat, lng *float64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`UPDATE users
		SET
			username = COALESCE($1, username),
			city = COALESCE($2, city),
			country = COALESCE($3, country),
			gender = CASE
				WHEN $4::text IS NULL THEN gender
				ELSE NULLIF($4::text, '')
			END,
			sober_since = CASE
				WHEN NOT $6 THEN sober_since
				ELSE $7::date
			END,
			bio = CASE
				WHEN $5::text IS NULL THEN bio
				ELSE NULLIF($5::text, '')
			END,
			birth_date = CASE
				WHEN NOT $8 THEN birth_date
				ELSE $9::date
			END,
			lat = COALESCE($11::float8, lat),
			lng = COALESCE($12::float8, lng),
			connection_intents = CASE
				WHEN NOT $13 THEN connection_intents
				ELSE $14::text[]
			END
		WHERE id = $10`,
		username, city, country, gender, bio, replaceSoberSince, soberSince, replaceBirthDate, birthDate, userID, lat, lng, replaceConnectionIntents, connectionIntents,
	)
	if err != nil {
		return err
	}

	if replaceInterests {
		if _, err := tx.Exec(ctx, `DELETE FROM user_interests WHERE user_id = $1`, userID); err != nil {
			return err
		}

		if len(interests) > 0 {
			if _, err := tx.Exec(ctx,
				`INSERT INTO user_interests (user_id, interest_id)
				SELECT $1, i.id
				FROM interests i
				WHERE i.name = ANY($2::text[])`,
				userID, interests,
			); err != nil {
				return err
			}
		}
	}

	if err := s.syncDiscoverUserStateTx(ctx, tx, userID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *pgStore) UpdateCurrentLocation(ctx context.Context, userID uuid.UUID, lat, lng float64, city, country string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users
			SET
				current_lat = $2,
				current_lng = $3,
				current_city = $4,
				current_country = $5,
				location_updated_at = NOW(),
				discover_lat = $2,
				discover_lng = $3
			WHERE id = $1`,
		userID, lat, lng, city, country,
	)
	return err
}

func (s *pgStore) DeleteCurrentUser(ctx context.Context, userID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	deletedUsername := "deleted." + strings.ReplaceAll(userID.String(), "-", "")[:12]
	deletedEmail := "deleted+" + userID.String() + "@deleted.local"

	if _, err := tx.Exec(ctx, `DELETE FROM notification_deliveries WHERE user_device_id IN (SELECT id FROM user_devices WHERE user_id = $1)`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_devices WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM notification_counters WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM notification_preferences WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM notifications WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_interests WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM group_memberships WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM group_join_requests WHERE user_id = $1`, userID); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx,
		`UPDATE users
		SET
			deleted_at = COALESCE(deleted_at, NOW()),
			username = $2,
			email = $3,
			password_hash = '',
			avatar_url = NULL,
			banner_url = NULL,
			city = NULL,
			country = NULL,
			bio = NULL,
			gender = NULL,
			birth_date = NULL,
			sober_since = NULL,
			subscription_tier = 'free',
			subscription_status = 'inactive',
			lat = NULL,
			lng = NULL,
			current_lat = NULL,
			current_lng = NULL,
			current_city = NULL,
			current_country = NULL,
			location_updated_at = NULL,
			discover_lat = NULL,
			discover_lng = NULL,
			connection_intents = ARRAY['friends']::text[],
			onboarding_completed_at = NULL,
			onboarding_owner_welcome_comment_id = NULL,
			identity_verification_status = 'not_started',
			identity_verification_provider = NULL,
			identity_verification_session_id = NULL,
			identity_verification_last_error = NULL,
			identity_verified_at = NULL,
			sobriety_band = NULL,
			profile_completeness = 0
		WHERE id = $1`,
		userID, deletedUsername, deletedEmail,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return tx.Commit(ctx)
}

func (s *pgStore) CompleteOnboarding(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users
		SET onboarding_completed_at = COALESCE(onboarding_completed_at, NOW())
		WHERE id = $1`,
		userID,
	)
	return err
}

func (s *pgStore) BlockUser(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`INSERT INTO user_blocks (blocker_id, blocked_id)
		VALUES ($1, $2)
		ON CONFLICT (blocker_id, blocked_id) DO NOTHING`,
		blockerID,
		blockedID,
	); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM friendships
		WHERE (user_a_id = $1 AND user_b_id = $2)
			OR (user_a_id = $2 AND user_b_id = $1)`,
		blockerID,
		blockedID,
	); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *pgStore) UnblockUser(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM user_blocks
		WHERE blocker_id = $1 AND blocked_id = $2`,
		blockerID,
		blockedID,
	)
	return err
}

func (s *pgStore) ReportUser(ctx context.Context, reporterID, reportedUserID uuid.UUID, reason string, details *string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_reports (reporter_id, reported_user_id, reason, details)
		VALUES ($1, $2, $3, $4)`,
		reporterID,
		reportedUserID,
		reason,
		details,
	)
	return err
}

func (s *pgStore) UpdateAvatarURL(ctx context.Context, userID uuid.UUID, avatarURL string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET avatar_url = $1 WHERE id = $2`,
		avatarURL, userID,
	)
	if err != nil {
		return err
	}
	return s.syncDiscoverUserState(ctx, userID)
}

func (s *pgStore) UpdateBannerURL(ctx context.Context, userID uuid.UUID, bannerURL string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET banner_url = $1 WHERE id = $2`,
		bannerURL, userID,
	)
	return err
}

func (s *pgStore) syncDiscoverUserState(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users u
		SET
			discover_lat = COALESCE(u.current_lat, u.lat),
			discover_lng = COALESCE(u.current_lng, u.lng),
			sobriety_band = CASE
				WHEN u.sober_since IS NULL THEN NULL
				WHEN CURRENT_DATE - u.sober_since < 30 THEN 1
				WHEN CURRENT_DATE - u.sober_since < 90 THEN 2
				WHEN CURRENT_DATE - u.sober_since < 365 THEN 3
				WHEN CURRENT_DATE - u.sober_since < 730 THEN 4
				WHEN CURRENT_DATE - u.sober_since < 1825 THEN 5
				ELSE 6
			END,
			profile_completeness = `+discoverProfileCompletenessExpr+`
		WHERE u.id = $1`,
		userID,
	)
	return err
}

func (s *pgStore) syncDiscoverUserStateTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`UPDATE users u
		SET
			discover_lat = COALESCE(u.current_lat, u.lat),
			discover_lng = COALESCE(u.current_lng, u.lng),
			sobriety_band = CASE
				WHEN u.sober_since IS NULL THEN NULL
				WHEN CURRENT_DATE - u.sober_since < 30 THEN 1
				WHEN CURRENT_DATE - u.sober_since < 90 THEN 2
				WHEN CURRENT_DATE - u.sober_since < 365 THEN 3
				WHEN CURRENT_DATE - u.sober_since < 730 THEN 4
				WHEN CURRENT_DATE - u.sober_since < 1825 THEN 5
				ELSE 6
			END,
			profile_completeness = `+discoverProfileCompletenessExpr+`
		WHERE u.id = $1`,
		userID,
	)
	return err
}

func (s *pgStore) DiscoverUsers(ctx context.Context, params DiscoverUsersParams) ([]User, error) {
	if params.Query != "" {
		// Search mode: prioritise exact and prefix username matches.
		return s.discoverBySearch(ctx, params)
	}
	return s.discoverUsersV2(ctx, params)
}

func (s *pgStore) CountDiscoverUsers(ctx context.Context, params DiscoverUsersParams) (int, error) {
	return s.countDiscoverUsersV2(ctx, params)
}

// discoverBySearch returns users filtered and sorted by username relevance.
func (s *pgStore) discoverBySearch(ctx context.Context, params DiscoverUsersParams) ([]User, error) {
	sobrietyMinDays := sobrietyMinimumDays(params.Sobriety)
	decodedCursor := decodeDiscoverCursor(params.Cursor)
	var cursorRank *int
	var cursorCreatedAt *time.Time
	var cursorID *uuid.UUID
	if decodedCursor.Mode == "search" && decodedCursor.Rank != nil && decodedCursor.CreatedAt != "" && decodedCursor.LastID != "" {
		if parsedAt, err := time.Parse(time.RFC3339Nano, decodedCursor.CreatedAt); err == nil {
			if parsedID, err := uuid.Parse(decodedCursor.LastID); err == nil {
				cursorRank = decodedCursor.Rank
				cursorCreatedAt = &parsedAt
				cursorID = &parsedID
			}
		}
	}
	rows, err := s.pool.Query(ctx,
		`SELECT
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
			END AS friendship_status
		FROM users u
		LEFT JOIN friendships f
			ON (
				(f.user_a_id = $1 AND f.user_b_id = u.id)
				OR (f.user_b_id = $1 AND f.user_a_id = u.id)
			)
		LEFT JOIN LATERAL (
			SELECT array_agg(i.name ORDER BY i.name) AS items
			FROM user_interests ui
			JOIN interests i ON i.id = ui.interest_id
			WHERE ui.user_id = u.id
		) interest_names ON true
		WHERE u.id != $1
			AND u.deleted_at IS NULL
			AND NOT EXISTS (
				SELECT 1
				FROM friendships fx
				WHERE (fx.user_a_id = $1 AND fx.user_b_id = u.id)
					OR (fx.user_b_id = $1 AND fx.user_a_id = u.id)
			)
			AND NOT EXISTS (
				SELECT 1
				FROM user_blocks ub
				WHERE (ub.blocker_id = $1 AND ub.blocked_id = u.id)
					OR (ub.blocker_id = u.id AND ub.blocked_id = $1)
			)
			AND ($2 = '' OR COALESCE(u.current_city, u.city) ILIKE $2)
			AND u.username ILIKE '%' || $3 || '%'
			AND ($4 = '' OR u.gender = $4)
			AND ($5::int IS NULL OR (u.birth_date IS NOT NULL AND u.birth_date <= CURRENT_DATE - make_interval(years => $5::int)))
			AND ($6::int IS NULL OR (u.birth_date IS NOT NULL AND u.birth_date > CURRENT_DATE - make_interval(years => ($6::int + 1))))
			AND ($7::int IS NULL OR (u.sober_since IS NOT NULL AND EXTRACT(EPOCH FROM (NOW() - u.sober_since::timestamptz)) / 86400.0 >= $7::float8))
			AND (
				$10::int IS NULL
				OR $10::int <= 0
				OR $8::float8 IS NULL
				OR $9::float8 IS NULL
				OR (
					u.discover_lat IS NOT NULL
					AND u.discover_lng IS NOT NULL
					AND 2.0 * 6371.0 * ASIN(SQRT(
						POWER(SIN(RADIANS((u.discover_lat - $8::float8) / 2.0)), 2)
						+ COS(RADIANS($8::float8)) * COS(RADIANS(u.discover_lat))
						* POWER(SIN(RADIANS((u.discover_lng - $9::float8) / 2.0)), 2)
					)) <= $10::float8
				)
			)
				AND (
					$11::text[] IS NULL
					OR EXISTS (
					SELECT 1
					FROM user_interests ui
					JOIN interests i ON i.id = ui.interest_id
					WHERE ui.user_id = u.id
						  AND i.name = ANY($11::text[])
					)
				)
				AND ($16 = '' OR u.connection_intents @> ARRAY[$16]::text[])
				AND (
					$13::int IS NULL
					OR (
						CASE
							WHEN u.username = $3 THEN 0
							WHEN u.username ILIKE $3 || '%' THEN 1
							ELSE 2
						END
					) > $13::int
					OR (
						CASE
							WHEN u.username = $3 THEN 0
							WHEN u.username ILIKE $3 || '%' THEN 1
							ELSE 2
						END
					) = $13::int AND u.created_at < $14::timestamptz
					OR (
						CASE
							WHEN u.username = $3 THEN 0
							WHEN u.username ILIKE $3 || '%' THEN 1
							ELSE 2
						END
					) = $13::int AND u.created_at = $14::timestamptz AND u.id > $15::uuid
				)
			ORDER BY
				CASE
					WHEN u.username = $3 THEN 0
					WHEN u.username ILIKE $3 || '%' THEN 1
					ELSE 2
				END,
				u.created_at DESC,
				u.id ASC
			LIMIT $12`,
		params.CurrentUserID, params.City, params.Query, params.Gender, params.AgeMin, params.AgeMax, sobrietyMinDays, params.Lat, params.Lng, params.DistanceKm, nullableTextArray(params.Interests), params.Limit, cursorRank, cursorCreatedAt, cursorID, params.Intent,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUsers(rows)
}

func scanUsers(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]User, error) {
	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.AvatarURL, &u.IsPlus, &u.SubscriptionTier, &u.SubscriptionStatus, &u.City, &u.Country, &u.Bio, &u.Interests, &u.ConnectionIntents, &u.Gender, &u.BirthDate, &u.SoberSince, &u.CreatedAt, &u.FriendshipStatus); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
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

func sobrietyMinimumDays(raw string) *int {
	var days int
	switch raw {
	case "days_30", "30+ days":
		days = 30
	case "days_90", "90+ days":
		days = 90
	case "years_1", "1+ year":
		days = 365
	case "years_5", "5+ years":
		days = 1825
	default:
		return nil
	}
	return &days
}

func nullableTextArray(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}
