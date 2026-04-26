package user

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type pgStore struct {
	pool               *pgxpool.Pool
	discoverPipelineV2 bool
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
)::smallint`

// NewPgStore wraps a pgxpool.Pool as the production Querier implementation.
func NewPgStore(pool *pgxpool.Pool) Querier {
	return NewPgStoreWithConfig(pool, StoreConfig{DiscoverPipelineV2: true})
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
			u.city,
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
		WHERE u.id = $2`,
		viewerID, userID,
	).Scan(
		&u.ID, &u.Username, &u.AvatarURL, &u.BannerURL, &u.IsPlus, &u.SubscriptionTier, &u.SubscriptionStatus, &u.City, &u.Country, &u.Bio, &u.Interests, &u.Gender, &u.BirthDate, &u.SoberSince, &u.CreatedAt,
		&u.FriendshipStatus, &u.FriendCount, &u.IncomingFriendRequestCt, &u.OutgoingFriendRequestCt,
		&u.CurrentCity, &u.LocationUpdatedAt,
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
		`SELECT EXISTS(SELECT 1 FROM users WHERE username = $1 AND id != $2)`,
		username, userID,
	).Scan(&exists)
	return exists, err
}

func (s *pgStore) UpdateUser(ctx context.Context, userID uuid.UUID, username, city, country, gender, bio *string, soberSince *time.Time, replaceSoberSince bool, birthDate *time.Time, replaceBirthDate bool, interests []string, replaceInterests bool, lat, lng *float64) error {
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
			lng = COALESCE($12::float8, lng)
		WHERE id = $10`,
		username, city, country, gender, bio, replaceSoberSince, soberSince, replaceBirthDate, birthDate, userID, lat, lng,
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

func (s *pgStore) UpdateCurrentLocation(ctx context.Context, userID uuid.UUID, lat, lng float64, city string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users
		SET
			current_lat = $2,
			current_lng = $3,
			current_city = $4,
			location_updated_at = NOW(),
			discover_lat = $2,
			discover_lng = $3
		WHERE id = $1`,
		userID, lat, lng, city,
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
	if s.discoverPipelineV2 {
		return s.discoverUsersV2(ctx, params)
	}
	return s.discoverRanked(ctx, params)
}

func (s *pgStore) CountDiscoverUsers(ctx context.Context, params DiscoverUsersParams) (int, error) {
	if s.discoverPipelineV2 {
		return s.countDiscoverUsersV2(ctx, params)
	}

	sobrietyMinDays := sobrietyMinimumDays(params.Sobriety)

	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*)
		FROM users u
		WHERE u.id != $1
			AND ($2 = '' OR u.city ILIKE $2)
			AND ($3 = '' OR u.username ILIKE '%' || $3 || '%')
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
					COALESCE(u.current_lat, u.lat) IS NOT NULL
					AND COALESCE(u.current_lng, u.lng) IS NOT NULL
					AND 2.0 * 6371.0 * ASIN(SQRT(
						POWER(SIN(RADIANS((COALESCE(u.current_lat, u.lat) - $8::float8) / 2.0)), 2)
						+ COS(RADIANS($8::float8)) * COS(RADIANS(COALESCE(u.current_lat, u.lat)))
						* POWER(SIN(RADIANS((COALESCE(u.current_lng, u.lng) - $9::float8) / 2.0)), 2)
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
			)`,
		params.CurrentUserID, params.City, params.Query, params.Gender, params.AgeMin, params.AgeMax, sobrietyMinDays, params.Lat, params.Lng, params.DistanceKm, nullableTextArray(params.Interests),
	).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// discoverBySearch returns users filtered and sorted by username relevance.
func (s *pgStore) discoverBySearch(ctx context.Context, params DiscoverUsersParams) ([]User, error) {
	sobrietyMinDays := sobrietyMinimumDays(params.Sobriety)
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
			AND NOT EXISTS (
				SELECT 1
				FROM friendships fx
				WHERE (fx.user_a_id = $1 AND fx.user_b_id = u.id)
					OR (fx.user_b_id = $1 AND fx.user_a_id = u.id)
			)
			AND NOT EXISTS (
				SELECT 1
				FROM discover_dismissals dd
				WHERE dd.viewer_id = $1
					AND dd.candidate_id = u.id
					AND dd.dismissed_at > NOW() - INTERVAL '14 days'
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
		ORDER BY
			CASE
				WHEN u.username = $3 THEN 0
				WHEN u.username ILIKE $3 || '%' THEN 1
				ELSE 2
			END,
			u.created_at DESC
		LIMIT $12 OFFSET $13`,
		params.CurrentUserID, params.City, params.Query, params.Gender, params.AgeMin, params.AgeMax, sobrietyMinDays, params.Lat, params.Lng, params.DistanceKm, nullableTextArray(params.Interests), params.Limit, params.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUsers(rows)
}

// discoverRanked returns users sorted by the five-signal suggestion score.
func (s *pgStore) discoverRanked(ctx context.Context, params DiscoverUsersParams) ([]User, error) {
	sobrietyMinDays := sobrietyMinimumDays(params.Sobriety)
	rows, err := s.pool.Query(ctx,
		`WITH viewer_data AS (
			SELECT
				CASE WHEN sober_since IS NOT NULL
					THEN EXTRACT(EPOCH FROM (NOW() - sober_since::timestamptz)) / 86400.0
					ELSE NULL
				END AS days_sober
			FROM users WHERE id = $1
		),
		viewer_band AS (
			SELECT CASE
				WHEN (SELECT days_sober FROM viewer_data) IS NULL    THEN NULL
				WHEN (SELECT days_sober FROM viewer_data) < 30       THEN 1
				WHEN (SELECT days_sober FROM viewer_data) < 90       THEN 2
				WHEN (SELECT days_sober FROM viewer_data) < 365      THEN 3
				WHEN (SELECT days_sober FROM viewer_data) < 730      THEN 4
				WHEN (SELECT days_sober FROM viewer_data) < 1825     THEN 5
				ELSE 6
			END AS band
		),
		candidates AS (
			SELECT
				u.id,
				u.username,
				u.avatar_url,
				u.subscription_tier,
				u.subscription_status,
				u.city,
				u.country,
				u.bio,
				u.gender,
				u.birth_date,
				u.sober_since,
				u.created_at,
				COALESCE(u.current_lat, u.lat) AS lat,
				COALESCE(u.current_lng, u.lng) AS lng,
				CASE
					WHEN f.status = 'accepted' THEN 'friends'
					WHEN f.requester_id = $1 THEN 'outgoing'
					WHEN f.requester_id = u.id THEN 'incoming'
					ELSE 'none'
				END AS friendship_status,
				CASE
					WHEN u.sober_since IS NULL THEN NULL
					WHEN EXTRACT(EPOCH FROM (NOW() - u.sober_since::timestamptz)) / 86400.0 < 30   THEN 1
					WHEN EXTRACT(EPOCH FROM (NOW() - u.sober_since::timestamptz)) / 86400.0 < 90   THEN 2
					WHEN EXTRACT(EPOCH FROM (NOW() - u.sober_since::timestamptz)) / 86400.0 < 365  THEN 3
					WHEN EXTRACT(EPOCH FROM (NOW() - u.sober_since::timestamptz)) / 86400.0 < 730  THEN 4
					WHEN EXTRACT(EPOCH FROM (NOW() - u.sober_since::timestamptz)) / 86400.0 < 1825 THEN 5
					ELSE 6
				END AS cand_band
			FROM users u
			LEFT JOIN friendships f ON (
				(f.user_a_id = $1 AND f.user_b_id = u.id)
				OR (f.user_b_id = $1 AND f.user_a_id = u.id)
			)
			WHERE u.id != $1
			  AND ($2 = '' OR u.city ILIKE $2)
			  AND ($3 = '' OR u.gender = $3)
			  AND ($4::int IS NULL OR (u.birth_date IS NOT NULL AND u.birth_date <= CURRENT_DATE - make_interval(years => $4::int)))
			  AND ($5::int IS NULL OR (u.birth_date IS NOT NULL AND u.birth_date > CURRENT_DATE - make_interval(years => ($5::int + 1))))
			  AND ($6::int IS NULL OR (u.sober_since IS NOT NULL AND EXTRACT(EPOCH FROM (NOW() - u.sober_since::timestamptz)) / 86400.0 >= $6::float8))
			  AND (
				$9::int IS NULL
				OR $9::int <= 0
				OR $7::float8 IS NULL
				OR $8::float8 IS NULL
				OR (
					COALESCE(u.current_lat, u.lat) IS NOT NULL
					AND COALESCE(u.current_lng, u.lng) IS NOT NULL
					AND 2.0 * 6371.0 * ASIN(SQRT(
						POWER(SIN(RADIANS((COALESCE(u.current_lat, u.lat) - $7::float8) / 2.0)), 2)
						+ COS(RADIANS($7::float8)) * COS(RADIANS(COALESCE(u.current_lat, u.lat)))
						* POWER(SIN(RADIANS((COALESCE(u.current_lng, u.lng) - $8::float8) / 2.0)), 2)
					)) <= $9::float8
				)
			  )
			  AND (
				$10::text[] IS NULL
				OR EXISTS (
					SELECT 1
					FROM user_interests ui
					JOIN interests i ON i.id = ui.interest_id
					WHERE ui.user_id = u.id
					  AND i.name = ANY($10::text[])
				)
			  )
		)
		SELECT
			c.id,
			c.username,
			c.avatar_url,
			(c.subscription_tier = 'plus' AND c.subscription_status = 'active') AS is_plus,
			c.subscription_tier,
			c.subscription_status,
			c.city,
			c.country,
			c.bio,
			COALESCE(interest_names.items, '{}') AS interests,
			c.gender,
			CASE
				WHEN c.birth_date IS NULL THEN NULL
				ELSE TO_CHAR(c.birth_date, 'YYYY-MM-DD')
			END AS birth_date,
			c.sober_since,
			c.created_at,
			c.friendship_status,
			(
				CASE
					WHEN $7::float8 IS NOT NULL AND $8::float8 IS NOT NULL
						 AND c.lat IS NOT NULL AND c.lng IS NOT NULL
					THEN 0.30 * EXP(-(
						2.0 * 6371.0 * ASIN(SQRT(
							POWER(SIN(RADIANS((c.lat - $7::float8) / 2.0)), 2)
							+ COS(RADIANS($7::float8)) * COS(RADIANS(c.lat))
							* POWER(SIN(RADIANS((c.lng - $8::float8) / 2.0)), 2)
						))
					) / 50.0)
					ELSE 0.0
				END
				+ CASE
					WHEN (SELECT band FROM viewer_band) IS NULL OR c.sober_since IS NULL THEN 0.0
					WHEN (SELECT band FROM viewer_band) = c.cand_band                    THEN 0.15
					WHEN ABS((SELECT band FROM viewer_band) - c.cand_band) = 1           THEN 0.075
					ELSE 0.0
				  END
				+ 0.25 * COALESCE(interest_jaccard.score, 0.0)
				+ 0.20 * LEAST(COALESCE(mutual.cnt, 0)::float8 / 5.0, 1.0)
				+ CASE WHEN active.recent THEN 0.10 ELSE 0.0 END
			) AS score
		FROM candidates c
		CROSS JOIN viewer_band
		LEFT JOIN LATERAL (
			SELECT array_agg(i.name ORDER BY i.name) AS items
			FROM user_interests ui
			JOIN interests i ON i.id = ui.interest_id
			WHERE ui.user_id = c.id
		) interest_names ON true
		LEFT JOIN LATERAL (
			SELECT
				CASE
					WHEN (u_cnt.n + v_cnt.n - i_cnt.n) = 0 THEN 0.0
					ELSE i_cnt.n::float8 / (u_cnt.n + v_cnt.n - i_cnt.n)::float8
				END AS score
			FROM
				(SELECT COUNT(*) AS n FROM user_interests WHERE user_id = c.id) u_cnt,
				(SELECT COUNT(*) AS n FROM user_interests WHERE user_id = $1) v_cnt,
				(
					SELECT COUNT(*) AS n
					FROM user_interests ui1
					JOIN user_interests ui2 ON ui1.interest_id = ui2.interest_id
					WHERE ui1.user_id = c.id AND ui2.user_id = $1
				) i_cnt
		) interest_jaccard ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS cnt
			FROM (
				SELECT CASE WHEN f.user_a_id = $1 THEN f.user_b_id ELSE f.user_a_id END AS fid
				FROM friendships f
				WHERE (f.user_a_id = $1 OR f.user_b_id = $1)
				  AND f.status = 'accepted'
			) vf
			WHERE EXISTS (
				SELECT 1 FROM friendships f2
				WHERE f2.status = 'accepted'
				  AND ((f2.user_a_id = c.id AND f2.user_b_id = vf.fid)
					   OR (f2.user_b_id = c.id AND f2.user_a_id = vf.fid))
			)
		) mutual ON true
		LEFT JOIN LATERAL (
			SELECT EXISTS (
				SELECT 1 FROM posts p
				WHERE p.user_id = c.id
				  AND p.created_at > NOW() - INTERVAL '7 days'
			) AS recent
		) active ON true
		ORDER BY score DESC, c.id
		LIMIT $11 OFFSET $12`,
		params.CurrentUserID, params.City, params.Gender, params.AgeMin, params.AgeMax, sobrietyMinDays, params.Lat, params.Lng, params.DistanceKm, nullableTextArray(params.Interests), params.Limit, params.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	var score float64
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.AvatarURL, &u.IsPlus, &u.SubscriptionTier, &u.SubscriptionStatus, &u.City, &u.Country, &u.Bio, &u.Interests, &u.Gender, &u.BirthDate, &u.SoberSince, &u.CreatedAt, &u.FriendshipStatus, &score); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func scanUsers(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]User, error) {
	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.AvatarURL, &u.IsPlus, &u.SubscriptionTier, &u.SubscriptionStatus, &u.City, &u.Country, &u.Bio, &u.Interests, &u.Gender, &u.BirthDate, &u.SoberSince, &u.CreatedAt, &u.FriendshipStatus); err != nil {
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
