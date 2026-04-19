package friends

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

func (s *pgStore) GetFriendshipState(ctx context.Context, userAID, userBID uuid.UUID) (found bool, status string, requesterID uuid.UUID, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT status, requester_id
		FROM friendships
		WHERE user_a_id = $1
			AND user_b_id = $2`,
		userAID, userBID,
	).Scan(&status, &requesterID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "", uuid.Nil, nil
	}
	if err != nil {
		return false, "", uuid.Nil, err
	}
	return true, status, requesterID, nil
}

func (s *pgStore) InsertFriendship(ctx context.Context, userAID, userBID, requesterID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO friendships (
			user_a_id,
			user_b_id,
			requester_id,
			status
		)
		VALUES ($1, $2, $3, 'pending')`,
		userAID, userBID, requesterID,
	)
	return err
}

func (s *pgStore) AcceptFriendRequest(ctx context.Context, userAID, userBID, userID, otherUserID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx,
		`UPDATE friendships
		SET
			status = 'accepted',
			accepted_at = NOW()
		WHERE user_a_id = $1
			AND user_b_id = $2
			AND requester_id = $3
			AND status = 'pending'`,
		userAID, userBID, otherUserID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	if _, err := tx.Exec(ctx,
		`UPDATE users SET friend_count = friend_count + 1 WHERE id = $1 OR id = $2`,
		userID, otherUserID,
	); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *pgStore) DeletePendingFriendship(ctx context.Context, userAID, userBID, requesterID uuid.UUID) error {
	result, err := s.pool.Exec(ctx,
		`DELETE FROM friendships
		WHERE user_a_id = $1
			AND user_b_id = $2
			AND requester_id = $3
			AND status = 'pending'`,
		userAID, userBID, requesterID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgStore) RemoveFriend(ctx context.Context, userAID, userBID, userID, otherUserID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx,
		`DELETE FROM friendships
		WHERE user_a_id = $1
			AND user_b_id = $2
			AND status = 'accepted'`,
		userAID, userBID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	if _, err := tx.Exec(ctx,
		`UPDATE users SET friend_count = GREATEST(friend_count - 1, 0) WHERE id = $1 OR id = $2`,
		userID, otherUserID,
	); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *pgStore) ListFriendUsers(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]friendUser, error) {
	return s.scanFriendUsers(ctx,
		`SELECT
			u.id,
			u.username,
			u.avatar_url,
			u.city,
			COALESCE(f.accepted_at, f.created_at) AS created_at
		FROM friendships f
		JOIN users u ON u.id = CASE
			WHEN f.user_a_id = $1 THEN f.user_b_id
			ELSE f.user_a_id
		END
		WHERE (f.user_a_id = $1 OR f.user_b_id = $1)
			AND f.status = 'accepted'
			AND ($2::timestamptz IS NULL OR COALESCE(f.accepted_at, f.created_at) < $2)
		ORDER BY COALESCE(f.accepted_at, f.created_at) DESC
		LIMIT $3`,
		userID, before, limit,
	)
}

func (s *pgStore) ListPendingRequests(ctx context.Context, userID uuid.UUID, outgoing bool, before *time.Time, limit int) ([]friendUser, error) {
	requesterFilter := "AND f.requester_id != $1"
	if outgoing {
		requesterFilter = "AND f.requester_id = $1"
	}

	return s.scanFriendUsers(ctx,
		`SELECT
			u.id,
			u.username,
			u.avatar_url,
			u.city,
			f.created_at
		FROM friendships f
		JOIN users u ON u.id = CASE
			WHEN f.user_a_id = $1 THEN f.user_b_id
			ELSE f.user_a_id
		END
		WHERE (f.user_a_id = $1 OR f.user_b_id = $1)
			AND f.status = 'pending'
			`+requesterFilter+`
			AND ($2::timestamptz IS NULL OR f.created_at < $2)
		ORDER BY f.created_at DESC
		LIMIT $3`,
		userID, before, limit,
	)
}

func (s *pgStore) scanFriendUsers(ctx context.Context, query string, args ...any) ([]friendUser, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []friendUser
	for rows.Next() {
		var u friendUser
		if err := rows.Scan(&u.UserID, &u.Username, &u.AvatarURL, &u.City, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
