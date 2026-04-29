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

func setSupportRequestLocation(req *SupportRequest, visibility string, city, region, country *string, lat, lng *float64) {
	if req.Topics == nil {
		req.Topics = []string{}
	}
	if visibility == "" {
		visibility = "hidden"
	}
	if visibility == "hidden" {
		req.Location = nil
		return
	}
	req.Location = &SupportLocation{
		City:           city,
		Region:         region,
		Country:        country,
		ApproximateLat: lat,
		ApproximateLng: lng,
		Visibility:     visibility,
	}
}

func supportFeedScore(urgency string, isPriority bool, replyCount int, offerCount int, createdAt time.Time, servedAt time.Time) float64 {
	score := 100.0
	switch urgency {
	case "high":
		score = 300
	case "medium":
		score = 200
	}
	if isPriority {
		score += 40
	}
	if replyCount == 0 && offerCount == 0 {
		score += 80
	} else if offerCount == 0 {
		score += 50
	}
	ageHours := servedAt.Sub(createdAt).Hours()
	if ageHours < 0 {
		ageHours = 0
	}
	score += 50 / (1 + ageHours/24)
	return score
}

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

func (s *pgStore) CountHighUrgencySupportRequestsSince(ctx context.Context, userID uuid.UUID, since time.Time) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*)
		FROM support_requests
		WHERE requester_id = $1
		  AND urgency = 'high'
		  AND created_at >= $2`,
		userID, since,
	).Scan(&count)
	return count, err
}

func (s *pgStore) GetSupportRequest(ctx context.Context, viewerID, requestID uuid.UUID) (*SupportRequest, error) {
	var req SupportRequest
	var locationVisibility string
	var locationCity *string
	var locationRegion *string
	var locationCountry *string
	var locationApproxLat *float64
	var locationApproxLng *float64
	err := s.pool.QueryRow(ctx,
		`SELECT
			sr.id,
			sr.requester_id,
			requester.username,
			requester.avatar_url,
			requester.city,
			sr.support_type,
			COALESCE(sr.topics, '{}'::text[]),
			sr.preferred_gender,
			sr.location_visibility,
			sr.location_city,
			sr.location_region,
			sr.location_country,
			sr.location_approx_lat,
			sr.location_approx_lng,
			sr.message,
			sr.urgency,
			sr.status,
			COALESCE(sr.reply_count, 0),
			sr.response_count,
			COALESCE(sr.view_count, 0),
			(COALESCE(sr.is_priority, false) AND (sr.priority_expires_at IS NULL OR sr.priority_expires_at > NOW())) AS is_priority,
			sr.created_at,
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
			EXISTS(
				SELECT 1 FROM support_replies own_reply
				WHERE own_reply.support_request_id = sr.id
					AND own_reply.author_id = $2
			) AS has_replied,
			sr.requester_id = $2 AS is_own_request
		FROM support_requests sr
		JOIN users requester ON requester.id = sr.requester_id
		LEFT JOIN users responder ON responder.id = sr.accepted_responder_id
		WHERE sr.id = $1`,
		requestID, viewerID,
	).Scan(
		&req.ID, &req.RequesterID, &req.Username, &req.AvatarURL, &req.City,
		&req.SupportType, &req.Topics, &req.PreferredGender,
		&locationVisibility, &locationCity, &locationRegion, &locationCountry, &locationApproxLat, &locationApproxLng,
		&req.Message, &req.Urgency, &req.Status, &req.ReplyCount, &req.ResponseCount, &req.ViewCount, &req.IsPriority,
		&req.CreatedAt, &req.PrivacyLevel,
		&req.AcceptedResponseID, &req.AcceptedResponderID, &req.AcceptedAt, &req.ClosedAt,
		&req.ResponderID, &req.ResponderUsername, &req.ResponderAvatarURL, &req.ChatID,
		&req.HasResponded, &req.HasReplied, &req.IsOwnRequest,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	req.OfferCount = req.ResponseCount
	req.HasOffered = req.HasResponded
	setSupportRequestLocation(&req, locationVisibility, locationCity, locationRegion, locationCountry, locationApproxLat, locationApproxLng)
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
		if _, err := tx.Exec(ctx,
			`WITH updated_chat AS (
				UPDATE chats
				SET
					next_message_seq = next_message_seq + 1,
					last_message_seq = next_message_seq,
					last_message_body = $3,
					last_message_at = NOW(),
					last_message_sender_id = $2,
					status = 'closed'
				WHERE id = $1
				RETURNING next_message_seq - 1 AS assigned_seq, last_message_at
			),
			inserted AS (
				INSERT INTO messages (chat_id, sender_id, kind, body, chat_seq, sent_at)
				SELECT $1, $2, 'system', $3, assigned_seq, last_message_at
				FROM updated_chat
				RETURNING id, chat_id
			)
			UPDATE chats ch
			SET last_message_id = inserted.id
			FROM inserted
			WHERE ch.id = inserted.chat_id`,
			chatID,
			userID,
			supportChatClosedMessage,
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
			sr.support_type, COALESCE(sr.topics, '{}'::text[]), sr.preferred_gender,
			sr.location_visibility, sr.location_city, sr.location_region, sr.location_country, sr.location_approx_lat, sr.location_approx_lng,
			sr.message, sr.urgency, sr.status,
			COALESCE(sr.reply_count, 0), sr.response_count, COALESCE(sr.view_count, 0),
			(COALESCE(sr.is_priority, false) AND (sr.priority_expires_at IS NULL OR sr.priority_expires_at > NOW())) AS is_priority,
			sr.created_at,
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
		var locationVisibility string
		var locationCity *string
		var locationRegion *string
		var locationCountry *string
		var locationApproxLat *float64
		var locationApproxLng *float64
		if err := rows.Scan(
			&req.ID, &req.RequesterID, &req.Username, &req.AvatarURL, &req.City,
			&req.SupportType, &req.Topics, &req.PreferredGender,
			&locationVisibility, &locationCity, &locationRegion, &locationCountry, &locationApproxLat, &locationApproxLng,
			&req.Message, &req.Urgency, &req.Status, &req.ReplyCount, &req.ResponseCount, &req.ViewCount, &req.IsPriority,
			&req.CreatedAt, &req.PrivacyLevel,
			&req.AcceptedResponseID, &req.AcceptedResponderID, &req.AcceptedAt, &req.ClosedAt,
			&req.ResponderID, &req.ResponderUsername, &req.ResponderAvatarURL, &req.ChatID,
			&req.HasResponded, &req.IsOwnRequest, &req.SortAt,
		); err != nil {
			return nil, err
		}
		if req.SortAt.IsZero() {
			req.SortAt = req.CreatedAt
		}
		req.OfferCount = req.ResponseCount
		req.HasOffered = req.HasResponded
		setSupportRequestLocation(&req, locationVisibility, locationCity, locationRegion, locationCountry, locationApproxLat, locationApproxLng)
		requests = append(requests, req)
	}
	return requests, rows.Err()
}

func supportUrgencyRank(urgency string) int {
	switch urgency {
	case "high":
		return 0
	case "medium":
		return 1
	default:
		return 2
	}
}

func (s *pgStore) ListVisibleSupportRequests(ctx context.Context, userID uuid.UUID, filter SupportRequestFilter, cursor *SupportFeedCursor, limit int) ([]SupportRequest, error) {
	servedAt := time.Now().UTC()
	var cursorScore *float64
	var cursorCreatedAt *time.Time
	var cursorID *uuid.UUID
	if cursor != nil {
		cursorScore = &cursor.Score
		cursorCreatedAt = &cursor.CreatedAt
		cursorID = &cursor.ID
		if !cursor.ServedAt.IsZero() {
			servedAt = cursor.ServedAt
		}
	}

	rows, err := s.pool.Query(ctx,
		`WITH visible_requests AS (
			SELECT
				sr.id,
				sr.requester_id,
				requester.username,
				requester.avatar_url,
				requester.city,
				sr.support_type,
				COALESCE(sr.topics, '{}'::text[]) AS topics,
				sr.preferred_gender,
				sr.location_visibility,
				sr.location_city,
				sr.location_region,
				sr.location_country,
				sr.location_approx_lat,
				sr.location_approx_lng,
				sr.message,
				sr.urgency,
				sr.status,
				COALESCE(sr.reply_count, 0) AS reply_count,
				sr.response_count,
				COALESCE(sr.view_count, 0) AS view_count,
				(COALESCE(sr.is_priority, false) AND (sr.priority_expires_at IS NULL OR sr.priority_expires_at > $3)) AS is_priority,
				sr.created_at,
				EXISTS(
					SELECT 1
					FROM support_responses own_res
					WHERE own_res.support_request_id = sr.id
					  AND own_res.responder_id = $1
				) AS has_offered,
				EXISTS(
					SELECT 1
					FROM support_replies own_reply
					WHERE own_reply.support_request_id = sr.id
					  AND own_reply.author_id = $1
				) AS has_replied
			FROM support_requests sr
			JOIN users requester ON requester.id = sr.requester_id
			WHERE sr.status = 'open'
			  AND sr.requester_id <> $1
			  AND (
			  	$2 = 'all'
			  	OR ($2 = 'urgent' AND sr.urgency = 'high')
			  	OR ($2 = 'unanswered' AND COALESCE(sr.reply_count, 0) = 0 AND sr.response_count = 0)
			  )
		),
		scored_requests AS (
			SELECT
				*,
				(
					CASE urgency
						WHEN 'high' THEN 300.0
						WHEN 'medium' THEN 200.0
						ELSE 100.0
					END
					+ CASE WHEN is_priority THEN 40.0 ELSE 0.0 END
					+ CASE
						WHEN reply_count = 0 AND response_count = 0 THEN 80.0
						WHEN response_count = 0 THEN 50.0
						ELSE 0.0
					END
					+ (50.0 / (1.0 + (GREATEST(EXTRACT(EPOCH FROM ($3 - created_at)) / 3600.0, 0.0) / 24.0)))
					- CASE WHEN has_offered THEN 100.0 ELSE 0.0 END
				) AS feed_score
			FROM visible_requests
		)
		SELECT
			id,
			requester_id,
			username,
			avatar_url,
			city,
			support_type,
			topics,
			preferred_gender,
			location_visibility,
			location_city,
			location_region,
			location_country,
			location_approx_lat,
			location_approx_lng,
			message,
			urgency,
			status,
			reply_count,
			response_count,
			view_count,
			is_priority,
			created_at,
			has_offered,
			has_replied,
			false AS is_own_request,
			created_at AS sort_at,
			feed_score
		FROM scored_requests
		WHERE (
			$4::double precision IS NULL
			OR feed_score < $4
			OR (feed_score = $4 AND created_at < $5)
			OR (feed_score = $4 AND created_at = $5 AND id < $6)
		)
		ORDER BY feed_score DESC, created_at DESC, id DESC
		LIMIT $7`,
		userID, filter, servedAt, cursorScore, cursorCreatedAt, cursorID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []SupportRequest
	for rows.Next() {
		var req SupportRequest
		var locationVisibility string
		var locationCity *string
		var locationRegion *string
		var locationCountry *string
		var locationApproxLat *float64
		var locationApproxLng *float64
		if err := rows.Scan(
			&req.ID, &req.RequesterID, &req.Username, &req.AvatarURL, &req.City,
			&req.SupportType, &req.Topics, &req.PreferredGender,
			&locationVisibility, &locationCity, &locationRegion, &locationCountry, &locationApproxLat, &locationApproxLng,
			&req.Message, &req.Urgency, &req.Status, &req.ReplyCount, &req.ResponseCount, &req.ViewCount, &req.IsPriority,
			&req.CreatedAt, &req.HasResponded, &req.HasReplied, &req.IsOwnRequest, &req.SortAt,
			&req.FeedScore,
		); err != nil {
			return nil, err
		}
		req.OfferCount = req.ResponseCount
		req.HasOffered = req.HasResponded
		setSupportRequestLocation(&req, locationVisibility, locationCity, locationRegion, locationCountry, locationApproxLat, locationApproxLng)
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

func (s *pgStore) CreateSupportOffer(ctx context.Context, requestID, userID uuid.UUID, offerType string, message *string, scheduledFor *time.Time) (*CreateSupportOfferResult, error) {
	formattedMessage := formatSupportOfferMessage(offerType, message, scheduledFor)

	var res SupportOffer
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
		requestID, userID, offerType, formattedMessage, scheduledFor,
	).Scan(
		&res.ID, &res.SupportRequestID, &res.ResponderID,
		&res.Username, &res.AvatarURL, &res.City,
		&res.OfferType, &res.Message, &res.Status, &res.ScheduledFor, &res.CreatedAt, &res.ChatID,
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

	return &CreateSupportOfferResult{Offer: &res}, nil
}

func (s *pgStore) AcceptSupportOffer(ctx context.Context, requesterID, requestID, responseID uuid.UUID) (*SupportRequest, error) {
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
				sr.support_type,
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

func (s *pgStore) ListSupportOffers(ctx context.Context, requestID uuid.UUID, limit, offset int) ([]SupportOffer, error) {
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

	var offers []SupportOffer
	for rows.Next() {
		var offer SupportOffer
		if err := rows.Scan(&offer.ID, &offer.SupportRequestID, &offer.ResponderID, &offer.Username, &offer.AvatarURL, &offer.City, &offer.OfferType, &offer.Message, &offer.Status, &offer.ScheduledFor, &offer.CreatedAt, &offer.ChatID); err != nil {
			return nil, err
		}
		offers = append(offers, offer)
	}
	return offers, rows.Err()
}

func (s *pgStore) DeclineSupportOffer(ctx context.Context, requesterID, requestID, offerID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE support_responses rsp
		SET status = 'not_selected'
		FROM support_requests sr
		WHERE rsp.id = $1
		  AND rsp.support_request_id = $2
		  AND sr.id = rsp.support_request_id
		  AND sr.requester_id = $3
		  AND sr.status = 'open'
		  AND rsp.status = 'pending'`,
		offerID, requestID, requesterID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgStore) CancelSupportOffer(ctx context.Context, responderID, requestID, offerID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE support_responses
		SET status = 'not_selected'
		WHERE id = $1
		  AND support_request_id = $2
		  AND responder_id = $3
		  AND status = 'pending'`,
		offerID, requestID, responderID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgStore) CreateSupportReply(ctx context.Context, requestID, authorID uuid.UUID, body string) (*SupportReply, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var reply SupportReply
	err = tx.QueryRow(ctx,
		`WITH inserted AS (
			INSERT INTO support_replies (support_request_id, author_id, body)
			VALUES ($1, $2, $3)
			RETURNING id, support_request_id, author_id, body, created_at
		),
		counter AS (
			UPDATE support_requests
			SET reply_count = reply_count + 1
			WHERE id = $1
			RETURNING id
		)
		SELECT
			i.id,
			i.support_request_id,
			i.author_id,
			u.username,
			u.avatar_url,
			i.body,
			i.created_at
		FROM inserted i
		JOIN users u ON u.id = i.author_id`,
		requestID, authorID, body,
	).Scan(&reply.ID, &reply.SupportRequestID, &reply.AuthorID, &reply.Username, &reply.AvatarURL, &reply.Body, &reply.CreatedAt)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &reply, nil
}

func (s *pgStore) ListSupportReplies(ctx context.Context, requestID uuid.UUID, cursor *SupportReplyCursor, limit int) ([]SupportReply, error) {
	var cursorCreatedAt *time.Time
	var cursorID *uuid.UUID
	if cursor != nil {
		cursorCreatedAt = &cursor.CreatedAt
		cursorID = &cursor.ID
	}

	rows, err := s.pool.Query(ctx,
		`SELECT
			sr.id,
			sr.support_request_id,
			sr.author_id,
			u.username,
			u.avatar_url,
			sr.body,
			sr.created_at
		FROM support_replies sr
		JOIN users u ON u.id = sr.author_id
		WHERE sr.support_request_id = $1
		  AND (
		  	$2::timestamptz IS NULL
		  	OR sr.created_at > $2
		  	OR (sr.created_at = $2 AND sr.id > $3)
		  )
		ORDER BY sr.created_at ASC, sr.id ASC
		LIMIT $4`,
		requestID, cursorCreatedAt, cursorID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var replies []SupportReply
	for rows.Next() {
		var reply SupportReply
		if err := rows.Scan(&reply.ID, &reply.SupportRequestID, &reply.AuthorID, &reply.Username, &reply.AvatarURL, &reply.Body, &reply.CreatedAt); err != nil {
			return nil, err
		}
		replies = append(replies, reply)
	}
	return replies, rows.Err()
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
