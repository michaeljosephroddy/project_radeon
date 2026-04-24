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
		`SELECT is_available_to_support, COALESCE(support_mode, ''), support_updated_at
		FROM users WHERE id = $1`,
		userID,
	).Scan(&p.IsAvailableToSupport, &p.SupportMode, &p.SupportUpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *pgStore) UpdateSupportProfile(ctx context.Context, userID uuid.UUID, available bool, mode string) (*SupportProfile, error) {
	var p SupportProfile
	err := s.pool.QueryRow(ctx,
		`UPDATE users
		SET
			is_available_to_support = $2,
			support_mode = $3,
			support_updated_at = NOW()
		WHERE id = $1
		RETURNING is_available_to_support, COALESCE(support_mode, ''), support_updated_at`,
		userID, available, mode,
	).Scan(&p.IsAvailableToSupport, &p.SupportMode, &p.SupportUpdatedAt)
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
			AND status = 'open'`,
		userID,
	).Scan(&count)
	return count, err
}

func (s *pgStore) CreateSupportRequest(
	ctx context.Context,
	userID uuid.UUID,
	reqType string,
	message *string,
	urgency string,
	priorityVisibility bool,
	priorityExpiresAt *time.Time,
) (*SupportRequest, error) {
	var req SupportRequest
	err := s.pool.QueryRow(ctx,
		`WITH inserted AS (
			INSERT INTO support_requests (
				requester_id, type, message, city, status, urgency, priority_visibility, priority_expires_at
			)
			SELECT u.id, $2, $3, u.city, 'open', $4, $5, $6
			FROM users u WHERE u.id = $1
			RETURNING id, requester_id, type, message, urgency, status, created_at, priority_visibility, priority_expires_at
		)
		SELECT
			i.id, i.requester_id, i.type, i.message, i.urgency, i.status, i.created_at,
			i.priority_visibility, i.priority_expires_at,
			u.username, u.avatar_url, u.city
		FROM inserted i
		JOIN users u ON u.id = i.requester_id`,
		userID, reqType, message, urgency, priorityVisibility, priorityExpiresAt,
	).Scan(
		&req.ID, &req.RequesterID, &req.Type, &req.Message, &req.Urgency, &req.Status, &req.CreatedAt,
		&req.PriorityVisibility, &req.PriorityExpiresAt,
		&req.Username, &req.AvatarURL, &req.City,
	)
	if err != nil {
		return nil, err
	}
	req.ResponseCount = 0
	req.HasResponded = false
	req.IsOwnRequest = true
	req.SortAt = req.CreatedAt
	if req.PriorityVisibility && req.PriorityExpiresAt != nil && req.PriorityExpiresAt.After(time.Now()) {
		req.SortAt = *req.PriorityExpiresAt
	}
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
			sr.urgency,
			CASE
				WHEN sr.status = 'open' THEN 'open'
				ELSE sr.status
			END AS status,
			sr.response_count,
			sr.created_at,
			sr.priority_visibility,
			sr.priority_expires_at,
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
		&req.Type, &req.Message, &req.Urgency, &req.Status, &req.ResponseCount,
		&req.CreatedAt, &req.PriorityVisibility, &req.PriorityExpiresAt, &req.HasResponded, &req.IsOwnRequest,
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
			sr.type, sr.message, sr.urgency, sr.status,
			sr.response_count, sr.created_at, sr.priority_visibility, sr.priority_expires_at,
			false AS has_responded,
			true AS is_own_request,
			sr.created_at AS sort_at
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
		`WITH viewer_data AS (
			SELECT
				u.support_mode,
				u.lat,
				u.lng,
				CASE WHEN u.sober_since IS NOT NULL
					THEN EXTRACT(EPOCH FROM (NOW() - u.sober_since::timestamptz)) / 86400.0
					ELSE NULL
				END AS days_sober
			FROM users u WHERE u.id = $1
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
				sr.id,
				sr.requester_id,
				sr.type,
				sr.message,
				sr.status,
				sr.urgency,
				sr.response_count,
				sr.created_at,
				sr.priority_visibility,
				sr.priority_expires_at,
				u.username,
				u.avatar_url,
				u.city,
				u.lat AS req_lat,
				u.lng AS req_lng,
				CASE
					WHEN u.sober_since IS NULL THEN NULL
					WHEN EXTRACT(EPOCH FROM (NOW() - u.sober_since::timestamptz)) / 86400.0 < 30   THEN 1
					WHEN EXTRACT(EPOCH FROM (NOW() - u.sober_since::timestamptz)) / 86400.0 < 90   THEN 2
					WHEN EXTRACT(EPOCH FROM (NOW() - u.sober_since::timestamptz)) / 86400.0 < 365  THEN 3
					WHEN EXTRACT(EPOCH FROM (NOW() - u.sober_since::timestamptz)) / 86400.0 < 730  THEN 4
					WHEN EXTRACT(EPOCH FROM (NOW() - u.sober_since::timestamptz)) / 86400.0 < 1825 THEN 5
					ELSE 6
				END AS cand_band,
				EXISTS(
					SELECT 1 FROM support_responses own_res
					WHERE own_res.support_request_id = sr.id
					  AND own_res.responder_id = $1
				) AS has_responded
			FROM support_requests sr
			JOIN users u ON u.id = sr.requester_id
			WHERE sr.status = 'open'
			  AND sr.requester_id != $1
			  AND ($2::timestamptz IS NULL OR sr.created_at < $2)
		)
		SELECT
			c.id,
			c.requester_id,
			c.username,
			c.avatar_url,
			c.city,
			c.type,
			c.message,
			c.urgency,
			c.status,
			c.response_count,
			c.created_at,
			c.priority_visibility,
			c.priority_expires_at,
			c.has_responded,
			false AS is_own_request,
			c.created_at AS sort_at,
			(
				CASE c.urgency
					WHEN 'right_now' THEN 0.40
					WHEN 'soon'      THEN 0.20
					ELSE 0.0
				END
				+ CASE
					WHEN (SELECT band FROM viewer_band) IS NULL OR c.cand_band IS NULL THEN 0.0
					WHEN (SELECT band FROM viewer_band) = c.cand_band                  THEN 0.35
					WHEN ABS((SELECT band FROM viewer_band) - c.cand_band) = 1         THEN 0.175
					ELSE 0.0
				  END
				+ 0.25 * EXP(-EXTRACT(EPOCH FROM (NOW() - c.created_at)) / 86400.0)
				+ CASE
					WHEN (SELECT support_mode FROM viewer_data) = 'nearby'
					     AND (SELECT lat FROM viewer_data) IS NOT NULL
					     AND (SELECT lng FROM viewer_data) IS NOT NULL
					     AND c.req_lat IS NOT NULL
					     AND c.req_lng IS NOT NULL
					THEN 0.30 * EXP(-(
						2.0 * 6371.0 * ASIN(SQRT(
							POWER(SIN(RADIANS((c.req_lat - (SELECT lat FROM viewer_data)) / 2.0)), 2)
							+ COS(RADIANS((SELECT lat FROM viewer_data))) * COS(RADIANS(c.req_lat))
							* POWER(SIN(RADIANS((c.req_lng - (SELECT lng FROM viewer_data)) / 2.0)), 2)
						))
					) / 300.0)
					ELSE 0.0
				  END
			) AS score,
			CASE
				WHEN c.priority_visibility = true
				     AND c.priority_expires_at IS NOT NULL
				     AND c.priority_expires_at > NOW()
				THEN true
				ELSE false
			END AS is_priority
		FROM candidates c
		CROSS JOIN viewer_band
		ORDER BY is_priority DESC, score DESC, c.id
		LIMIT $3`,
		userID, before, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []SupportRequest
	for rows.Next() {
		var req SupportRequest
		var score float64
		var isPriority bool
		if err := rows.Scan(
			&req.ID, &req.RequesterID, &req.Username, &req.AvatarURL, &req.City,
			&req.Type, &req.Message, &req.Urgency, &req.Status, &req.ResponseCount,
			&req.CreatedAt, &req.PriorityVisibility, &req.PriorityExpiresAt, &req.HasResponded, &req.IsOwnRequest, &req.SortAt,
			&score, &isPriority,
		); err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}
	return requests, rows.Err()
}

func (s *pgStore) FetchSupportSummary(ctx context.Context, viewerID uuid.UUID) (int, int, error) {
	var openCount int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*)
		FROM support_requests
		WHERE status = 'open'
		  AND requester_id != $1`,
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

func (s *pgStore) GetSupportRequestState(ctx context.Context, requestID uuid.UUID) (uuid.UUID, string, error) {
	var requesterID uuid.UUID
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT requester_id, status FROM support_requests WHERE id = $1`,
		requestID,
	).Scan(&requesterID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", ErrNotFound
	}
	return requesterID, status, err
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
			&req.Type, &req.Message, &req.Urgency, &req.Status, &req.ResponseCount,
			&req.CreatedAt, &req.PriorityVisibility, &req.PriorityExpiresAt, &req.HasResponded, &req.IsOwnRequest, &req.SortAt,
		); err != nil {
			return nil, err
		}
		if req.SortAt.IsZero() {
			req.SortAt = req.CreatedAt
		}
		requests = append(requests, req)
	}
	return requests, rows.Err()
}
