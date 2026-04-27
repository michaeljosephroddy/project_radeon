package support

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *pgStore) RouteSupportRequest(ctx context.Context, requestID uuid.UUID) error {
	ready, err := s.supportRoutingTablesReady(ctx)
	if err != nil {
		return err
	}
	if !ready {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var requesterID uuid.UUID
	var requestType string
	var requestUrgency string
	var requestStatus string
	var requesterLat *float64
	var requesterLng *float64
	err = tx.QueryRow(ctx,
		`SELECT
			sr.requester_id,
			sr.type,
			sr.urgency,
			sr.status,
			COALESCE(u.current_lat, u.lat),
			COALESCE(u.current_lng, u.lng)
		FROM support_requests sr
		JOIN users u ON u.id = sr.requester_id
		WHERE sr.id = $1
		  AND sr.channel = 'immediate'
		FOR UPDATE`,
		requestID,
	).Scan(&requesterID, &requestType, &requestUrgency, &requestStatus, &requesterLat, &requesterLng)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if requestStatus != "open" {
		return nil
	}

	var batchNumber int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(batch_number), 0) + 1
		FROM support_offers
		WHERE support_request_id = $1`,
		requestID,
	).Scan(&batchNumber); err != nil {
		return err
	}

	rows, err := tx.Query(ctx,
		`SELECT
			u.id,
			COALESCE(spr.available_now, false) AS available_now,
			COALESCE(srp.supports_in_person, false) AS supports_in_person,
			COALESCE(spr.active_session_count, 0) AS active_session_count,
			COALESCE(srp.max_concurrent_sessions, 2) AS max_concurrent_sessions,
			(
				0.25 * CASE WHEN COALESCE(spr.available_now, false) THEN 1.0 ELSE 0.0 END
				+ 0.18 * COALESCE(srs.acceptance_rate, 0.0)
				+ 0.16 * COALESCE(srs.completion_rate, 0.0)
				+ 0.14 * CASE
					WHEN $2 = 'need_in_person_help' AND COALESCE(srp.supports_in_person, false) THEN 1.0
					WHEN $2 != 'need_in_person_help' AND COALESCE(srp.supports_chat, true) THEN 1.0
					ELSE 0.0
				END
				+ 0.10 * CASE
					WHEN $3::double precision IS NOT NULL
					     AND $4::double precision IS NOT NULL
					     AND COALESCE(u.current_lat, u.lat) IS NOT NULL
					     AND COALESCE(u.current_lng, u.lng) IS NOT NULL
					THEN EXP(-(
						2.0 * 6371.0 * ASIN(SQRT(
							POWER(SIN(RADIANS((COALESCE(u.current_lat, u.lat) - $3::double precision) / 2.0)), 2)
							+ COS(RADIANS($3::double precision)) * COS(RADIANS(COALESCE(u.current_lat, u.lat)))
							* POWER(SIN(RADIANS((COALESCE(u.current_lng, u.lng) - $4::double precision) / 2.0)), 2)
						))
					) / 300.0)
					ELSE 0.0
				END
				+ 0.05 * CASE WHEN COALESCE(spr.available_now, false) THEN 1.0 ELSE 0.5 END
				- 0.12 * LEAST(
					COALESCE(spr.active_session_count, 0)::double precision
					/ GREATEST(COALESCE(srp.max_concurrent_sessions, 2), 1)::double precision,
					1.0
				)
				- 0.08 * LEAST(COALESCE(srs.ignored_offer_count, 0)::double precision / 10.0, 1.0)
				- 0.04 * LEAST(COALESCE(srs.recent_decline_count, 0)::double precision / 10.0, 1.0)
			) AS total_score
		FROM users u
		JOIN support_responder_profiles srp ON srp.user_id = u.id
		JOIN support_responder_presence spr ON spr.user_id = u.id
		LEFT JOIN support_responder_stats srs ON srs.user_id = u.id
		WHERE u.id != $1
		  AND srp.is_available_for_immediate = true
		  AND (
			($2 = 'need_in_person_help' AND srp.supports_in_person = true)
			OR ($2 != 'need_in_person_help' AND srp.supports_chat = true)
		  )
		  AND COALESCE(spr.active_session_count, 0) < GREATEST(COALESCE(srp.max_concurrent_sessions, 2), 1)
		  AND NOT EXISTS (
			SELECT 1
			FROM support_offers existing
			WHERE existing.support_request_id = $5
			  AND existing.responder_id = u.id
		  )
		ORDER BY total_score DESC, u.id
		LIMIT 5`,
		requesterID,
		requestType,
		requesterLat,
		requesterLng,
		requestID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type candidate struct {
		responderID           uuid.UUID
		availableNow          bool
		supportsInPerson      bool
		activeSessionCount    int
		maxConcurrentSessions int
		score                 float64
	}

	candidates := make([]candidate, 0, 5)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(
			&item.responderID,
			&item.availableNow,
			&item.supportsInPerson,
			&item.activeSessionCount,
			&item.maxConcurrentSessions,
			&item.score,
		); err != nil {
			return err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(candidates) == 0 {
		_, err := tx.Exec(ctx,
			`UPDATE support_requests
			SET routing_status = 'fallback'
			WHERE id = $1`,
			requestID,
		)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	expiresAt := time.Now().Add(90 * time.Second)
	for _, item := range candidates {
		fitSummary := buildOfferFitSummary(requestType, item.availableNow, item.activeSessionCount, item.maxConcurrentSessions, requestUrgency)
		if _, err := tx.Exec(ctx,
			`INSERT INTO support_offers (
				support_request_id,
				responder_id,
				status,
				match_score,
				fit_summary,
				batch_number,
				offered_at,
				expires_at
			) VALUES ($1, $2, 'pending', $3, $4, $5, NOW(), $6)`,
			requestID,
			item.responderID,
			item.score,
			fitSummary,
			batchNumber,
			expiresAt,
		); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE support_requests
		SET routing_status = 'offered'
		WHERE id = $1`,
		requestID,
	); err != nil {
		return err
	}

	_, _ = tx.Exec(ctx,
		`INSERT INTO support_events (support_request_id, event_type, payload)
		VALUES ($1, $2, $3::jsonb)`,
		requestID,
		"support_request.routed",
		fmt.Sprintf(`{"batch_number":%d,"offer_count":%d}`, batchNumber, len(candidates)),
	)

	return tx.Commit(ctx)
}

func (s *pgStore) AcceptSupportOffer(ctx context.Context, responderID, offerID uuid.UUID) (*SupportSession, error) {
	ready, err := s.supportRoutingTablesReady(ctx)
	if err != nil {
		return nil, err
	}
	if !ready {
		return nil, ErrNotFound
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var requestID uuid.UUID
	var requesterID uuid.UUID
	var requesterUsername string
	var responderUsername string
	var offerStatus SupportOfferStatus
	var expiresAt time.Time
	err = tx.QueryRow(ctx,
		`SELECT
			so.support_request_id,
			sr.requester_id,
			requester.username,
			responder.username,
			so.status,
			so.expires_at
		FROM support_offers so
		JOIN support_requests sr ON sr.id = so.support_request_id
		JOIN users requester ON requester.id = sr.requester_id
		JOIN users responder ON responder.id = so.responder_id
		WHERE so.id = $1
		  AND so.responder_id = $2
		FOR UPDATE`,
		offerID,
		responderID,
	).Scan(
		&requestID,
		&requesterID,
		&requesterUsername,
		&responderUsername,
		&offerStatus,
		&expiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if offerStatus != SupportOfferPending || !expiresAt.After(time.Now()) {
		return nil, ErrConflict
	}

	// Lock the parent request so only one responder can turn a pending offer into a live session.
	var requestStatus string
	err = tx.QueryRow(ctx,
		`SELECT status
		FROM support_requests
		WHERE id = $1
		  AND channel = 'immediate'
		FOR UPDATE`,
		requestID,
	).Scan(&requestStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if requestStatus != "open" {
		return nil, ErrConflict
	}

	var hasSession bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1
			FROM support_sessions
			WHERE support_request_id = $1
		)`,
		requestID,
	).Scan(&hasSession); err != nil {
		return nil, err
	}
	if hasSession {
		return nil, ErrConflict
	}

	var sessionID uuid.UUID
	var createdAt time.Time
	startedAt := time.Now()
	if err := tx.QueryRow(ctx,
		`INSERT INTO support_sessions (
			support_request_id,
			requester_id,
			responder_id,
			status,
			started_at
		) VALUES ($1, $2, $3, 'active', $4)
		RETURNING id, created_at`,
		requestID,
		requesterID,
		responderID,
		startedAt,
	).Scan(&sessionID, &createdAt); err != nil {
		return nil, err
	}

	var chatID uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO chats (is_group, name, status, support_request_id)
		VALUES (false, NULL, 'active', $1)
		RETURNING id`,
		requestID,
	).Scan(&chatID); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO chat_members (chat_id, user_id, role) VALUES ($1, $2, 'requester')`,
		chatID,
		requesterID,
	); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO chat_members (chat_id, user_id, role) VALUES ($1, $2, 'addressee')`,
		chatID,
		responderID,
	); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE support_sessions
		SET chat_id = $2
		WHERE id = $1`,
		sessionID,
		chatID,
	); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE support_offers
		SET status = 'accepted', responded_at = NOW()
		WHERE id = $1`,
		offerID,
	); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE support_offers
		SET status = 'closed', closed_at = NOW()
		WHERE support_request_id = $1
		  AND id != $2
		  AND status = 'pending'`,
		requestID,
		offerID,
	); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE support_requests
		SET
			status = 'matched',
			routing_status = 'matched',
			matched_session_id = $2
		WHERE id = $1`,
		requestID,
		sessionID,
	); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE support_responder_presence
		SET
			active_session_count = active_session_count + 1,
			updated_at = NOW()
		WHERE user_id = $1`,
		responderID,
	); err != nil {
		return nil, err
	}

	_, _ = tx.Exec(ctx,
		`INSERT INTO support_events (support_request_id, support_offer_id, support_session_id, actor_user_id, event_type, payload)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
		requestID,
		offerID,
		sessionID,
		responderID,
		"support_offer.accepted",
		fmt.Sprintf(`{"chat_id":"%s"}`, chatID),
	)

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &SupportSession{
		ID:                sessionID,
		SupportRequestID:  requestID,
		RequesterID:       requesterID,
		RequesterUsername: requesterUsername,
		ResponderID:       responderID,
		ResponderUsername: responderUsername,
		Status:            SupportSessionActive,
		ChatID:            &chatID,
		StartedAt:         &startedAt,
		CreatedAt:         createdAt,
		SortAt:            createdAt,
	}, nil
}

func (s *pgStore) ConvertImmediateRequestToCommunity(ctx context.Context, requestID, userID uuid.UUID) (*SupportRequest, error) {
	ready, err := s.supportRoutingTablesReady(ctx)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var status string
	var routingStatus SupportRoutingStatus
	var matchedSessionID *uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT status, routing_status, matched_session_id
		FROM support_requests
		WHERE id = $1
		  AND requester_id = $2
		  AND channel = 'immediate'
		FOR UPDATE`,
		requestID,
		userID,
	).Scan(&status, &routingStatus, &matchedSessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "open" || routingStatus == SupportRoutingMatched || matchedSessionID != nil {
		return nil, ErrConflict
	}

	if ready {
		if _, err := tx.Exec(ctx,
			`UPDATE support_offers
			SET status = 'closed', closed_at = NOW()
			WHERE support_request_id = $1
			  AND status = 'pending'`,
			requestID,
		); err != nil {
			return nil, err
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE support_requests
		SET
			channel = 'community',
			routing_status = 'not_applicable',
			matched_session_id = NULL
		WHERE id = $1`,
		requestID,
	); err != nil {
		return nil, err
	}

	if ready {
		_, _ = tx.Exec(ctx,
			`INSERT INTO support_events (support_request_id, actor_user_id, event_type, payload)
			VALUES ($1, $2, $3, $4::jsonb)`,
			requestID,
			userID,
			"support_request.converted_to_community",
			`{"from":"immediate","to":"community"}`,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return s.GetSupportRequest(ctx, userID, requestID)
}

func (s *pgStore) DeclineSupportOffer(ctx context.Context, responderID, offerID uuid.UUID) error {
	ready, err := s.supportRoutingTablesReady(ctx)
	if err != nil {
		return err
	}
	if !ready {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var requestID uuid.UUID
	result, err := tx.Exec(ctx,
		`UPDATE support_offers
		SET status = 'declined', responded_at = NOW()
		WHERE id = $1
		  AND responder_id = $2
		  AND status = 'pending'`,
		offerID,
		responderID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	if err := tx.QueryRow(ctx,
		`SELECT support_request_id FROM support_offers WHERE id = $1`,
		offerID,
	).Scan(&requestID); err != nil {
		return err
	}

	var pendingCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM support_offers WHERE support_request_id = $1 AND status = 'pending' AND expires_at > NOW()`,
		requestID,
	).Scan(&pendingCount); err != nil {
		return err
	}
	_, _ = tx.Exec(ctx,
		`INSERT INTO support_events (support_request_id, support_offer_id, actor_user_id, event_type)
		VALUES ($1, $2, $3, $4)`,
		requestID,
		offerID,
		responderID,
		"support_offer.declined",
	)

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	if pendingCount == 0 {
		return s.RouteSupportRequest(ctx, requestID)
	}

	return nil
}

func buildOfferFitSummary(requestType string, availableNow bool, activeSessionCount, maxConcurrentSessions int, requestUrgency string) string {
	switch {
	case requestType == "need_in_person_help" && availableNow:
		return "Available now and can meet in person"
	case requestType == "need_in_person_help":
		return "Can meet in person"
	case availableNow && requestUrgency == "right_now":
		return "Available now for urgent support"
	case availableNow:
		return "Available now"
	case activeSessionCount == 0:
		return "Low current support load"
	case activeSessionCount < maxConcurrentSessions:
		return "Has room for another support session"
	default:
		return "Eligible support match"
	}
}

func (s *pgStore) expireAndRerouteResponderOffers(ctx context.Context, responderID uuid.UUID) error {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT support_request_id
		FROM support_offers
		WHERE responder_id = $1
		  AND status = 'pending'
		  AND expires_at <= NOW()`,
		responderID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	requestIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var requestID uuid.UUID
		if err := rows.Scan(&requestID); err != nil {
			return err
		}
		requestIDs = append(requestIDs, requestID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(requestIDs) == 0 {
		return nil
	}

	if _, err := s.pool.Exec(ctx,
		`UPDATE support_offers
		SET
			status = 'expired',
			responded_at = NOW(),
			closed_at = NOW()
		WHERE responder_id = $1
		  AND status = 'pending'
		  AND expires_at <= NOW()`,
		responderID,
	); err != nil {
		return err
	}

	for _, requestID := range requestIDs {
		if err := s.RouteSupportRequest(ctx, requestID); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}

	return nil
}

func (s *pgStore) SweepExpiredSupportOffers(ctx context.Context) error {
	ready, err := s.supportRoutingTablesReady(ctx)
	if err != nil {
		return err
	}
	if !ready {
		return nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT support_request_id
		FROM support_offers
		WHERE status = 'pending'
		  AND expires_at <= NOW()`,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	requestIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var requestID uuid.UUID
		if err := rows.Scan(&requestID); err != nil {
			return err
		}
		requestIDs = append(requestIDs, requestID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(requestIDs) == 0 {
		return nil
	}

	if _, err := s.pool.Exec(ctx,
		`UPDATE support_offers
		SET
			status = 'expired',
			responded_at = NOW(),
			closed_at = NOW()
		WHERE status = 'pending'
		  AND expires_at <= NOW()`,
	); err != nil {
		return err
	}

	for _, requestID := range requestIDs {
		if err := s.RouteSupportRequest(ctx, requestID); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return nil
}
