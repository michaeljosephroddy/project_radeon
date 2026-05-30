package support

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *pgStore) CountSupportSignalsSince(ctx context.Context, userID uuid.UUID, since time.Time) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*)
		FROM support_signals
		WHERE user_id = $1
			AND created_at >= $2`,
		userID, since,
	).Scan(&count)
	return count, err
}

func (s *pgStore) GetActiveSupportSignalForUser(ctx context.Context, viewerID, userID uuid.UUID) (*SupportSignal, error) {
	return scanSupportSignal(s.pool.QueryRow(ctx, supportSignalSelect()+`
		WHERE ss.user_id = $2
			AND ss.status = 'active'
			AND ss.expires_at > NOW()
			AND u.deleted_at IS NULL
			AND NOT EXISTS (
				SELECT 1 FROM user_blocks ub
				WHERE (ub.blocker_id = $1 AND ub.blocked_id = ss.user_id)
					OR (ub.blocker_id = ss.user_id AND ub.blocked_id = $1)
			)
		ORDER BY ss.created_at DESC
		LIMIT 1`,
		viewerID, userID,
	))
}

func (s *pgStore) ListActiveSupportSignals(ctx context.Context, viewerID uuid.UUID, before *time.Time, limit int) ([]SupportSignal, error) {
	rows, err := s.pool.Query(ctx, supportSignalSelect()+`
		WHERE ss.status = 'active'
			AND ss.expires_at > NOW()
			AND ss.user_id <> $1
			AND u.deleted_at IS NULL
			AND ($2::timestamptz IS NULL OR ss.created_at < $2)
			AND NOT EXISTS (
				SELECT 1 FROM user_blocks ub
				WHERE (ub.blocker_id = $1 AND ub.blocked_id = ss.user_id)
					OR (ub.blocker_id = ss.user_id AND ub.blocked_id = $1)
			)
		ORDER BY is_friend DESC, ss.response_count ASC, ss.created_at DESC
		LIMIT $3`,
		viewerID, before, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	signals := []SupportSignal{}
	for rows.Next() {
		signal, err := scanSupportSignal(rows)
		if err != nil {
			return nil, err
		}
		signals = append(signals, *signal)
	}
	return signals, rows.Err()
}

func (s *pgStore) CreateSupportSignal(ctx context.Context, userID uuid.UUID, input CreateSupportSignalInput, expiresAt time.Time) (*SupportSignal, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var cooldownCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*)
		FROM support_signals
		WHERE user_id = $1
			AND (
				(status = 'active' AND expires_at > NOW())
				OR COALESCE(resolved_at, cancelled_at, expires_at) > NOW() - INTERVAL '15 minutes'
			)`,
		userID,
	).Scan(&cooldownCount); err != nil {
		return nil, err
	}
	if cooldownCount > 0 {
		return nil, ErrConflict
	}

	var signalID uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO support_signals (user_id, reason, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id`,
		userID, input.Reason, expiresAt,
	).Scan(&signalID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return scanSupportSignal(s.pool.QueryRow(ctx, supportSignalSelect()+` WHERE ss.id = $2`, userID, signalID))
}

func (s *pgStore) ResolveSupportSignal(ctx context.Context, userID, signalID uuid.UUID) (*SupportSignal, error) {
	return s.closeSupportSignal(ctx, userID, signalID, "resolved")
}

func (s *pgStore) CancelSupportSignal(ctx context.Context, userID, signalID uuid.UUID) (*SupportSignal, error) {
	return s.closeSupportSignal(ctx, userID, signalID, "cancelled")
}

func (s *pgStore) closeSupportSignal(ctx context.Context, userID, signalID uuid.UUID, status string) (*SupportSignal, error) {
	timestampColumn := "resolved_at"
	if status == "cancelled" {
		timestampColumn = "cancelled_at"
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE support_signals
		SET status = $3,
			`+timestampColumn+` = NOW()
		WHERE id = $1
			AND user_id = $2
			AND status = 'active'`,
		signalID, userID, status,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return scanSupportSignal(s.pool.QueryRow(ctx, supportSignalSelect()+` WHERE ss.id = $2`, userID, signalID))
}

func (s *pgStore) RespondToSupportSignal(ctx context.Context, userID, signalID uuid.UUID) (*SupportSignalResponseResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var requesterID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT user_id
		FROM support_signals
		WHERE id = $1
			AND status = 'active'
			AND expires_at > NOW()
		FOR UPDATE`,
		signalID,
	).Scan(&requesterID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if requesterID == userID {
		return nil, ErrConflict
	}

	var blocked bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM user_blocks ub
			WHERE (ub.blocker_id = $1 AND ub.blocked_id = $2)
				OR (ub.blocker_id = $2 AND ub.blocked_id = $1)
		)`,
		userID, requesterID,
	).Scan(&blocked); err != nil {
		return nil, err
	}
	if blocked {
		return nil, ErrNotFound
	}

	chatID, err := findOrCreateReachOutChat(ctx, tx, userID, requesterID)
	if err != nil {
		return nil, err
	}

	tag, err := tx.Exec(ctx,
		`INSERT INTO support_signal_responses (signal_id, responder_id, chat_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (signal_id, responder_id) DO NOTHING`,
		signalID, userID, chatID,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE support_signals
			SET response_count = response_count + 1
			WHERE id = $1`,
			signalID,
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	signal, err := scanSupportSignal(s.pool.QueryRow(ctx, supportSignalSelect()+` WHERE ss.id = $2`, userID, signalID))
	if err != nil {
		return nil, err
	}
	return &SupportSignalResponseResult{Signal: signal, ChatID: chatID}, nil
}

func findOrCreateReachOutChat(ctx context.Context, q pgx.Tx, userID, otherUserID uuid.UUID) (uuid.UUID, error) {
	var chatID uuid.UUID
	err := q.QueryRow(ctx,
		`SELECT ch.id
		FROM chats ch
		JOIN chat_members cm1 ON cm1.chat_id = ch.id AND cm1.user_id = $1
		JOIN chat_members cm2 ON cm2.chat_id = ch.id AND cm2.user_id = $2
		WHERE ch.is_group = false
			AND ch.support_request_id IS NULL
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
		VALUES (false, NULL, 'active', NULL)
		RETURNING id`,
	).Scan(&chatID); err != nil {
		return uuid.Nil, err
	}
	if _, err := q.Exec(ctx,
		`INSERT INTO chat_members (chat_id, user_id, role)
		VALUES ($1, $2, 'requester'), ($1, $3, 'addressee')`,
		chatID, otherUserID, userID,
	); err != nil {
		return uuid.Nil, err
	}
	return chatID, nil
}

func supportSignalSelect() string {
	return `SELECT
			ss.id,
			ss.user_id,
			u.username,
			u.avatar_url,
			u.city,
			ss.reason,
			ss.status,
			ss.expires_at,
			ss.response_count,
			ss.created_at,
			ss.resolved_at,
			ss.cancelled_at,
			ss.user_id = $1 AS is_own_signal,
			EXISTS (
				SELECT 1 FROM friendships f
				WHERE f.status = 'accepted'
					AND (
						(f.user_a_id = $1 AND f.user_b_id = ss.user_id)
						OR (f.user_b_id = $1 AND f.user_a_id = ss.user_id)
					)
			) AS is_friend
		FROM support_signals ss
		JOIN users u ON u.id = ss.user_id
	`
}

type supportSignalScanner interface {
	Scan(dest ...any) error
}

func scanSupportSignal(row supportSignalScanner) (*SupportSignal, error) {
	var signal SupportSignal
	if err := row.Scan(
		&signal.ID,
		&signal.UserID,
		&signal.Username,
		&signal.AvatarURL,
		&signal.City,
		&signal.Reason,
		&signal.Status,
		&signal.ExpiresAt,
		&signal.ResponseCount,
		&signal.CreatedAt,
		&signal.ResolvedAt,
		&signal.CancelledAt,
		&signal.IsOwnSignal,
		&signal.IsFriend,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &signal, nil
}
