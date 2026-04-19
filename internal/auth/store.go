package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgStore struct {
	pool *pgxpool.Pool
}

// NewPgStore wraps a pgxpool.Pool as the production Querier implementation.
func NewPgStore(pool *pgxpool.Pool) Querier {
	return &pgStore{pool: pool}
}

func (s *pgStore) EmailExists(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)", email).Scan(&exists)
	return exists, err
}

func (s *pgStore) UsernameExists(ctx context.Context, username string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)", username).Scan(&exists)
	return exists, err
}

func (s *pgStore) CreateUser(ctx context.Context, username, email, passwordHash, city, country string, soberSince *time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (
			username,
			email,
			password_hash,
			city,
			country,
			sober_since
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		username, email, passwordHash, city, country, soberSince,
	).Scan(&id)
	return id, err
}

func (s *pgStore) GetUserCredentials(ctx context.Context, email string) (uuid.UUID, string, error) {
	var id uuid.UUID
	var hash string
	err := s.pool.QueryRow(ctx, "SELECT id, password_hash FROM users WHERE email = $1", email).Scan(&id, &hash)
	return id, hash, err
}
