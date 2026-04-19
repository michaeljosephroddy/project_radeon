package support

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/pagination"
	"github.com/project_radeon/api/pkg/response"
)

type Handler struct {
	db *pgxpool.Pool
}

var validSupportTypes = map[string]bool{
	"need_to_talk":       true,
	"need_distraction":   true,
	"need_encouragement": true,
	"need_company":       true,
}

var validSupportAudiences = map[string]bool{
	"friends":   true,
	"city":      true,
	"community": true,
}

var validSupportResponseTypes = map[string]bool{
	"can_chat":       true,
	"check_in_later": true,
	"nearby":         true,
}

// NewHandler builds a support handler backed by the shared database pool.
func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{db: db}
}

type SupportProfile struct {
	IsAvailableToSupport bool       `json:"is_available_to_support"`
	SupportModes         []string   `json:"support_modes"`
	SupportUpdatedAt     *time.Time `json:"support_updated_at,omitempty"`
}

type SupportRequest struct {
	ID            uuid.UUID `json:"id"`
	RequesterID   uuid.UUID `json:"requester_id"`
	Username      string    `json:"username"`
	AvatarURL     *string   `json:"avatar_url"`
	City          *string   `json:"city"`
	Type          string    `json:"type"`
	Message       *string   `json:"message"`
	Audience      string    `json:"audience"`
	Status        string    `json:"status"`
	ResponseCount int       `json:"response_count"`
	ExpiresAt     time.Time `json:"expires_at"`
	CreatedAt     time.Time `json:"created_at"`
	HasResponded  bool      `json:"has_responded"`
	IsOwnRequest  bool      `json:"is_own_request"`
}

type SupportResponse struct {
	ID               uuid.UUID `json:"id"`
	SupportRequestID uuid.UUID `json:"support_request_id"`
	ResponderID      uuid.UUID `json:"responder_id"`
	Username         string    `json:"username"`
	AvatarURL        *string   `json:"avatar_url"`
	City             *string   `json:"city"`
	ResponseType     string    `json:"response_type"`
	Message          *string   `json:"message"`
	CreatedAt        time.Time `json:"created_at"`
}

type SupportRequestsPage struct {
	Items                   []SupportRequest `json:"items"`
	Limit                   int              `json:"limit"`
	HasMore                 bool             `json:"has_more"`
	NextCursor              *string          `json:"next_cursor,omitempty"`
	OpenRequestCount        *int             `json:"open_request_count,omitempty"`
	AvailableToSupportCount *int             `json:"available_to_support_count,omitempty"`
}

// GetMySupportProfile returns the caller's support availability settings.
func (h *Handler) GetMySupportProfile(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var profile SupportProfile
	err := h.db.QueryRow(r.Context(),
		`SELECT is_available_to_support, support_modes, support_updated_at
		FROM users
		WHERE id = $1`,
		userID,
	).Scan(&profile.IsAvailableToSupport, &profile.SupportModes, &profile.SupportUpdatedAt)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support profile")
		return
	}

	response.Success(w, http.StatusOK, profile)
}

// UpdateMySupportProfile saves the caller's support availability settings.
func (h *Handler) UpdateMySupportProfile(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		IsAvailableToSupport bool     `json:"is_available_to_support"`
		SupportModes         []string `json:"support_modes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	supportModes := input.SupportModes
	if supportModes == nil {
		supportModes = []string{}
	}

	var profile SupportProfile
	err := h.db.QueryRow(r.Context(),
		`UPDATE users
		SET
			is_available_to_support = $2,
			support_modes = $3,
			support_updated_at = NOW()
		WHERE id = $1
		RETURNING is_available_to_support, support_modes, support_updated_at`,
		userID, input.IsAvailableToSupport, supportModes,
	).Scan(&profile.IsAvailableToSupport, &profile.SupportModes, &profile.SupportUpdatedAt)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update support profile")
		return
	}

	response.Success(w, http.StatusOK, profile)
}

// CreateSupportRequest creates a time-bound support request for the authenticated user.
func (h *Handler) CreateSupportRequest(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Type      string  `json:"type"`
		Message   *string `json:"message"`
		Audience  string  `json:"audience"`
		ExpiresAt string  `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	input.Type = strings.TrimSpace(input.Type)
	input.Audience = strings.TrimSpace(input.Audience)
	if input.Message != nil {
		msg := strings.TrimSpace(*input.Message)
		input.Message = &msg
	}

	errs := map[string]string{}
	if input.Type == "" {
		errs["type"] = "required"
	} else if !validSupportTypes[input.Type] {
		errs["type"] = "invalid"
	}
	if input.Audience == "" {
		errs["audience"] = "required"
	} else if !validSupportAudiences[input.Audience] {
		errs["audience"] = "invalid"
	}
	if input.ExpiresAt == "" {
		errs["expires_at"] = "required"
	}
	if len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}

	expiresAt, err := time.Parse(time.RFC3339, input.ExpiresAt)
	if err != nil || !expiresAt.After(time.Now()) {
		response.Error(w, http.StatusBadRequest, "expires_at must be a future RFC3339 timestamp")
		return
	}

	var openCount int
	if err := h.db.QueryRow(r.Context(),
		`SELECT COUNT(*)
		FROM support_requests
		WHERE requester_id = $1
			AND status = 'open'
			AND expires_at > NOW()`,
		userID,
	).Scan(&openCount); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not validate support request")
		return
	}
	if openCount > 0 {
		response.Error(w, http.StatusConflict, "you already have an open support request")
		return
	}

	req, err := h.createSupportRequest(r.Context(), userID, input.Type, input.Message, input.Audience, expiresAt)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create support request")
		return
	}

	response.Success(w, http.StatusCreated, req)
}

// ListMySupportRequests returns support requests created by the authenticated user.
// Paginate with ?before=<next_cursor> from the previous response.
func (h *Handler) ListMySupportRequests(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 20, 50)

	requests, err := h.listSupportRequests(r.Context(),
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
			false AS has_responded,
			true AS is_own_request
		FROM support_requests sr
		JOIN users u ON u.id = sr.requester_id
		WHERE sr.requester_id = $1
			AND ($2::timestamptz IS NULL OR sr.created_at < $2)
		ORDER BY sr.created_at DESC
		LIMIT $3`,
		userID, params.Before, params.Limit+1,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support requests")
		return
	}

	page := pagination.CursorSlice(requests, params.Limit, func(sr SupportRequest) time.Time { return sr.CreatedAt })
	response.Success(w, http.StatusOK, SupportRequestsPage{
		Items:      page.Items,
		Limit:      page.Limit,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
	})
}

// ListSupportRequests returns the visible support page plus the lightweight tab
// summary counts the mobile client shows above the request cards.
// Paginate with ?before=<next_cursor> from the previous response.
func (h *Handler) ListSupportRequests(w http.ResponseWriter, r *http.Request) {
	params := pagination.ParseCursor(r, 20, 50)
	requests, err := h.ListVisibleSupportRequests(r, params)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support requests")
		return
	}

	openCount, availableCount, err := h.fetchSupportSummary(r.Context(), middleware.CurrentUserID(r))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support requests")
		return
	}

	page := pagination.CursorSlice(requests, params.Limit, func(sr SupportRequest) time.Time { return sr.CreatedAt })
	response.Success(w, http.StatusOK, SupportRequestsPage{
		Items:                   page.Items,
		Limit:                   page.Limit,
		HasMore:                 page.HasMore,
		NextCursor:              page.NextCursor,
		OpenRequestCount:        &openCount,
		AvailableToSupportCount: &availableCount,
	})
}

// GetSupportRequest returns one support request with viewer-specific metadata.
func (h *Handler) GetSupportRequest(w http.ResponseWriter, r *http.Request) {
	viewerID := middleware.CurrentUserID(r)
	requestID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support request id")
		return
	}

	req, err := h.fetchSupportRequest(r.Context(), viewerID, requestID)
	if err != nil {
		response.Error(w, http.StatusNotFound, "support request not found")
		return
	}

	response.Success(w, http.StatusOK, req)
}

// UpdateSupportRequest lets the requester close their own support request.
func (h *Handler) UpdateSupportRequest(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	requestID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support request id")
		return
	}

	var input struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(input.Status) != "closed" {
		response.Error(w, http.StatusBadRequest, "unsupported support request update")
		return
	}

	result, err := h.db.Exec(r.Context(),
		`UPDATE support_requests
		SET
			status = 'closed',
			closed_at = NOW()
		WHERE id = $1
			AND requester_id = $2`,
		requestID, userID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update support request")
		return
	}
	if result.RowsAffected() == 0 {
		response.Error(w, http.StatusNotFound, "support request not found")
		return
	}

	req, err := h.fetchSupportRequest(r.Context(), userID, requestID)
	if err != nil {
		response.Error(w, http.StatusNotFound, "support request not found")
		return
	}

	response.Success(w, http.StatusOK, req)
}

// CreateSupportResponse records one user's response to an open support request.
func (h *Handler) CreateSupportResponse(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	requestID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support request id")
		return
	}

	var input struct {
		ResponseType string  `json:"response_type"`
		Message      *string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.ResponseType = strings.TrimSpace(input.ResponseType)
	if input.Message != nil {
		msg := strings.TrimSpace(*input.Message)
		input.Message = &msg
	}
	if input.ResponseType == "" {
		response.ValidationError(w, map[string]string{"response_type": "required"})
		return
	}
	if !validSupportResponseTypes[input.ResponseType] {
		response.ValidationError(w, map[string]string{"response_type": "invalid"})
		return
	}

	var requesterID uuid.UUID
	var status string
	var expiresAt time.Time
	err = h.db.QueryRow(r.Context(),
		`SELECT requester_id, status, expires_at
		FROM support_requests
		WHERE id = $1`,
		requestID,
	).Scan(&requesterID, &status, &expiresAt)
	if err != nil {
		response.Error(w, http.StatusNotFound, "support request not found")
		return
	}
	if requesterID == userID {
		response.Error(w, http.StatusBadRequest, "cannot respond to your own request")
		return
	}
	if status != "open" || !expiresAt.After(time.Now()) {
		response.Error(w, http.StatusConflict, "support request is no longer open")
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create support response")
		return
	}
	defer tx.Rollback(r.Context())

	var res SupportResponse
	err = tx.QueryRow(r.Context(),
		`WITH inserted AS (
			INSERT INTO support_responses (
				support_request_id,
				responder_id,
				response_type,
				message
			)
			VALUES ($1, $2, $3, $4)
			RETURNING id, support_request_id, responder_id, response_type, message, created_at
		)
		SELECT
			i.id,
			i.support_request_id,
			i.responder_id,
			u.username,
			u.avatar_url,
			u.city,
			i.response_type,
			i.message,
			i.created_at
		FROM inserted i
		JOIN users u ON u.id = i.responder_id`,
		requestID, userID, input.ResponseType, input.Message,
	).Scan(
		&res.ID, &res.SupportRequestID, &res.ResponderID,
		&res.Username, &res.AvatarURL, &res.City,
		&res.ResponseType, &res.Message, &res.CreatedAt,
	)
	if err != nil {
		response.Error(w, http.StatusConflict, "could not create support response")
		return
	}

	if _, err := tx.Exec(r.Context(),
		`UPDATE support_requests SET response_count = response_count + 1 WHERE id = $1`,
		requestID,
	); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create support response")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create support response")
		return
	}

	response.Success(w, http.StatusCreated, res)
}

// ListSupportResponses returns a paginated list of responses for a support request owned by the caller.
func (h *Handler) ListSupportResponses(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	requestID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support request id")
		return
	}

	var requesterID uuid.UUID
	if err := h.db.QueryRow(r.Context(),
		`SELECT requester_id
		FROM support_requests
		WHERE id = $1`,
		requestID,
	).Scan(&requesterID); err != nil {
		response.Error(w, http.StatusNotFound, "support request not found")
		return
	}
	if requesterID != userID {
		response.Error(w, http.StatusForbidden, "cannot view support responses")
		return
	}

	params := pagination.Parse(r, 50, 100)

	rows, err := h.db.Query(r.Context(),
		`SELECT
			sres.id,
			sres.support_request_id,
			sres.responder_id,
			u.username,
			u.avatar_url,
			u.city,
			sres.response_type,
			sres.message,
			sres.created_at
		FROM support_responses sres
		JOIN users u ON u.id = sres.responder_id
		WHERE sres.support_request_id = $1
		ORDER BY sres.created_at ASC
		LIMIT $2 OFFSET $3`,
		requestID, params.Limit+1, params.Offset,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support responses")
		return
	}
	defer rows.Close()

	var responses []SupportResponse
	for rows.Next() {
		var res SupportResponse
		if err := rows.Scan(&res.ID, &res.SupportRequestID, &res.ResponderID, &res.Username, &res.AvatarURL, &res.City, &res.ResponseType, &res.Message, &res.CreatedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not read support responses")
			return
		}
		responses = append(responses, res)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read support responses")
		return
	}

	response.Success(w, http.StatusOK, pagination.Slice(responses, params))
}

// ListVisibleSupportRequests returns open support requests that should appear in the caller's feed.
// ListVisibleSupportRequests applies the same audience rules as the support
// feed while keeping pagination inside the main visibility query.
func (h *Handler) ListVisibleSupportRequests(r *http.Request, params pagination.CursorParams) ([]SupportRequest, error) {
	userID := middleware.CurrentUserID(r)
	return h.listSupportRequests(r.Context(),
		`SELECT
			sr.id,
			sr.requester_id,
			u.username,
			u.avatar_url,
			u.city,
			sr.type,
			sr.message,
			sr.audience,
			sr.status,
			sr.response_count,
			sr.expires_at,
			sr.created_at,
			EXISTS(
				SELECT 1
				FROM support_responses own_res
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
						SELECT 1
						FROM friendships f
						WHERE (f.user_a_id = $1 OR f.user_b_id = $1)
							AND (f.user_a_id = sr.requester_id OR f.user_b_id = sr.requester_id)
							AND f.status = 'accepted'
					)
				)
			)
		ORDER BY sr.created_at DESC
		LIMIT $3`,
		userID, params.Before, params.Limit+1,
	)
}

// fetchSupportSummary computes the support-tab header counts separately from
// the card page so the UI can show aggregates without downloading more rows.
func (h *Handler) fetchSupportSummary(ctx context.Context, viewerID uuid.UUID) (int, int, error) {
	var openCount int
	err := h.db.QueryRow(ctx,
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
						SELECT 1
						FROM friendships f
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
	err = h.db.QueryRow(ctx,
		`SELECT COUNT(*)
		FROM users
		WHERE id != $1
			AND is_available_to_support = true`,
		viewerID,
	).Scan(&availableCount)
	if err != nil {
		return 0, 0, err
	}

	return openCount, availableCount, nil
}

func (h *Handler) createSupportRequest(ctx context.Context, userID uuid.UUID, requestType string, message *string, audience string, expiresAt time.Time) (*SupportRequest, error) {
	var req SupportRequest
	err := h.db.QueryRow(ctx,
		`WITH inserted AS (
			INSERT INTO support_requests (
				requester_id,
				type,
				message,
				audience,
				city,
				status,
				expires_at
			)
			SELECT u.id, $2, $3, $4, u.city, 'open', $5
			FROM users u
			WHERE u.id = $1
			RETURNING id, requester_id, type, message, audience, status, expires_at, created_at
		)
		SELECT
			i.id, i.requester_id, i.type, i.message, i.audience, i.status, i.expires_at, i.created_at,
			u.username, u.avatar_url, u.city
		FROM inserted i
		JOIN users u ON u.id = i.requester_id`,
		userID, requestType, message, audience, expiresAt,
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

func (h *Handler) listSupportRequests(ctx context.Context, query string, args ...any) ([]SupportRequest, error) {
	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []SupportRequest
	for rows.Next() {
		var req SupportRequest
		if err := rows.Scan(
			&req.ID,
			&req.RequesterID,
			&req.Username,
			&req.AvatarURL,
			&req.City,
			&req.Type,
			&req.Message,
			&req.Audience,
			&req.Status,
			&req.ResponseCount,
			&req.ExpiresAt,
			&req.CreatedAt,
			&req.HasResponded,
			&req.IsOwnRequest,
		); err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return requests, nil
}

func (h *Handler) fetchSupportRequest(ctx context.Context, viewerID uuid.UUID, requestID uuid.UUID) (*SupportRequest, error) {
	var req SupportRequest
	err := h.db.QueryRow(ctx,
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
				SELECT 1
				FROM support_responses own_res
				WHERE own_res.support_request_id = sr.id
					AND own_res.responder_id = $2
			) AS has_responded,
			sr.requester_id = $2 AS is_own_request
		FROM support_requests sr
		JOIN users u ON u.id = sr.requester_id
		WHERE sr.id = $1`,
		requestID, viewerID,
	).Scan(
		&req.ID,
		&req.RequesterID,
		&req.Username,
		&req.AvatarURL,
		&req.City,
		&req.Type,
		&req.Message,
		&req.Audience,
		&req.Status,
		&req.ResponseCount,
		&req.ExpiresAt,
		&req.CreatedAt,
		&req.HasResponded,
		&req.IsOwnRequest,
	)
	if err != nil {
		return nil, err
	}

	return &req, nil
}
