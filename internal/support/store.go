package support

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

const supportChatClosedMessage = "This support request has been completed. This chat is now closed."

type pgStore struct {
	pool *pgxpool.Pool
}

const (
	immediateSupportCoolingWindow = 10 * time.Minute
	generalSupportCoolingWindow   = time.Hour
)

// NewPgStore wraps a pgxpool.Pool as the production Querier implementation.
func NewPgStore(pool *pgxpool.Pool) Querier {
	return &pgStore{pool: pool}
}

func (s *pgStore) CountOpenSupportRequests(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*)
		FROM support_requests
		WHERE requester_id = $1
			AND status IN ('open', 'active')`,
		userID,
	).Scan(&count)
	return count, err
}

func (s *pgStore) GetSupportRequest(ctx context.Context, viewerID, requestID uuid.UUID) (*SupportRequest, error) {
	var req SupportRequest
	err := s.pool.QueryRow(ctx,
		`SELECT
			sr.id,
			sr.requester_id,
			requester.username,
			requester.avatar_url,
			requester.city,
			sr.type,
			sr.message,
			sr.urgency,
			sr.status,
			sr.response_count,
			sr.created_at,
			sr.channel,
			sr.privacy_level,
			sr.accepted_response_id,
			sr.accepted_responder_id,
			sr.accepted_at,
			sr.closed_at,
			sr.accepted_responder_id,
			responder.username,
			responder.avatar_url,
			sr.chat_id,
			EXISTS(
				SELECT 1 FROM support_responses own_res
				WHERE own_res.support_request_id = sr.id
					AND own_res.responder_id = $2
			) AS has_responded,
			sr.requester_id = $2 AS is_own_request
		FROM support_requests sr
		JOIN users requester ON requester.id = sr.requester_id
		LEFT JOIN users responder ON responder.id = sr.accepted_responder_id
		WHERE sr.id = $1`,
		requestID, viewerID,
	).Scan(
		&req.ID, &req.RequesterID, &req.Username, &req.AvatarURL, &req.City,
		&req.Type, &req.Message, &req.Urgency, &req.Status, &req.ResponseCount,
		&req.CreatedAt, &req.Channel, &req.PrivacyLevel,
		&req.AcceptedResponseID, &req.AcceptedResponderID, &req.AcceptedAt, &req.ClosedAt,
		&req.ResponderID, &req.ResponderUsername, &req.ResponderAvatarURL, &req.ChatID,
		&req.HasResponded, &req.IsOwnRequest,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &req, nil
}

func (s *pgStore) CloseSupportRequest(ctx context.Context, requestID, userID uuid.UUID) ([]uuid.UUID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var requesterID uuid.UUID
	var status string
	var acceptedResponderID *uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT
			sr.requester_id,
			sr.status,
			sr.accepted_responder_id
		FROM support_requests sr
		WHERE sr.id = $1
		FOR UPDATE OF sr`,
		requestID,
	).Scan(&requesterID, &status, &acceptedResponderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	isRequester := requesterID == userID
	isAcceptedResponder := acceptedResponderID != nil && *acceptedResponderID == userID
	if !isRequester && !isAcceptedResponder {
		return nil, ErrNotFound
	}
	if status == "closed" {
		return nil, tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE support_requests
		SET
			status = 'closed',
			closed_at = NOW()
		WHERE id = $1`,
		requestID,
	); err != nil {
		return nil, err
	}

	chatRows, err := tx.Query(ctx,
		`SELECT id
		FROM chats
		WHERE support_request_id = $1
		  AND is_group = false
		  AND status <> 'closed'
		FOR UPDATE`,
		requestID,
	)
	if err != nil {
		return nil, err
	}

	var chatIDs []uuid.UUID
	for chatRows.Next() {
		var chatID uuid.UUID
		if err := chatRows.Scan(&chatID); err != nil {
			chatRows.Close()
			return nil, err
		}
		chatIDs = append(chatIDs, chatID)
	}
	if err := chatRows.Err(); err != nil {
		chatRows.Close()
		return nil, err
	}
	chatRows.Close()

	for _, chatID := range chatIDs {
		var nextSeq int64
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(MAX(chat_seq), 0) + 1
			FROM messages
			WHERE chat_id = $1`,
			chatID,
		).Scan(&nextSeq); err != nil {
			return nil, err
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO messages (chat_id, sender_id, kind, body, chat_seq)
			VALUES ($1, $2, 'system', $3, $4)`,
			chatID,
			userID,
			supportChatClosedMessage,
			nextSeq,
		); err != nil {
			return nil, err
		}

		if _, err := tx.Exec(ctx,
			`UPDATE chats
			SET status = 'closed'
			WHERE id = $1`,
			chatID,
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return chatIDs, nil
}

func (s *pgStore) ListMySupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			sr.id, sr.requester_id, requester.username, requester.avatar_url, requester.city,
			sr.type, sr.message, sr.urgency, sr.status,
			sr.response_count, sr.created_at,
			sr.channel,
			sr.privacy_level,
			sr.accepted_response_id,
			sr.accepted_responder_id,
			sr.accepted_at,
			sr.closed_at,
			sr.accepted_responder_id,
			responder.username,
			responder.avatar_url,
			sr.chat_id,
			false AS has_responded,
			true AS is_own_request,
			sr.created_at AS sort_at
		FROM support_requests sr
		JOIN users requester ON requester.id = sr.requester_id
		LEFT JOIN users responder ON responder.id = sr.accepted_responder_id
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

	var requests []SupportRequest
	for rows.Next() {
		var req SupportRequest
		if err := rows.Scan(
			&req.ID, &req.RequesterID, &req.Username, &req.AvatarURL, &req.City,
			&req.Type, &req.Message, &req.Urgency, &req.Status, &req.ResponseCount,
			&req.CreatedAt, &req.Channel, &req.PrivacyLevel,
			&req.AcceptedResponseID, &req.AcceptedResponderID, &req.AcceptedAt, &req.ClosedAt,
			&req.ResponderID, &req.ResponderUsername, &req.ResponderAvatarURL, &req.ChatID,
			&req.HasResponded, &req.IsOwnRequest, &req.SortAt,
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

func supportUrgencyRank(urgency string) int {
	switch urgency {
	case "right_now":
		return 0
	case "soon":
		return 1
	default:
		return 2
	}
}

func supportAttentionBucket(channel SupportChannel, hasResponded bool, responseCount int, lastResponseAt *time.Time, now time.Time) int {
	if hasResponded {
		return 3
	}
	if responseCount == 0 {
		return 0
	}
	if responseCount >= 3 {
		return 2
	}

	coolingWindow := generalSupportCoolingWindow
	if channel == SupportChannelImmediate {
		coolingWindow = immediateSupportCoolingWindow
	}
	if lastResponseAt != nil && lastResponseAt.After(now.Add(-coolingWindow)) {
		return 2
	}
	return 1
}

func (s *pgStore) ListVisibleSupportRequests(ctx context.Context, userID uuid.UUID, channel SupportChannel, cursor *SupportQueueCursor, limit int) ([]SupportRequest, error) {
	coolingThreshold := time.Now().UTC().Add(-generalSupportCoolingWindow)
	if channel == SupportChannelImmediate {
		coolingThreshold = time.Now().UTC().Add(-immediateSupportCoolingWindow)
	}

	var cursorBucket *int
	var cursorUrgency *int
	var cursorCreatedAt *time.Time
	var cursorID *uuid.UUID
	if cursor != nil {
		cursorBucket = &cursor.AttentionBucket
		cursorUrgency = &cursor.UrgencyRank
		cursorCreatedAt = &cursor.CreatedAt
		cursorID = &cursor.ID
	}

	rows, err := s.pool.Query(ctx,
		`WITH base_requests AS (
			SELECT
				sr.id,
				sr.requester_id,
				requester.username,
				requester.avatar_url,
				requester.city,
				sr.type,
				sr.message,
				sr.urgency,
				sr.status,
				sr.response_count,
				sr.created_at,
				sr.channel,
				EXISTS(
					SELECT 1
					FROM support_responses own_res
					WHERE own_res.support_request_id = sr.id
					  AND own_res.responder_id = $1
				) AS has_responded,
				sr.last_response_at,
				CASE sr.urgency
					WHEN 'right_now' THEN 0
					WHEN 'soon' THEN 1
					ELSE 2
				END AS urgency_rank
			FROM support_requests sr
			JOIN users requester ON requester.id = sr.requester_id
			WHERE sr.status = 'open'
			  AND COALESCE(sr.channel, 'community') = $2
			  AND sr.requester_id <> $1
		),
		visible_requests AS (
			SELECT
				id,
				requester_id,
				username,
				avatar_url,
				city,
				type,
				message,
				urgency,
				status,
				response_count,
				created_at,
				channel,
				has_responded,
				urgency_rank,
				CASE
					WHEN has_responded THEN 3
					WHEN response_count = 0 THEN 0
					WHEN response_count >= 3 THEN 2
					WHEN last_response_at IS NOT NULL AND last_response_at > $3 THEN 2
					ELSE 1
				END AS attention_bucket
			FROM base_requests
		)
		SELECT
			id,
			requester_id,
			username,
			avatar_url,
			city,
			type,
			message,
			urgency,
			status,
			response_count,
			created_at,
			channel,
			has_responded,
			false AS is_own_request,
			created_at AS sort_at,
			attention_bucket,
			urgency_rank
		FROM visible_requests
		WHERE (
			$4::int IS NULL
			OR attention_bucket > $4
			OR (attention_bucket = $4 AND urgency_rank > $5)
			OR (attention_bucket = $4 AND urgency_rank = $5 AND created_at > $6)
			OR (attention_bucket = $4 AND urgency_rank = $5 AND created_at = $6 AND id > $7)
		)
		ORDER BY attention_bucket ASC, urgency_rank ASC, created_at ASC, id ASC
		LIMIT $8`,
		userID, channel, coolingThreshold, cursorBucket, cursorUrgency, cursorCreatedAt, cursorID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []SupportRequest
	for rows.Next() {
		var req SupportRequest
		if err := rows.Scan(
			&req.ID, &req.RequesterID, &req.Username, &req.AvatarURL, &req.City,
			&req.Type, &req.Message, &req.Urgency, &req.Status, &req.ResponseCount,
			&req.CreatedAt, &req.Channel, &req.HasResponded, &req.IsOwnRequest, &req.SortAt,
			&req.AttentionBucket, &req.UrgencyRank,
		); err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}
	return requests, rows.Err()
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
	formattedMessage := formatSupportResponseMessage(responseType, message, scheduledFor)

	var res SupportResponse
	err := s.pool.QueryRow(ctx,
		`WITH inserted AS (
			INSERT INTO support_responses (
				support_request_id, responder_id, response_type, message, status, scheduled_for
			)
			VALUES ($1, $2, $3, $4, 'pending', $5)
			RETURNING id, support_request_id, responder_id, response_type, message, status, scheduled_for, created_at
		)
		SELECT
			i.id, i.support_request_id, i.responder_id,
			u.username, u.avatar_url, u.city,
			i.response_type, i.message, i.status, i.scheduled_for, i.created_at, NULL::uuid
		FROM inserted i
		JOIN users u ON u.id = i.responder_id`,
		requestID, userID, responseType, formattedMessage, scheduledFor,
	).Scan(
		&res.ID, &res.SupportRequestID, &res.ResponderID,
		&res.Username, &res.AvatarURL, &res.City,
		&res.ResponseType, &res.Message, &res.Status, &res.ScheduledFor, &res.CreatedAt, &res.ChatID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if _, err := s.pool.Exec(ctx,
		`UPDATE support_requests
		SET
			response_count = response_count + 1,
			last_response_at = NOW()
		WHERE id = $1`,
		requestID,
	); err != nil {
		return nil, err
	}

	return &CreateSupportResponseResult{Response: &res}, nil
}

func (s *pgStore) AcceptSupportResponse(ctx context.Context, requesterID, requestID, responseID uuid.UUID) (*SupportRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var requestType string
	var requestMessage *string
	var requestStatus string
	var acceptedResponseID *uuid.UUID
	var requesterUsername string
	var requesterAvatarURL *string
	err = tx.QueryRow(ctx,
		`SELECT
			sr.type,
			sr.message,
			sr.status,
			sr.accepted_response_id,
			u.username,
			u.avatar_url
		FROM support_requests sr
		JOIN users u ON u.id = sr.requester_id
		WHERE sr.id = $1
		  AND sr.requester_id = $2
		FOR UPDATE`,
		requestID, requesterID,
	).Scan(&requestType, &requestMessage, &requestStatus, &acceptedResponseID, &requesterUsername, &requesterAvatarURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if requestStatus != "open" || acceptedResponseID != nil {
		return nil, ErrConflict
	}

	var responderID uuid.UUID
	var responseType string
	var responseMessage *string
	err = tx.QueryRow(ctx,
		`SELECT responder_id, response_type, message
		FROM support_responses
		WHERE id = $1
		  AND support_request_id = $2
		  AND status = 'pending'
		FOR UPDATE`,
		responseID, requestID,
	).Scan(&responderID, &responseType, &responseMessage)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}

	var responderUsername string
	var responderAvatarURL *string
	if err := tx.QueryRow(ctx,
		`SELECT username, avatar_url FROM users WHERE id = $1`,
		responderID,
	).Scan(&responderUsername, &responderAvatarURL); err != nil {
		return nil, err
	}

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
		requestID, requesterID, responderID,
	).Scan(&chatID, &chatCreatedAt, &chatStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx,
			`INSERT INTO chats (is_group, name, status, support_request_id)
			VALUES (false, NULL, 'active', $1)
			RETURNING id, created_at, status`,
			requestID,
		).Scan(&chatID, &chatCreatedAt, &chatStatus)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO chat_members (chat_id, user_id, role) VALUES ($1, $2, 'requester')`,
			chatID, requesterID,
		); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO chat_members (chat_id, user_id, role) VALUES ($1, $2, 'addressee')`,
			chatID, responderID,
		); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else if chatStatus != "active" {
		if _, err := tx.Exec(ctx,
			`UPDATE chats SET status = 'active' WHERE id = $1`,
			chatID,
		); err != nil {
			return nil, err
		}
		chatStatus = "active"
	}

	if _, err := tx.Exec(ctx,
		`UPDATE support_responses
		SET status = CASE WHEN id = $2 THEN 'accepted' ELSE 'not_selected' END
		WHERE support_request_id = $1
		  AND status = 'pending'`,
		requestID, responseID,
	); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE support_requests
		SET
			status = 'active',
			accepted_response_id = $2,
			accepted_responder_id = $3,
			accepted_at = NOW(),
			chat_id = $4
		WHERE id = $1`,
		requestID, responseID, responderID, chatID,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return s.GetSupportRequest(ctx, requesterID, requestID)
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
			sres.response_type, sres.message, sres.status, sres.scheduled_for, sres.created_at,
			CASE WHEN sr.accepted_response_id = sres.id THEN sr.chat_id ELSE NULL::uuid END
		FROM support_responses sres
		JOIN support_requests sr ON sr.id = sres.support_request_id
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
		if err := rows.Scan(&res.ID, &res.SupportRequestID, &res.ResponderID, &res.Username, &res.AvatarURL, &res.City, &res.ResponseType, &res.Message, &res.Status, &res.ScheduledFor, &res.CreatedAt, &res.ChatID); err != nil {
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
			&req.CreatedAt, &req.HasResponded, &req.IsOwnRequest, &req.SortAt,
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
