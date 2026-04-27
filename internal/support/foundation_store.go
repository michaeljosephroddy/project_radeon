package support

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *pgStore) supportRoutingTablesReady(ctx context.Context) (bool, error) {
	var offersExists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name = 'support_offers'
		)`,
	).Scan(&offersExists); err != nil {
		return false, err
	}

	var sessionsExists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name = 'support_sessions'
		)`,
	).Scan(&sessionsExists); err != nil {
		return false, err
	}

	return offersExists && sessionsExists, nil
}

func (s *pgStore) GetSupportHome(ctx context.Context, userID uuid.UUID) (*SupportHomePayload, error) {
	ready, err := s.supportRoutingTablesReady(ctx)
	if err != nil {
		return nil, err
	}
	if !ready {
		profile, err := s.GetSupportProfile(ctx, userID)
		if err != nil {
			return nil, err
		}
		return &SupportHomePayload{
			ResponderProfile: &SupportResponderProfile{
				UserID:                  userID,
				IsAvailableForImmediate: profile.IsAvailableToSupport,
				IsAvailableForCommunity: profile.IsAvailableToSupport,
			},
		}, nil
	}

	profile, err := s.GetSupportResponderProfile(ctx, userID)
	if err != nil {
		return nil, err
	}

	payload := &SupportHomePayload{
		ResponderProfile: profile,
	}

	err = s.pool.QueryRow(ctx,
		`SELECT
			(SELECT COUNT(*) FROM support_offers so WHERE so.responder_id = $1 AND so.status = 'pending' AND so.expires_at > NOW()),
			(SELECT COUNT(*) FROM support_sessions ss WHERE (ss.requester_id = $1 OR ss.responder_id = $1) AND ss.status IN ('pending', 'active')),
			(SELECT COUNT(*) FROM support_requests sr WHERE sr.requester_id = $1 AND sr.channel = 'community' AND sr.status = 'open')`,
		userID,
	).Scan(&payload.PendingOfferCount, &payload.ActiveSessionCount, &payload.CommunityRequestCount)
	if err != nil {
		return nil, err
	}

	row := s.pool.QueryRow(ctx,
		`SELECT
			sr.id, sr.requester_id, u.username, u.avatar_url, u.city,
			sr.type, sr.message, sr.urgency, sr.status,
			sr.response_count, sr.created_at, sr.priority_visibility, sr.priority_expires_at,
			sr.channel, sr.routing_status, sr.desired_response_window, sr.privacy_level, sr.matched_session_id
		FROM support_requests sr
		JOIN users u ON u.id = sr.requester_id
		WHERE sr.requester_id = $1
		  AND sr.status IN ('open', 'matched')
		ORDER BY sr.created_at DESC
		LIMIT 1`,
		userID,
	)

	var request SupportRequest
	err = row.Scan(
		&request.ID, &request.RequesterID, &request.Username, &request.AvatarURL, &request.City,
		&request.Type, &request.Message, &request.Urgency, &request.Status,
		&request.ResponseCount, &request.CreatedAt, &request.PriorityVisibility, &request.PriorityExpiresAt,
		&request.Channel, &request.RoutingStatus, &request.DesiredResponseWindow, &request.PrivacyLevel, &request.MatchedSessionID,
	)
	if err == nil {
		request.IsOwnRequest = true
		request.SortAt = request.CreatedAt
		payload.ActiveRequest = &request
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	return payload, nil
}

func (s *pgStore) GetSupportResponderProfile(ctx context.Context, userID uuid.UUID) (*SupportResponderProfile, error) {
	ready, err := s.supportRoutingTablesReady(ctx)
	if err != nil {
		return nil, err
	}
	if !ready {
		profile, err := s.GetSupportProfile(ctx, userID)
		if err != nil {
			return nil, err
		}
		return &SupportResponderProfile{
			UserID:                  userID,
			IsAvailableForImmediate: profile.IsAvailableToSupport,
			IsAvailableForCommunity: profile.IsAvailableToSupport,
			SupportsChat:            true,
			SupportsCheckIns:        true,
			SupportsInPerson:        false,
			MaxConcurrentSessions:   2,
			Languages:               []string{},
		}, nil
	}

	var profile SupportResponderProfile
	err = s.pool.QueryRow(ctx,
		`SELECT
			u.id,
			COALESCE(srp.is_available_for_immediate, u.is_available_to_support),
			COALESCE(srp.is_available_for_community, true),
			COALESCE(srp.supports_chat, true),
			COALESCE(srp.supports_check_ins, true),
			COALESCE(srp.supports_in_person, false),
			COALESCE(srp.max_concurrent_sessions, 2),
			COALESCE(srp.languages, '{}'::text[]),
			COALESCE(spr.is_active, false),
			COALESCE(spr.available_now, false),
			COALESCE(spr.active_session_count, 0),
			COALESCE(srs.acceptance_rate, 0),
			COALESCE(srs.completion_rate, 0),
			COALESCE(srs.helpfulness_score, 0),
			COALESCE(srs.median_response_seconds, 0),
			COALESCE(srp.updated_at, u.support_updated_at, NOW()),
			spr.last_seen_at,
			srs.last_session_completed_at
		FROM users u
		LEFT JOIN support_responder_profiles srp ON srp.user_id = u.id
		LEFT JOIN support_responder_presence spr ON spr.user_id = u.id
		LEFT JOIN support_responder_stats srs ON srs.user_id = u.id
		WHERE u.id = $1`,
		userID,
	).Scan(
		&profile.UserID,
		&profile.IsAvailableForImmediate,
		&profile.IsAvailableForCommunity,
		&profile.SupportsChat,
		&profile.SupportsCheckIns,
		&profile.SupportsInPerson,
		&profile.MaxConcurrentSessions,
		&profile.Languages,
		&profile.IsActive,
		&profile.AvailableNow,
		&profile.ActiveSessionCount,
		&profile.AcceptanceRate,
		&profile.CompletionRate,
		&profile.HelpfulnessScore,
		&profile.MedianResponseSeconds,
		&profile.UpdatedAt,
		&profile.LastSeenAt,
		&profile.LastSessionCompletedAt,
	)
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (s *pgStore) UpdateSupportResponderProfile(ctx context.Context, userID uuid.UUID, input UpdateSupportResponderProfileInput) (*SupportResponderProfile, error) {
	ready, err := s.supportRoutingTablesReady(ctx)
	if err != nil {
		return nil, err
	}
	if !ready {
		profile, err := s.UpdateSupportProfile(ctx, userID, input.IsAvailableForImmediate || input.IsAvailableForCommunity)
		if err != nil {
			return nil, err
		}
		return &SupportResponderProfile{
			UserID:                  userID,
			IsAvailableForImmediate: profile.IsAvailableToSupport,
			IsAvailableForCommunity: profile.IsAvailableToSupport,
			SupportsChat:            true,
			SupportsCheckIns:        true,
			SupportsInPerson:        false,
			MaxConcurrentSessions:   2,
			Languages:               []string{},
		}, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`INSERT INTO support_responder_profiles (
			user_id,
			is_available_for_immediate,
			is_available_for_community,
			supports_chat,
			supports_check_ins,
			supports_in_person,
			max_concurrent_sessions,
			languages,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			is_available_for_immediate = EXCLUDED.is_available_for_immediate,
			is_available_for_community = EXCLUDED.is_available_for_community,
			supports_chat = EXCLUDED.supports_chat,
			supports_check_ins = EXCLUDED.supports_check_ins,
			supports_in_person = EXCLUDED.supports_in_person,
			max_concurrent_sessions = EXCLUDED.max_concurrent_sessions,
			languages = EXCLUDED.languages,
			updated_at = NOW()`,
		userID,
		input.IsAvailableForImmediate,
		input.IsAvailableForCommunity,
		input.SupportsChat,
		input.SupportsCheckIns,
		input.SupportsInPerson,
		input.MaxConcurrentSessions,
		input.Languages,
	)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO support_responder_presence (
			user_id,
			is_active,
			available_now,
			updated_at
		) VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			is_active = EXCLUDED.is_active,
			available_now = EXCLUDED.available_now,
			updated_at = NOW(),
			last_seen_at = CASE WHEN EXCLUDED.is_active THEN NOW() ELSE support_responder_presence.last_seen_at END`,
		userID,
		input.IsActive,
		input.AvailableNow,
	)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO support_responder_stats (user_id)
		VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING`,
		userID,
	)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx,
		`UPDATE users
		SET
			is_available_to_support = $2,
			support_updated_at = NOW()
		WHERE id = $1`,
		userID,
		input.IsAvailableForImmediate || input.IsAvailableForCommunity,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return s.GetSupportResponderProfile(ctx, userID)
}

func (s *pgStore) CreateImmediateSupportRequest(
	ctx context.Context,
	userID uuid.UUID,
	reqType string,
	message *string,
	urgency string,
	privacyLevel string,
	priorityVisibility bool,
	priorityExpiresAt *time.Time,
) (*SupportRequest, error) {
	return s.createRoutedSupportRequest(ctx, userID, SupportChannelImmediate, reqType, message, urgency, privacyLevel, priorityVisibility, priorityExpiresAt)
}

func (s *pgStore) CreateCommunitySupportRequest(
	ctx context.Context,
	userID uuid.UUID,
	reqType string,
	message *string,
	urgency string,
	privacyLevel string,
	priorityVisibility bool,
	priorityExpiresAt *time.Time,
) (*SupportRequest, error) {
	return s.createRoutedSupportRequest(ctx, userID, SupportChannelCommunity, reqType, message, urgency, privacyLevel, priorityVisibility, priorityExpiresAt)
}

func (s *pgStore) ListResponderQueue(ctx context.Context, responderID uuid.UUID, before *time.Time, limit int) ([]SupportOffer, error) {
	if err := s.expireAndRerouteResponderOffers(ctx, responderID); err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT
			so.id,
			so.support_request_id,
			sr.requester_id,
			u.username,
			u.avatar_url,
			sr.type,
			sr.message,
			sr.urgency,
			sr.channel,
			so.status,
			so.match_score,
			so.fit_summary,
			so.batch_number,
			so.offered_at,
			so.expires_at,
			so.responded_at
		FROM support_offers so
		JOIN support_requests sr ON sr.id = so.support_request_id
		JOIN users u ON u.id = sr.requester_id
		WHERE so.responder_id = $1
		  AND so.status = 'pending'
		  AND ($2::timestamptz IS NULL OR so.offered_at < $2)
		ORDER BY so.offered_at DESC, so.id DESC
		LIMIT $3`,
		responderID, before, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var offers []SupportOffer
	for rows.Next() {
		var offer SupportOffer
		if err := rows.Scan(
			&offer.ID,
			&offer.SupportRequestID,
			&offer.RequesterID,
			&offer.RequesterUsername,
			&offer.RequesterAvatarURL,
			&offer.RequestType,
			&offer.RequestMessage,
			&offer.RequestUrgency,
			&offer.RequestChannel,
			&offer.Status,
			&offer.MatchScore,
			&offer.FitSummary,
			&offer.BatchNumber,
			&offer.OfferedAt,
			&offer.ExpiresAt,
			&offer.RespondedAt,
		); err != nil {
			return nil, err
		}
		offer.SortAt = offer.OfferedAt
		offers = append(offers, offer)
	}
	return offers, rows.Err()
}

func (s *pgStore) ListSupportSessions(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportSession, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			ss.id,
			ss.support_request_id,
			ss.requester_id,
			requester.username,
			ss.responder_id,
			responder.username,
			ss.status,
			ss.outcome,
			ss.chat_id,
			ss.started_at,
			ss.completed_at,
			ss.cancelled_at,
			ss.created_at
		FROM support_sessions ss
		JOIN users requester ON requester.id = ss.requester_id
		JOIN users responder ON responder.id = ss.responder_id
		WHERE (ss.requester_id = $1 OR ss.responder_id = $1)
		  AND ($2::timestamptz IS NULL OR ss.created_at < $2)
		ORDER BY ss.created_at DESC, ss.id DESC
		LIMIT $3`,
		userID, before, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []SupportSession
	for rows.Next() {
		var session SupportSession
		if err := rows.Scan(
			&session.ID,
			&session.SupportRequestID,
			&session.RequesterID,
			&session.RequesterUsername,
			&session.ResponderID,
			&session.ResponderUsername,
			&session.Status,
			&session.Outcome,
			&session.ChatID,
			&session.StartedAt,
			&session.CompletedAt,
			&session.CancelledAt,
			&session.CreatedAt,
		); err != nil {
			return nil, err
		}
		session.SortAt = session.CreatedAt
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (s *pgStore) CloseSupportSession(ctx context.Context, userID, sessionID uuid.UUID, outcome string) (*SupportSession, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var session SupportSession
	var previousStatus SupportSessionStatus
	err = tx.QueryRow(ctx,
		`SELECT
			ss.id,
			ss.support_request_id,
			ss.requester_id,
			requester.username,
			ss.responder_id,
			responder.username,
			ss.status,
			ss.outcome,
			ss.chat_id,
			ss.started_at,
			ss.completed_at,
			ss.cancelled_at,
			ss.created_at
		FROM support_sessions ss
		JOIN users requester ON requester.id = ss.requester_id
		JOIN users responder ON responder.id = ss.responder_id
		WHERE ss.id = $1
		  AND (ss.requester_id = $2 OR ss.responder_id = $2)
		FOR UPDATE`,
		sessionID,
		userID,
	).Scan(
		&session.ID,
		&session.SupportRequestID,
		&session.RequesterID,
		&session.RequesterUsername,
		&session.ResponderID,
		&session.ResponderUsername,
		&previousStatus,
		&session.Outcome,
		&session.ChatID,
		&session.StartedAt,
		&session.CompletedAt,
		&session.CancelledAt,
		&session.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	newStatus := SupportSessionCompleted
	if outcome == "cancelled" {
		newStatus = SupportSessionCancelled
	}

	var outcomeValue *string
	if trimmed := outcome; trimmed != "" {
		outcomeValue = &trimmed
	}

	if _, err := tx.Exec(ctx,
		`UPDATE support_sessions
		SET
			status = $2,
			outcome = $3,
			completed_at = CASE WHEN $2 = 'completed' THEN NOW() ELSE completed_at END,
			cancelled_at = CASE WHEN $2 = 'cancelled' THEN NOW() ELSE cancelled_at END
		WHERE id = $1`,
		sessionID,
		newStatus,
		outcomeValue,
	); err != nil {
		return nil, err
	}

	if previousStatus == SupportSessionActive {
		if _, err := tx.Exec(ctx,
			`UPDATE support_responder_presence
			SET
				active_session_count = GREATEST(active_session_count - 1, 0),
				updated_at = NOW()
			WHERE user_id = $1`,
			session.ResponderID,
		); err != nil {
			return nil, err
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE support_requests
		SET
			status = 'closed',
			routing_status = 'closed',
			closed_at = NOW()
		WHERE id = $1`,
		session.SupportRequestID,
	); err != nil {
		return nil, err
	}

	_, _ = tx.Exec(ctx,
		`INSERT INTO support_events (support_request_id, support_session_id, actor_user_id, event_type, payload)
		VALUES ($1, $2, $3, $4, $5::jsonb)`,
		session.SupportRequestID,
		sessionID,
		userID,
		"support_session.closed",
		`{"outcome":"`+outcome+`"}`,
	)

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return s.getSupportSessionByID(ctx, sessionID)
}

func (s *pgStore) getSupportSessionByID(ctx context.Context, sessionID uuid.UUID) (*SupportSession, error) {
	var session SupportSession
	err := s.pool.QueryRow(ctx,
		`SELECT
			ss.id,
			ss.support_request_id,
			ss.requester_id,
			requester.username,
			ss.responder_id,
			responder.username,
			ss.status,
			ss.outcome,
			ss.chat_id,
			ss.started_at,
			ss.completed_at,
			ss.cancelled_at,
			ss.created_at
		FROM support_sessions ss
		JOIN users requester ON requester.id = ss.requester_id
		JOIN users responder ON responder.id = ss.responder_id
		WHERE ss.id = $1`,
		sessionID,
	).Scan(
		&session.ID,
		&session.SupportRequestID,
		&session.RequesterID,
		&session.RequesterUsername,
		&session.ResponderID,
		&session.ResponderUsername,
		&session.Status,
		&session.Outcome,
		&session.ChatID,
		&session.StartedAt,
		&session.CompletedAt,
		&session.CancelledAt,
		&session.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	session.SortAt = session.CreatedAt
	return &session, nil
}

func (s *pgStore) createRoutedSupportRequest(
	ctx context.Context,
	userID uuid.UUID,
	channel SupportChannel,
	reqType string,
	message *string,
	urgency string,
	privacyLevel string,
	priorityVisibility bool,
	priorityExpiresAt *time.Time,
) (*SupportRequest, error) {
	routingStatus := SupportRoutingNotApplicable
	if channel == SupportChannelImmediate {
		routingStatus = SupportRoutingPending
	}

	var req SupportRequest
	err := s.pool.QueryRow(ctx,
		`WITH inserted AS (
			INSERT INTO support_requests (
				requester_id,
				type,
				message,
				city,
				status,
				urgency,
				priority_visibility,
				priority_expires_at,
				channel,
				routing_status,
				desired_response_window,
				privacy_level
			)
			SELECT
				u.id,
				$2,
				$3,
				u.city,
				'open',
				$4,
				$5,
				$6,
				$7,
				$8,
				$4,
				$9
			FROM users u
			WHERE u.id = $1
			RETURNING
				id,
				requester_id,
				type,
				message,
				urgency,
				status,
				created_at,
				priority_visibility,
				priority_expires_at,
				channel,
				routing_status,
				desired_response_window,
				privacy_level,
				matched_session_id
		)
		SELECT
			i.id,
			i.requester_id,
			i.type,
			i.message,
			i.urgency,
			i.status,
			i.created_at,
			i.priority_visibility,
			i.priority_expires_at,
			i.channel,
			i.routing_status,
			i.desired_response_window,
			i.privacy_level,
			i.matched_session_id,
			u.username,
			u.avatar_url,
			u.city
		FROM inserted i
		JOIN users u ON u.id = i.requester_id`,
		userID,
		reqType,
		message,
		urgency,
		priorityVisibility,
		priorityExpiresAt,
		channel,
		routingStatus,
		privacyLevel,
	).Scan(
		&req.ID,
		&req.RequesterID,
		&req.Type,
		&req.Message,
		&req.Urgency,
		&req.Status,
		&req.CreatedAt,
		&req.PriorityVisibility,
		&req.PriorityExpiresAt,
		&req.Channel,
		&req.RoutingStatus,
		&req.DesiredResponseWindow,
		&req.PrivacyLevel,
		&req.MatchedSessionID,
		&req.Username,
		&req.AvatarURL,
		&req.City,
	)
	if err != nil {
		return nil, err
	}

	req.ResponseCount = 0
	req.HasResponded = false
	req.IsOwnRequest = true
	req.SortAt = req.CreatedAt

	_, _ = s.pool.Exec(ctx,
		`INSERT INTO support_events (support_request_id, actor_user_id, event_type, payload)
		VALUES ($1, $2, $3, $4::jsonb)`,
		req.ID,
		userID,
		"support_request.created",
		`{"channel":"`+string(channel)+`"}`,
	)

	return &req, nil
}
