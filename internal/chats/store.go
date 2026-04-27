package chats

import (
	"context"
	"errors"
	"sync"
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

	messageSchemaOnce           sync.Once
	messageSchemaRealtimeFields bool
	messageSchemaErr            error
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
			COALESCE(unread.unread_count, 0) AS unread_count,
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

func (s *pgStore) ListChats(ctx context.Context, userID uuid.UUID, query string, before *ChatListCursor, limit int) ([]Chat, error) {
	var beforeActivityAt *time.Time
	var beforeChatID *uuid.UUID
	if before != nil {
		normalizedActivityAt := before.ActivityAt.UTC()
		normalizedChatID := before.ChatID
		beforeActivityAt = &normalizedActivityAt
		beforeChatID = &normalizedChatID
	}

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
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS unread_count
			FROM messages unread
			LEFT JOIN chat_reads cr
				ON cr.chat_id = ch.id
				AND cr.user_id = $1
			WHERE unread.chat_id = ch.id
				AND unread.sender_id <> $1
				AND (cr.last_read_at IS NULL OR unread.sent_at > cr.last_read_at)
		) unread ON true
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
			AND (
				$3::timestamptz IS NULL
				OR COALESCE(m.sent_at, ch.created_at) < $3
				OR (
					COALESCE(m.sent_at, ch.created_at) = $3
					AND ch.id < $4
				)
			)
		ORDER BY COALESCE(m.sent_at, ch.created_at) DESC, ch.id DESC
		LIMIT $5`,
		userID, query, beforeActivityAt, beforeChatID, limit,
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
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS unread_count
			FROM messages unread
			LEFT JOIN chat_reads cr
				ON cr.chat_id = ch.id
				AND cr.user_id = $1
			WHERE unread.chat_id = ch.id
				AND unread.sender_id <> $1
				AND (cr.last_read_at IS NULL OR unread.sent_at > cr.last_read_at)
		) unread ON true
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
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS unread_count
			FROM messages unread
			LEFT JOIN chat_reads cr
				ON cr.chat_id = ch.id
				AND cr.user_id = $1
			WHERE unread.chat_id = ch.id
				AND unread.sender_id <> $1
				AND (cr.last_read_at IS NULL OR unread.sent_at > cr.last_read_at)
		) unread ON true
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
			&ch.UnreadCount,
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

func (s *pgStore) ListChatMemberIDs(ctx context.Context, chatID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id
		FROM chat_members
		WHERE chat_id = $1`,
		chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memberIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var memberID uuid.UUID
		if err := rows.Scan(&memberID); err != nil {
			return nil, err
		}
		memberIDs = append(memberIDs, memberID)
	}
	return memberIDs, rows.Err()
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

func (s *pgStore) messageRealtimeFieldsEnabled(ctx context.Context) (bool, error) {
	s.messageSchemaOnce.Do(func() {
		var clientMessageIDExists bool
		s.messageSchemaErr = s.pool.QueryRow(ctx,
			`SELECT EXISTS (
				SELECT 1
				FROM information_schema.columns
				WHERE table_name = 'messages'
					AND column_name = 'client_message_id'
			)`,
		).Scan(&clientMessageIDExists)
		if s.messageSchemaErr != nil {
			return
		}

		var chatSeqExists bool
		s.messageSchemaErr = s.pool.QueryRow(ctx,
			`SELECT EXISTS (
				SELECT 1
				FROM information_schema.columns
				WHERE table_name = 'messages'
					AND column_name = 'chat_seq'
			)`,
		).Scan(&chatSeqExists)
		if s.messageSchemaErr != nil {
			return
		}

		s.messageSchemaRealtimeFields = clientMessageIDExists && chatSeqExists
	})

	return s.messageSchemaRealtimeFields, s.messageSchemaErr
}

func (s *pgStore) ListMessages(ctx context.Context, chatID, userID uuid.UUID, before *time.Time, limit int) ([]Message, *uuid.UUID, error) {
	realtimeFieldsEnabled, err := s.messageRealtimeFieldsEnabled(ctx)
	if err != nil {
		return nil, nil, err
	}

	query := `SELECT
			m.id,
			m.chat_id,
			m.sender_id,
			u.username,
			u.avatar_url,
			m.body,
			m.sent_at,
			m.client_message_id,
			m.chat_seq
		FROM messages m
		JOIN users u ON u.id = m.sender_id
		WHERE m.chat_id = $1
			AND ($2::timestamptz IS NULL OR m.sent_at < $2)
		ORDER BY m.chat_seq DESC, m.sent_at DESC
		LIMIT $3`
	if !realtimeFieldsEnabled {
		query = `SELECT
			m.id,
			m.chat_id,
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
		LIMIT $3`
	}

	rows, err := s.pool.Query(ctx, query, chatID, before, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if realtimeFieldsEnabled {
			if err := rows.Scan(&m.ID, &m.ChatID, &m.SenderID, &m.Username, &m.AvatarURL, &m.Body, &m.SentAt, &m.ClientMessageID, &m.ChatSeq); err != nil {
				return nil, nil, err
			}
		} else {
			if err := rows.Scan(&m.ID, &m.ChatID, &m.SenderID, &m.Username, &m.AvatarURL, &m.Body, &m.SentAt); err != nil {
				return nil, nil, err
			}
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	var otherUserLastReadMessageID *uuid.UUID
	err = s.pool.QueryRow(ctx,
		`SELECT cr.last_read_message_id
		FROM chats ch
		JOIN chat_members cm
			ON cm.chat_id = ch.id
			AND cm.user_id <> $2
		LEFT JOIN chat_reads cr
			ON cr.chat_id = ch.id
			AND cr.user_id = cm.user_id
		WHERE ch.id = $1
			AND ch.is_group = false
		LIMIT 1`,
		chatID, userID,
	).Scan(&otherUserLastReadMessageID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		otherUserLastReadMessageID = nil
	}

	return msgs, otherUserLastReadMessageID, nil
}

func (s *pgStore) InsertMessage(ctx context.Context, chatID, userID uuid.UUID, body string, clientMessageID *string) (*Message, error) {
	realtimeFieldsEnabled, err := s.messageRealtimeFieldsEnabled(ctx)
	if err != nil {
		return nil, err
	}

	if !realtimeFieldsEnabled {
		var message Message
		err = s.pool.QueryRow(ctx,
			`WITH inserted AS (
				INSERT INTO messages (chat_id, sender_id, body)
				VALUES ($1, $2, $3)
				RETURNING id, chat_id, sender_id, body, sent_at
			)
			SELECT
				inserted.id,
				inserted.chat_id,
				inserted.sender_id,
				u.username,
				u.avatar_url,
				inserted.body,
				inserted.sent_at
			FROM inserted
			JOIN users u ON u.id = inserted.sender_id`,
			chatID, userID, body,
		).Scan(
			&message.ID,
			&message.ChatID,
			&message.SenderID,
			&message.Username,
			&message.AvatarURL,
			&message.Body,
			&message.SentAt,
		)
		if err != nil {
			return nil, err
		}
		return &message, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if clientMessageID != nil {
		normalizedID := *clientMessageID
		if normalizedID != "" {
			var existing Message
			err = tx.QueryRow(ctx,
				`SELECT
					m.id,
					m.chat_id,
					m.sender_id,
					u.username,
					u.avatar_url,
					m.body,
					m.sent_at,
					m.client_message_id,
					m.chat_seq
				FROM messages m
				JOIN users u ON u.id = m.sender_id
				WHERE m.chat_id = $1
					AND m.client_message_id = $2
				LIMIT 1`,
				chatID, normalizedID,
			).Scan(
				&existing.ID,
				&existing.ChatID,
				&existing.SenderID,
				&existing.Username,
				&existing.AvatarURL,
				&existing.Body,
				&existing.SentAt,
				&existing.ClientMessageID,
				&existing.ChatSeq,
			)
			if err == nil {
				if commitErr := tx.Commit(ctx); commitErr != nil {
					return nil, commitErr
				}
				return &existing, nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
		}
	}

	if _, err := tx.Exec(ctx, `SELECT id FROM chats WHERE id = $1 FOR UPDATE`, chatID); err != nil {
		return nil, err
	}

	var nextSeq int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(chat_seq), 0) + 1
		FROM messages
		WHERE chat_id = $1`,
		chatID,
	).Scan(&nextSeq); err != nil {
		return nil, err
	}

	var message Message
	err = tx.QueryRow(ctx,
		`WITH inserted AS (
			INSERT INTO messages (chat_id, sender_id, body, client_message_id, chat_seq)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id, chat_id, sender_id, body, sent_at, client_message_id, chat_seq
		)
		SELECT
			inserted.id,
			inserted.chat_id,
			inserted.sender_id,
			u.username,
			u.avatar_url,
			inserted.body,
			inserted.sent_at,
			inserted.client_message_id,
			inserted.chat_seq
		FROM inserted
		JOIN users u ON u.id = inserted.sender_id`,
		chatID, userID, body, clientMessageID, nextSeq,
	).Scan(
		&message.ID,
		&message.ChatID,
		&message.SenderID,
		&message.Username,
		&message.AvatarURL,
		&message.Body,
		&message.SentAt,
		&message.ClientMessageID,
		&message.ChatSeq,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &message, nil
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
