package support

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

func (s *pgStore) GetSupportProfile(ctx context.Context, userID uuid.UUID) (*SupportProfile, error) {
	var p SupportProfile
	err := s.pool.QueryRow(ctx,
		`SELECT is_available_to_support, support_modes, support_updated_at
		FROM users WHERE id = $1`,
		userID,
	).Scan(&p.IsAvailableToSupport, &p.SupportModes, &p.SupportUpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *pgStore) UpdateSupportProfile(ctx context.Context, userID uuid.UUID, available bool, modes []string) (*SupportProfile, error) {
	var p SupportProfile
	err := s.pool.QueryRow(ctx,
		`UPDATE users
		SET
			is_available_to_support = $2,
			support_modes = $3,
			support_updated_at = NOW()
		WHERE id = $1
		RETURNING is_available_to_support, support_modes, support_updated_at`,
		userID, available, modes,
	).Scan(&p.IsAvailableToSupport, &p.SupportModes, &p.SupportUpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *pgStore) CountOpenSupportRequests(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*)
		FROM support_requests
		WHERE requester_id = $1
			AND status = 'open'
			AND expires_at > NOW()`,
		userID,
	).Scan(&count)
	return count, err
}

func (s *pgStore) CreateSupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, audience string, expiresAt time.Time) (*SupportRequest, error) {
	var req SupportRequest
	err := s.pool.QueryRow(ctx,
		`WITH inserted AS (
			INSERT INTO support_requests (
				requester_id, type, message, audience, city, status, expires_at
			)
			SELECT u.id, $2, $3, $4, u.city, 'open', $5
			FROM users u WHERE u.id = $1
			RETURNING id, requester_id, type, message, audience, status, expires_at, created_at
		)
		SELECT
			i.id, i.requester_id, i.type, i.message, i.audience, i.status, i.expires_at, i.created_at,
			u.username, u.avatar_url, u.city
		FROM inserted i
		JOIN users u ON u.id = i.requester_id`,
		userID, reqType, message, audience, expiresAt,
	).Scan(
		&req.ID, &req.RequesterID, &req.Type, &req.Message, &req.Audience, &req.Status, &req.ExpiresAt, &req.CreatedAt,
		&req.Username, &req.AvatarURL, &req.City,
	)
	if err != nil {
		return nil, err
	}
	req.ResponseCount = 0
	req.HasResponded = false
	req.IsOwnRequest = true
	return &req, nil
}

func (s *pgStore) GetSupportRequest(ctx context.Context, viewerID, requestID uuid.UUID) (*SupportRequest, error) {
	var req SupportRequest
	err := s.pool.QueryRow(ctx,
		`SELECT
			sr.id,
			sr.requester_id,
			u.username,
			u.avatar_url,
			u.city,
			sr.type,
			sr.message,
			sr.audience,
			CASE
				WHEN sr.status = 'open' AND sr.expires_at <= NOW() THEN 'expired'
				ELSE sr.status
			END AS status,
			sr.response_count,
			sr.expires_at,
			sr.created_at,
			EXISTS(
				SELECT 1 FROM support_responses own_res
				WHERE own_res.support_request_id = sr.id
					AND own_res.responder_id = $2
			) AS has_responded,
			sr.requester_id = $2 AS is_own_request
		FROM support_requests sr
		JOIN users u ON u.id = sr.requester_id
		WHERE sr.id = $1`,
		requestID, viewerID,
	).Scan(
		&req.ID, &req.RequesterID, &req.Username, &req.AvatarURL, &req.City,
		&req.Type, &req.Message, &req.Audience, &req.Status, &req.ResponseCount,
		&req.ExpiresAt, &req.CreatedAt, &req.HasResponded, &req.IsOwnRequest,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &req, nil
}

func (s *pgStore) CloseSupportRequest(ctx context.Context, requestID, userID uuid.UUID) error {
	result, err := s.pool.Exec(ctx,
		`UPDATE support_requests
		SET status = 'closed', closed_at = NOW()
		WHERE id = $1 AND requester_id = $2`,
		requestID, userID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgStore) ListMySupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			sr.id, sr.requester_id, u.username, u.avatar_url, u.city,
			sr.type, sr.message, sr.audience,
			CASE
				WHEN sr.status = 'open' AND sr.expires_at <= NOW() THEN 'expired'
				ELSE sr.status
			END AS status,
			sr.response_count, sr.expires_at, sr.created_at,
			false AS has_responded,
			true AS is_own_request
		FROM support_requests sr
		JOIN users u ON u.id = sr.requester_id
		WHERE sr.requester_id = $1
			AND ($2::timestamptz IS NULL OR sr.created_at < $2)
		ORDER BY sr.created_at DESC
		LIMIT $3`,
		userID, before, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSupportRequests(rows)
}

func (s *pgStore) ListVisibleSupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			sr.id, sr.requester_id, u.username, u.avatar_url, u.city,
			sr.type, sr.message, sr.audience, sr.status, sr.response_count,
			sr.expires_at, sr.created_at,
			EXISTS(
				SELECT 1 FROM support_responses own_res
				WHERE own_res.support_request_id = sr.id
					AND own_res.responder_id = $1
			) AS has_responded,
			false AS is_own_request
		FROM support_requests sr
		JOIN users u ON u.id = sr.requester_id
		WHERE sr.status = 'open'
			AND sr.expires_at > NOW()
			AND sr.requester_id != $1
			AND ($2::timestamptz IS NULL OR sr.created_at < $2)
			AND (
				sr.audience = 'community'
				OR (sr.audience = 'city' AND u.city IS NOT NULL AND u.city = (SELECT city FROM users WHERE id = $1))
				OR (
					sr.audience = 'friends'
					AND EXISTS(
						SELECT 1 FROM friendships f
						WHERE (f.user_a_id = $1 OR f.user_b_id = $1)
							AND (f.user_a_id = sr.requester_id OR f.user_b_id = sr.requester_id)
							AND f.status = 'accepted'
					)
				)
			)
		ORDER BY sr.created_at DESC
		LIMIT $3`,
		userID, before, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSupportRequests(rows)
}

func (s *pgStore) FetchSupportSummary(ctx context.Context, viewerID uuid.UUID) (int, int, error) {
	var openCount int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*)
		FROM support_requests sr
		JOIN users u ON u.id = sr.requester_id
		WHERE sr.status = 'open'
			AND sr.expires_at > NOW()
			AND sr.requester_id != $1
			AND (
				sr.audience = 'community'
				OR (sr.audience = 'city' AND u.city IS NOT NULL AND u.city = (SELECT city FROM users WHERE id = $1))
				OR (
					sr.audience = 'friends'
					AND EXISTS(
						SELECT 1 FROM friendships f
						WHERE (f.user_a_id = $1 OR f.user_b_id = $1)
							AND (f.user_a_id = sr.requester_id OR f.user_b_id = sr.requester_id)
							AND f.status = 'accepted'
					)
				)
			)`,
		viewerID,
	).Scan(&openCount)
	if err != nil {
		return 0, 0, err
	}

	var availableCount int
	err = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM users WHERE id != $1 AND is_available_to_support = true`,
		viewerID,
	).Scan(&availableCount)
	if err != nil {
		return 0, 0, err
	}

	return openCount, availableCount, nil
}

func (s *pgStore) GetSupportRequestState(ctx context.Context, requestID uuid.UUID) (uuid.UUID, string, time.Time, error) {
	var requesterID uuid.UUID
	var status string
	var expiresAt time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT requester_id, status, expires_at FROM support_requests WHERE id = $1`,
		requestID,
	).Scan(&requesterID, &status, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", time.Time{}, ErrNotFound
	}
	return requesterID, status, expiresAt, err
}

func (s *pgStore) CreateSupportResponse(ctx context.Context, requestID, userID uuid.UUID, responseType string, message *string, scheduledFor *time.Time) (*CreateSupportResponseResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var requesterID uuid.UUID
	var requestType string
	var requestMessage *string
	var requesterUsername string
	var requesterAvatarURL *string
	err = tx.QueryRow(ctx,
		`SELECT sr.requester_id, sr.type, sr.message, u.username, u.avatar_url
		FROM support_requests sr
		JOIN users u ON u.id = sr.requester_id
		WHERE sr.id = $1`,
		requestID,
	).Scan(&requesterID, &requestType, &requestMessage, &requesterUsername, &requesterAvatarURL)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	formattedMessage := formatSupportResponseMessage(responseType, message, scheduledFor)
	messageForInsert := formattedMessage

	var chatID uuid.UUID
	var chatCreatedAt time.Time
	var chatStatus string
	err = tx.QueryRow(ctx,
		`SELECT ch.id, ch.created_at, ch.status
		FROM chats ch
		JOIN chat_members requester_member
			ON requester_member.chat_id = ch.id
			AND requester_member.user_id = $2
		JOIN chat_members responder_member
			ON responder_member.chat_id = ch.id
			AND responder_member.user_id = $3
		WHERE ch.is_group = false
			AND ch.support_request_id = $1
		ORDER BY ch.created_at DESC
		LIMIT 1`,
		requestID, requesterID, userID,
	).Scan(&chatID, &chatCreatedAt, &chatStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx,
			`INSERT INTO chats (is_group, name, status, support_request_id)
			VALUES (false, NULL, 'request', $1)
			RETURNING id, created_at, status`,
			requestID,
		).Scan(&chatID, &chatCreatedAt, &chatStatus)
		if err != nil {
			return nil, err
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO chat_members (chat_id, user_id, role) VALUES ($1, $2, 'requester')`,
			chatID, userID,
		); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO chat_members (chat_id, user_id, role) VALUES ($1, $2, 'addressee')`,
			chatID, requesterID,
		); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	var res SupportResponse
	err = tx.QueryRow(ctx,
		`WITH inserted AS (
			INSERT INTO support_responses (
				support_request_id, responder_id, response_type, message, scheduled_for, chat_id
			)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id, support_request_id, responder_id, response_type, message, scheduled_for, created_at, chat_id
		)
		SELECT
			i.id, i.support_request_id, i.responder_id,
			u.username, u.avatar_url, u.city,
			i.response_type, i.message, i.scheduled_for, i.created_at, i.chat_id
		FROM inserted i
		JOIN users u ON u.id = i.responder_id`,
		requestID, userID, responseType, messageForInsert, scheduledFor, chatID,
	).Scan(
		&res.ID, &res.SupportRequestID, &res.ResponderID,
		&res.Username, &res.AvatarURL, &res.City,
		&res.ResponseType, &res.Message, &res.ScheduledFor, &res.CreatedAt, &res.ChatID,
	)
	if err != nil {
		return nil, err
	}

	var lastMessageAt time.Time
	if err := tx.QueryRow(ctx,
		`INSERT INTO messages (chat_id, sender_id, body) VALUES ($1, $2, $3) RETURNING sent_at`,
		chatID, userID, messageForInsert,
	).Scan(&lastMessageAt); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE support_requests SET response_count = response_count + 1 WHERE id = $1`,
		requestID,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	latestResponseType := res.ResponseType
	awaitingUserID := requesterID
	chat := &ChatSummary{
		ID:            chatID,
		IsGroup:       false,
		Username:      &requesterUsername,
		AvatarURL:     requesterAvatarURL,
		CreatedAt:     chatCreatedAt,
		LastMessage:   &messageForInsert,
		LastMessageAt: &lastMessageAt,
		Status:        chatStatus,
		SupportContext: &SupportChatContext{
			SupportRequestID:   requestID,
			RequestType:        requestType,
			RequestMessage:     requestMessage,
			RequesterID:        requesterID,
			RequesterUsername:  requesterUsername,
			LatestResponseType: &latestResponseType,
			Status:             mapSupportChatStatus(chatStatus),
			AwaitingUserID:     &awaitingUserID,
		},
	}

	return &CreateSupportResponseResult{Response: &res, Chat: chat}, nil
}

func (s *pgStore) GetSupportRequestOwner(ctx context.Context, requestID uuid.UUID) (uuid.UUID, error) {
	var requesterID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT requester_id FROM support_requests WHERE id = $1`,
		requestID,
	).Scan(&requesterID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	return requesterID, err
}

func (s *pgStore) ListSupportResponses(ctx context.Context, requestID uuid.UUID, limit, offset int) ([]SupportResponse, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			sres.id, sres.support_request_id, sres.responder_id,
			u.username, u.avatar_url, u.city,
			sres.response_type, sres.message, sres.scheduled_for, sres.created_at, sres.chat_id
		FROM support_responses sres
		JOIN users u ON u.id = sres.responder_id
		WHERE sres.support_request_id = $1
		ORDER BY sres.created_at ASC
		LIMIT $2 OFFSET $3`,
		requestID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var responses []SupportResponse
	for rows.Next() {
		var res SupportResponse
		if err := rows.Scan(&res.ID, &res.SupportRequestID, &res.ResponderID, &res.Username, &res.AvatarURL, &res.City, &res.ResponseType, &res.Message, &res.ScheduledFor, &res.CreatedAt, &res.ChatID); err != nil {
			return nil, err
		}
		responses = append(responses, res)
	}
	return responses, rows.Err()
}

func mapSupportChatStatus(chatStatus string) string {
	switch chatStatus {
	case "request":
		return "pending_requester_acceptance"
	case "active":
		return "accepted"
	default:
		return chatStatus
	}
}

func scanSupportRequests(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]SupportRequest, error) {
	var requests []SupportRequest
	for rows.Next() {
		var req SupportRequest
		if err := rows.Scan(
			&req.ID, &req.RequesterID, &req.Username, &req.AvatarURL, &req.City,
			&req.Type, &req.Message, &req.Audience, &req.Status, &req.ResponseCount,
			&req.ExpiresAt, &req.CreatedAt, &req.HasResponded, &req.IsOwnRequest,
		); err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}
	return requests, rows.Err()
}
