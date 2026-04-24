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
	pool *pgxpool.Pool
}

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
			u.city,
			u.country,
			u.bio,
			COALESCE(interest_names.items, '{}') AS interests,
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
			oc.cnt AS outgoing_friend_request_count
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
		&u.ID, &u.Username, &u.AvatarURL, &u.City, &u.Country, &u.Bio, &u.Interests, &u.SoberSince, &u.CreatedAt,
		&u.FriendshipStatus, &u.FriendCount, &u.IncomingFriendRequestCt, &u.OutgoingFriendRequestCt,
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

func (s *pgStore) UpdateUser(ctx context.Context, userID uuid.UUID, username, city, country, bio *string, soberSince *time.Time, replaceSoberSince bool, interests []string, replaceInterests bool, lat, lng *float64) error {
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
			bio = CASE
				WHEN $4::text IS NULL THEN bio
				ELSE NULLIF($4::text, '')
			END,
			sober_since = CASE
				WHEN NOT $5 THEN sober_since
				ELSE $6::date
			END,
			lat = COALESCE($8::float8, lat),
			lng = COALESCE($9::float8, lng)
		WHERE id = $7`,
		username, city, country, bio, replaceSoberSince, soberSince, userID, lat, lng,
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

	return tx.Commit(ctx)
}

func (s *pgStore) UpdateAvatarURL(ctx context.Context, userID uuid.UUID, avatarURL string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET avatar_url = $1 WHERE id = $2`,
		avatarURL, userID,
	)
	return err
}

func (s *pgStore) DiscoverUsers(ctx context.Context, currentUserID uuid.UUID, city, query string, lat, lng *float64, limit, offset int) ([]User, error) {
	if query != "" {
		// Search mode: prioritise exact and prefix username matches.
		return s.discoverBySearch(ctx, currentUserID, city, query, limit, offset)
	}
	return s.discoverRanked(ctx, currentUserID, city, lat, lng, limit, offset)
}

// discoverBySearch returns users filtered and sorted by username relevance.
func (s *pgStore) discoverBySearch(ctx context.Context, currentUserID uuid.UUID, city, query string, limit, offset int) ([]User, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			u.id,
			u.username,
			u.avatar_url,
			u.city,
			u.country,
			u.bio,
			COALESCE(interest_names.items, '{}') AS interests,
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
			AND ($2 = '' OR u.city ILIKE $2)
			AND u.username ILIKE '%' || $3 || '%'
		ORDER BY
			CASE
				WHEN u.username = $3 THEN 0
				WHEN u.username ILIKE $3 || '%' THEN 1
				ELSE 2
			END,
			u.created_at DESC
		LIMIT $4 OFFSET $5`,
		currentUserID, city, query, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanUsers(rows)
}

// discoverRanked returns users sorted by the five-signal suggestion score.
func (s *pgStore) discoverRanked(ctx context.Context, currentUserID uuid.UUID, city string, lat, lng *float64, limit, offset int) ([]User, error) {
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
				u.city,
				u.country,
				u.bio,
				u.sober_since,
				u.created_at,
				u.lat,
				u.lng,
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
		)
		SELECT
			c.id,
			c.username,
			c.avatar_url,
			c.city,
			c.country,
			c.bio,
			COALESCE(interest_names.items, '{}') AS interests,
			c.sober_since,
			c.created_at,
			c.friendship_status,
			(
				CASE
					WHEN $3::float8 IS NOT NULL AND $4::float8 IS NOT NULL
						 AND c.lat IS NOT NULL AND c.lng IS NOT NULL
					THEN 0.30 * EXP(-(
						2.0 * 6371.0 * ASIN(SQRT(
							POWER(SIN(RADIANS((c.lat - $3::float8) / 2.0)), 2)
							+ COS(RADIANS($3::float8)) * COS(RADIANS(c.lat))
							* POWER(SIN(RADIANS((c.lng - $4::float8) / 2.0)), 2)
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
		LIMIT $5 OFFSET $6`,
		currentUserID, city, lat, lng, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		var score float64
		if err := rows.Scan(&u.ID, &u.Username, &u.AvatarURL, &u.City, &u.Country, &u.Bio, &u.Interests, &u.SoberSince, &u.CreatedAt, &u.FriendshipStatus, &score); err != nil {
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
		if err := rows.Scan(&u.ID, &u.Username, &u.AvatarURL, &u.City, &u.Country, &u.Bio, &u.Interests, &u.SoberSince, &u.CreatedAt, &u.FriendshipStatus); err != nil {
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
