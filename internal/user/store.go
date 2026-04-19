package user

import (
	"context"
	"errors"

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
		&u.ID, &u.Username, &u.AvatarURL, &u.City, &u.Country, &u.SoberSince, &u.CreatedAt,
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

func (s *pgStore) UpdateUser(ctx context.Context, userID uuid.UUID, username, city, country *string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users
		SET
			username = COALESCE($1, username),
			city = COALESCE($2, city),
			country = COALESCE($3, country)
		WHERE id = $4`,
		username, city, country, userID,
	)
	return err
}

func (s *pgStore) UpdateAvatarURL(ctx context.Context, userID uuid.UUID, avatarURL string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET avatar_url = $1 WHERE id = $2`,
		avatarURL, userID,
	)
	return err
}

func (s *pgStore) DiscoverUsers(ctx context.Context, currentUserID uuid.UUID, city, query string, limit, offset int) ([]User, error) {
	// The ORDER BY prioritises exact and prefix username matches before falling
	// back to newest users, which gives search results a predictable ranking.
	rows, err := s.pool.Query(ctx,
		`SELECT
			u.id,
			u.username,
			u.avatar_url,
			u.city,
			u.country,
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
		WHERE u.id != $1
			AND ($2 = '' OR u.city ILIKE $2)
			AND (
				$3 = ''
				OR u.username ILIKE '%' || $3 || '%'
			)
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

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.AvatarURL, &u.City, &u.Country, &u.SoberSince, &u.CreatedAt, &u.FriendshipStatus); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
