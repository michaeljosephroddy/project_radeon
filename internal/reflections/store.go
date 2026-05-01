package reflections

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	GetTodayReflection(ctx context.Context, userID uuid.UUID, today time.Time) (*DailyReflection, error)
	UpsertTodayReflection(ctx context.Context, userID uuid.UUID, today time.Time, input UpsertDailyReflectionInput) (*DailyReflection, error)
	ListReflections(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]DailyReflection, error)
	GetReflection(ctx context.Context, userID, reflectionID uuid.UUID) (*DailyReflection, error)
	UpdateReflection(ctx context.Context, userID, reflectionID uuid.UUID, input UpdateDailyReflectionInput) (*DailyReflection, error)
	DeleteReflection(ctx context.Context, userID, reflectionID uuid.UUID) error
	ShareReflection(ctx context.Context, userID, reflectionID uuid.UUID) (uuid.UUID, error)
}

type pgStore struct {
	pool *pgxpool.Pool
}

func NewPgStore(pool *pgxpool.Pool) Store {
	return &pgStore{pool: pool}
}

func (s *pgStore) GetTodayReflection(ctx context.Context, userID uuid.UUID, today time.Time) (*DailyReflection, error) {
	return s.getReflectionByDate(ctx, userID, dateOnly(today))
}

func (s *pgStore) UpsertTodayReflection(ctx context.Context, userID uuid.UUID, today time.Time, input UpsertDailyReflectionInput) (*DailyReflection, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO daily_reflections (
			user_id,
			reflection_date,
			prompt_key,
			prompt_text,
			grateful_for,
			on_mind,
			blocking_today,
			body
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, $6, $7, $8)
		ON CONFLICT (user_id, reflection_date) DO UPDATE SET
			prompt_key = EXCLUDED.prompt_key,
			prompt_text = EXCLUDED.prompt_text,
			grateful_for = EXCLUDED.grateful_for,
			on_mind = EXCLUDED.on_mind,
			blocking_today = EXCLUDED.blocking_today,
			body = EXCLUDED.body,
			updated_at = NOW()
		RETURNING
			id,
			user_id,
			TO_CHAR(reflection_date, 'YYYY-MM-DD'),
			prompt_key,
			prompt_text,
			grateful_for,
			on_mind,
			blocking_today,
			body,
			shared_post_id,
			created_at,
			updated_at`,
		userID,
		dateOnly(today),
		valueOrEmpty(input.PromptKey),
		valueOrEmpty(input.PromptText),
		input.GratefulFor,
		input.OnMind,
		input.BlockingToday,
		input.Body,
	)
	return scanReflection(row)
}

func (s *pgStore) ListReflections(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]DailyReflection, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			id,
			user_id,
			TO_CHAR(reflection_date, 'YYYY-MM-DD'),
			prompt_key,
			prompt_text,
			grateful_for,
			on_mind,
			blocking_today,
			body,
			shared_post_id,
			created_at,
			updated_at
		FROM daily_reflections
		WHERE user_id = $1
			AND ($2::date IS NULL OR reflection_date < $2::date)
		ORDER BY reflection_date DESC, id DESC
		LIMIT $3`,
		userID,
		before,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reflections := make([]DailyReflection, 0, limit)
	for rows.Next() {
		reflection, err := scanReflection(rows)
		if err != nil {
			return nil, err
		}
		reflections = append(reflections, *reflection)
	}
	return reflections, rows.Err()
}

func (s *pgStore) GetReflection(ctx context.Context, userID, reflectionID uuid.UUID) (*DailyReflection, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			id,
			user_id,
			TO_CHAR(reflection_date, 'YYYY-MM-DD'),
			prompt_key,
			prompt_text,
			grateful_for,
			on_mind,
			blocking_today,
			body,
			shared_post_id,
			created_at,
			updated_at
		FROM daily_reflections
		WHERE id = $1 AND user_id = $2`,
		reflectionID,
		userID,
	)
	return scanReflection(row)
}

func (s *pgStore) UpdateReflection(ctx context.Context, userID, reflectionID uuid.UUID, input UpdateDailyReflectionInput) (*DailyReflection, error) {
	current, err := s.GetReflection(ctx, userID, reflectionID)
	if err != nil {
		return nil, err
	}

	promptKey := current.PromptKey
	if input.PromptKey != nil {
		promptKey = emptyStringAsNil(*input.PromptKey)
	}
	promptText := current.PromptText
	if input.PromptText != nil {
		promptText = emptyStringAsNil(*input.PromptText)
	}
	gratefulFor := current.GratefulFor
	if input.GratefulFor != nil {
		gratefulFor = emptyStringAsNil(*input.GratefulFor)
	}
	onMind := current.OnMind
	if input.OnMind != nil {
		onMind = emptyStringAsNil(*input.OnMind)
	}
	blockingToday := current.BlockingToday
	if input.BlockingToday != nil {
		blockingToday = emptyStringAsNil(*input.BlockingToday)
	}
	body := current.Body
	if input.Body != nil {
		body = *input.Body
	}

	row := s.pool.QueryRow(ctx, `
		UPDATE daily_reflections
		SET
			prompt_key = $3,
			prompt_text = $4,
			grateful_for = $5,
			on_mind = $6,
			blocking_today = $7,
			body = $8,
			updated_at = NOW()
		WHERE id = $1 AND user_id = $2
		RETURNING
			id,
			user_id,
			TO_CHAR(reflection_date, 'YYYY-MM-DD'),
			prompt_key,
			prompt_text,
			grateful_for,
			on_mind,
			blocking_today,
			body,
			shared_post_id,
			created_at,
			updated_at`,
		reflectionID,
		userID,
		promptKey,
		promptText,
		gratefulFor,
		onMind,
		blockingToday,
		body,
	)
	return scanReflection(row)
}

func (s *pgStore) DeleteReflection(ctx context.Context, userID, reflectionID uuid.UUID) error {
	result, err := s.pool.Exec(ctx,
		`DELETE FROM daily_reflections WHERE id = $1 AND user_id = $2`,
		reflectionID,
		userID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgStore) ShareReflection(ctx context.Context, userID, reflectionID uuid.UUID) (uuid.UUID, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	var body string
	var existingPostID *uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT body, shared_post_id
		FROM daily_reflections
		WHERE id = $1 AND user_id = $2
		FOR UPDATE`,
		reflectionID,
		userID,
	).Scan(&body, &existingPostID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	if existingPostID != nil {
		return *existingPostID, tx.Commit(ctx)
	}

	var postID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO posts (
			user_id,
			body,
			source_type,
			source_id,
			source_label
		)
		VALUES ($1, $2, 'daily_reflection', $3, 'Daily reflection')
		RETURNING id`,
		userID,
		body,
		reflectionID,
	).Scan(&postID); err != nil {
		return uuid.Nil, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE daily_reflections
		SET shared_post_id = $3, updated_at = NOW()
		WHERE id = $1 AND user_id = $2`,
		reflectionID,
		userID,
		postID,
	); err != nil {
		return uuid.Nil, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE users
		SET last_active_at = GREATEST(last_active_at, NOW())
		WHERE id = $1`,
		userID,
	); err != nil {
		return uuid.Nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return postID, nil
}

func (s *pgStore) getReflectionByDate(ctx context.Context, userID uuid.UUID, date time.Time) (*DailyReflection, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			id,
			user_id,
			TO_CHAR(reflection_date, 'YYYY-MM-DD'),
			prompt_key,
			prompt_text,
			grateful_for,
			on_mind,
			blocking_today,
			body,
			shared_post_id,
			created_at,
			updated_at
		FROM daily_reflections
		WHERE user_id = $1 AND reflection_date = $2::date`,
		userID,
		date,
	)
	reflection, err := scanReflection(row)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return reflection, err
}

type scanner interface {
	Scan(...any) error
}

func scanReflection(row scanner) (*DailyReflection, error) {
	var reflection DailyReflection
	if err := row.Scan(
		&reflection.ID,
		&reflection.UserID,
		&reflection.ReflectionDate,
		&reflection.PromptKey,
		&reflection.PromptText,
		&reflection.GratefulFor,
		&reflection.OnMind,
		&reflection.BlockingToday,
		&reflection.Body,
		&reflection.SharedPostID,
		&reflection.CreatedAt,
		&reflection.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &reflection, nil
}

func dateOnly(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func emptyStringAsNil(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
