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

func (s *pgStore) CreateUser(ctx context.Context, username, email, passwordHash, city, country string, gender *string, birthDate, soberSince *time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (
			username,
			email,
			password_hash,
			city,
			country,
			gender,
			birth_date,
			sober_since,
			sobriety_band,
			profile_completeness,
			last_active_at
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			CASE
				WHEN $8::date IS NULL THEN NULL
				WHEN CURRENT_DATE - $8::date < 30 THEN 1
				WHEN CURRENT_DATE - $8::date < 90 THEN 2
				WHEN CURRENT_DATE - $8::date < 365 THEN 3
				WHEN CURRENT_DATE - $8::date < 730 THEN 4
				WHEN CURRENT_DATE - $8::date < 1825 THEN 5
				ELSE 6
			END,
			(
				CASE WHEN NULLIF($4, '') IS NOT NULL THEN 1 ELSE 0 END
				+ CASE WHEN NULLIF($5, '') IS NOT NULL THEN 1 ELSE 0 END
				+ CASE WHEN NULLIF($6, '') IS NOT NULL THEN 1 ELSE 0 END
				+ CASE WHEN $7::date IS NOT NULL THEN 1 ELSE 0 END
				+ CASE WHEN $8::date IS NOT NULL THEN 1 ELSE 0 END
			)::smallint,
			NOW()
		)
		RETURNING id`,
		username, email, passwordHash, city, country, gender, birthDate, soberSince,
	).Scan(&id)
	return id, err
}

func (s *pgStore) GetUserCredentials(ctx context.Context, email string) (uuid.UUID, string, error) {
	var id uuid.UUID
	var hash string
	err := s.pool.QueryRow(ctx, "SELECT id, password_hash FROM users WHERE email = $1", email).Scan(&id, &hash)
	return id, hash, err
}
