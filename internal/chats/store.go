package chats

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
)

type pgStore struct {
	pool *pgxpool.Pool
}

const chatSelectColumns = `SELECT
			ch.id,
			ch.is_group,
			ch.name,
			other.username,
			other.avatar_url,
			ch.created_at,
			m.body AS last_message,
			m.sent_at AS last_message_at,
			ch.status,
			sr.id AS support_request_id,
			sr.type AS support_request_type,
			sr.message AS support_request_message,
			sr.requester_id,
			requester.username AS requester_username,
			latest_support.response_type AS latest_response_type,
			CASE
				WHEN sr.id IS NULL THEN NULL
				WHEN ch.status = 'request' THEN 'pending_requester_acceptance'
				WHEN ch.status = 'active' THEN 'accepted'
				WHEN ch.status = 'declined' THEN 'declined'
				ELSE ch.status
			END AS support_status,
			CASE
				WHEN sr.id IS NULL THEN NULL
				WHEN ch.status = 'request' THEN sr.requester_id
				ELSE NULL
			END AS awaiting_user_id`

// NewPgStore wraps a pgxpool.Pool as the production Querier implementation.
func NewPgStore(pool *pgxpool.Pool) Querier {
	return &pgStore{pool: pool}
}

func (s *pgStore) ListChats(ctx context.Context, userID uuid.UUID, query string, limit, offset int) ([]Chat, error) {
	rows, err := s.pool.Query(ctx,
		chatSelectColumns+`
		FROM chats ch
		JOIN chat_members cm
			ON cm.chat_id = ch.id
			AND cm.user_id = $1
		LEFT JOIN LATERAL (
			SELECT
				u.username,
				u.avatar_url
			FROM chat_members cm2
			JOIN users u ON u.id = cm2.user_id
			WHERE cm2.chat_id = ch.id
				AND cm2.user_id != $1
			LIMIT 1
		) other ON NOT ch.is_group
		LEFT JOIN LATERAL (
			SELECT
				body,
				sent_at
			FROM messages
			WHERE chat_id = ch.id
			ORDER BY sent_at DESC
			LIMIT 1
		) m ON true
		LEFT JOIN support_requests sr ON sr.id = ch.support_request_id
		LEFT JOIN users requester ON requester.id = sr.requester_id
		LEFT JOIN LATERAL (
			SELECT response_type
			FROM support_responses
			WHERE chat_id = ch.id
			ORDER BY created_at DESC
			LIMIT 1
		) latest_support ON true
		WHERE (
				ch.status IN ('active', 'request')
				OR (ch.status = 'declined' AND ch.support_request_id IS NOT NULL)
			)
			AND (
				$2 = ''
				OR (
					ch.is_group = true
					AND COALESCE(ch.name, '') ILIKE '%' || $2 || '%'
				)
				OR (
					ch.is_group = false
					AND COALESCE(other.username, '') ILIKE '%' || $2 || '%'
				)
			)
		ORDER BY COALESCE(m.sent_at, ch.created_at) DESC
		LIMIT $3 OFFSET $4`,
		userID, query, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChats(rows)
}

func (s *pgStore) ListChatRequests(ctx context.Context, userID uuid.UUID) ([]Chat, error) {
	rows, err := s.pool.Query(ctx,
		chatSelectColumns+`
		FROM chats ch
		JOIN chat_members cm
			ON cm.chat_id = ch.id
			AND cm.user_id = $1
			AND cm.role = 'addressee'
		LEFT JOIN LATERAL (
			SELECT
				u.username,
				u.avatar_url
			FROM chat_members cm2
			JOIN users u ON u.id = cm2.user_id
			WHERE cm2.chat_id = ch.id
				AND cm2.user_id != $1
			LIMIT 1
		) other ON NOT ch.is_group
		LEFT JOIN LATERAL (
			SELECT
				body,
				sent_at
			FROM messages
			WHERE chat_id = ch.id
			ORDER BY sent_at DESC
			LIMIT 1
		) m ON true
		LEFT JOIN support_requests sr ON sr.id = ch.support_request_id
		LEFT JOIN users requester ON requester.id = sr.requester_id
		LEFT JOIN LATERAL (
			SELECT response_type
			FROM support_responses
			WHERE chat_id = ch.id
			ORDER BY created_at DESC
			LIMIT 1
		) latest_support ON true
		WHERE ch.status = 'request'
		ORDER BY ch.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChats(rows)
}

func (s *pgStore) GetChat(ctx context.Context, userID, chatID uuid.UUID) (*Chat, error) {
	rows, err := s.pool.Query(ctx,
		chatSelectColumns+`
		FROM chats ch
		JOIN chat_members cm
			ON cm.chat_id = ch.id
			AND cm.user_id = $1
		LEFT JOIN LATERAL (
			SELECT
				u.username,
				u.avatar_url
			FROM chat_members cm2
			JOIN users u ON u.id = cm2.user_id
			WHERE cm2.chat_id = ch.id
				AND cm2.user_id != $1
			LIMIT 1
		) other ON NOT ch.is_group
		LEFT JOIN LATERAL (
			SELECT
				body,
				sent_at
			FROM messages
			WHERE chat_id = ch.id
			ORDER BY sent_at DESC
			LIMIT 1
		) m ON true
		LEFT JOIN support_requests sr ON sr.id = ch.support_request_id
		LEFT JOIN users requester ON requester.id = sr.requester_id
		LEFT JOIN LATERAL (
			SELECT response_type
			FROM support_responses
			WHERE chat_id = ch.id
			ORDER BY created_at DESC
			LIMIT 1
		) latest_support ON true
		WHERE ch.id = $2
		LIMIT 1`,
		userID, chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chats, err := scanChats(rows)
	if err != nil {
		return nil, err
	}
	if len(chats) == 0 {
		return nil, ErrNotFound
	}
	return &chats[0], nil
}

func scanChats(rows pgx.Rows) ([]Chat, error) {
	var chats []Chat
	for rows.Next() {
		var ch Chat
		var supportRequestID *uuid.UUID
		var requestType *string
		var requestMessage *string
		var requesterID *uuid.UUID
		var requesterUsername *string
		var latestResponseType *string
		var supportStatus *string
		var awaitingUserID *uuid.UUID
		if err := rows.Scan(
			&ch.ID,
			&ch.IsGroup,
			&ch.Name,
			&ch.Username,
			&ch.AvatarURL,
			&ch.CreatedAt,
			&ch.LastMessage,
			&ch.LastMessageAt,
			&ch.Status,
			&supportRequestID,
			&requestType,
			&requestMessage,
			&requesterID,
			&requesterUsername,
			&latestResponseType,
			&supportStatus,
			&awaitingUserID,
		); err != nil {
			return nil, err
		}
		if supportRequestID != nil && requestType != nil && requesterID != nil && requesterUsername != nil && supportStatus != nil {
			ch.SupportContext = &SupportChatContext{
				SupportRequestID:   *supportRequestID,
				RequestType:        *requestType,
				RequestMessage:     requestMessage,
				RequesterID:        *requesterID,
				RequesterUsername:  *requesterUsername,
				LatestResponseType: latestResponseType,
				Status:             *supportStatus,
				AwaitingUserID:     awaitingUserID,
			}
		}
		chats = append(chats, ch)
	}
	return chats, rows.Err()
}

func (s *pgStore) GetChatStatus(ctx context.Context, chatID uuid.UUID) (string, error) {
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT status FROM chats WHERE id = $1`,
		chatID,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return status, err
}

func (s *pgStore) FindDirectChat(ctx context.Context, userID, otherUserID uuid.UUID) (uuid.UUID, bool, error) {
	var chatID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT ch.id
		FROM chats ch
		JOIN chat_members cm1
			ON cm1.chat_id = ch.id
			AND cm1.user_id = $1
		JOIN chat_members cm2
			ON cm2.chat_id = ch.id
			AND cm2.user_id = $2
		WHERE ch.is_group = false
			AND ch.support_request_id IS NULL
		LIMIT 1`,
		userID, otherUserID,
	).Scan(&chatID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	return chatID, true, nil
}

func (s *pgStore) CreateChat(ctx context.Context, userID uuid.UUID, isGroup bool, name *string, memberIDs []uuid.UUID) (uuid.UUID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	var chatID uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO chats (is_group, name, status, support_request_id) VALUES ($1, $2, 'active', NULL) RETURNING id`,
		isGroup, name,
	).Scan(&chatID); err != nil {
		return uuid.Nil, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO chat_members (chat_id, user_id, role) VALUES ($1, $2, 'requester')`,
		chatID, userID,
	); err != nil {
		return uuid.Nil, err
	}

	for _, memberID := range memberIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO chat_members (chat_id, user_id, role) VALUES ($1, $2, 'addressee')`,
			chatID, memberID,
		); err != nil {
			return uuid.Nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return chatID, nil
}

func (s *pgStore) IsAddresseeOfChat(ctx context.Context, chatID, userID uuid.UUID) (bool, error) {
	var is bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1
			FROM chat_members
			WHERE chat_id = $1
				AND user_id = $2
				AND role = 'addressee'
		)`,
		chatID, userID,
	).Scan(&is)
	return is, err
}

func (s *pgStore) AcceptChatRequest(ctx context.Context, chatID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chats SET status = 'active' WHERE id = $1`,
		chatID,
	)
	return err
}

func (s *pgStore) DeclineChatRequest(ctx context.Context, chatID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chats SET status = 'declined' WHERE id = $1`,
		chatID,
	)
	return err
}

func (s *pgStore) IsMemberOfChat(ctx context.Context, chatID, userID uuid.UUID) (bool, error) {
	var is bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1
			FROM chat_members
			WHERE chat_id = $1
				AND user_id = $2
		)`,
		chatID, userID,
	).Scan(&is)
	return is, err
}

func (s *pgStore) ListMessages(ctx context.Context, chatID uuid.UUID, before *time.Time, limit int) ([]Message, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			m.id,
			m.sender_id,
			u.username,
			u.avatar_url,
			m.body,
			m.sent_at
		FROM messages m
		JOIN users u ON u.id = m.sender_id
		WHERE m.chat_id = $1
			AND ($2::timestamptz IS NULL OR m.sent_at < $2)
		ORDER BY m.sent_at DESC
		LIMIT $3`,
		chatID, before, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SenderID, &m.Username, &m.AvatarURL, &m.Body, &m.SentAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *pgStore) InsertMessage(ctx context.Context, chatID, userID uuid.UUID, body string) (uuid.UUID, error) {
	var msgID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`INSERT INTO messages (chat_id, sender_id, body) VALUES ($1, $2, $3) RETURNING id`,
		chatID, userID, body,
	).Scan(&msgID)
	return msgID, err
}

// DeleteOrLeaveChat removes the user from a group or deletes a direct chat entirely.
// Returns "left" for groups and "deleted" for direct chats.
func (s *pgStore) DeleteOrLeaveChat(ctx context.Context, chatID, userID uuid.UUID) (string, error) {
	var isGroup bool
	var isMember bool
	err := s.pool.QueryRow(ctx,
		`SELECT
			ch.is_group,
			EXISTS(
				SELECT 1
				FROM chat_members cm
				WHERE cm.chat_id = ch.id
					AND cm.user_id = $2
			) AS is_member
		FROM chats ch
		WHERE ch.id = $1`,
		chatID, userID,
	).Scan(&isGroup, &isMember)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if !isMember {
		return "", ErrForbidden
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	action := "deleted"
	if isGroup {
		action = "left"
		if _, err := tx.Exec(ctx,
			`DELETE FROM chat_members WHERE chat_id = $1 AND user_id = $2`,
			chatID, userID,
		); err != nil {
			return "", err
		}

		var remaining int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM chat_members WHERE chat_id = $1`,
			chatID,
		).Scan(&remaining); err != nil {
			return "", err
		}

		if remaining == 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM messages WHERE chat_id = $1`, chatID); err != nil {
				return "", err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM chats WHERE id = $1`, chatID); err != nil {
				return "", err
			}
		}
	} else {
		if _, err := tx.Exec(ctx, `DELETE FROM messages WHERE chat_id = $1`, chatID); err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM chat_members WHERE chat_id = $1`, chatID); err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM chats WHERE id = $1`, chatID); err != nil {
			return "", err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return action, nil
}
