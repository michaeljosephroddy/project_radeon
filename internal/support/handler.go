package support

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/pagination"
	"github.com/project_radeon/api/pkg/response"
)

// Querier is the database interface required by the support handler.
type Querier interface {
	GetSupportProfile(ctx context.Context, userID uuid.UUID) (*SupportProfile, error)
	UpdateSupportProfile(ctx context.Context, userID uuid.UUID, available bool) (*SupportProfile, error)
	GetSupportHome(ctx context.Context, userID uuid.UUID) (*SupportHomePayload, error)
	GetSupportResponderProfile(ctx context.Context, userID uuid.UUID) (*SupportResponderProfile, error)
	UpdateSupportResponderProfile(ctx context.Context, userID uuid.UUID, input UpdateSupportResponderProfileInput) (*SupportResponderProfile, error)
	CountOpenSupportRequests(ctx context.Context, userID uuid.UUID) (int, error)
	CreateSupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, priorityVisibility bool, priorityExpiresAt *time.Time) (*SupportRequest, error)
	CreateImmediateSupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string, priorityVisibility bool, priorityExpiresAt *time.Time) (*SupportRequest, error)
	CreateCommunitySupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string, priorityVisibility bool, priorityExpiresAt *time.Time) (*SupportRequest, error)
	RouteSupportRequest(ctx context.Context, requestID uuid.UUID) error
	AcceptSupportOffer(ctx context.Context, responderID, offerID uuid.UUID) (*SupportSession, error)
	DeclineSupportOffer(ctx context.Context, responderID, offerID uuid.UUID) error
	GetSupportRequest(ctx context.Context, viewerID, requestID uuid.UUID) (*SupportRequest, error)
	CloseSupportRequest(ctx context.Context, requestID, userID uuid.UUID) error
	ListMySupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error)
	ListVisibleSupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error)
	ListRespondedSupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error)
	ListResponderQueue(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportOffer, error)
	ListSupportSessions(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportSession, error)
	CloseSupportSession(ctx context.Context, userID, sessionID uuid.UUID, outcome string) (*SupportSession, error)
	SweepExpiredSupportOffers(ctx context.Context) error
	FetchSupportSummary(ctx context.Context, viewerID uuid.UUID) (openCount, availableCount int, err error)
	GetSupportRequestState(ctx context.Context, requestID uuid.UUID) (requesterID uuid.UUID, status string, err error)
	CreateSupportResponse(ctx context.Context, requestID, userID uuid.UUID, responseType string, message *string, scheduledFor *time.Time) (*CreateSupportResponseResult, error)
	GetSupportRequestOwner(ctx context.Context, requestID uuid.UUID) (uuid.UUID, error)
	ListSupportResponses(ctx context.Context, requestID uuid.UUID, limit, offset int) ([]SupportResponse, error)
}

type Handler struct {
	db Querier
}

var validSupportTypes = map[string]bool{
	"need_to_talk":        true,
	"need_distraction":    true,
	"need_encouragement":  true,
	"need_in_person_help": true,
}

var validSupportUrgencies = map[string]bool{
	"when_you_can": true,
	"soon":         true,
	"right_now":    true,
}

var validSupportPrivacyLevels = map[string]bool{
	"standard": true,
	"private":  true,
}

var validSupportResponseTypes = map[string]bool{
	"can_chat":       true,
	"check_in_later": true,
	"can_meet":       true,
}

// NewHandler builds a support handler. Pass support.NewPgStore(pool) for production.
func NewHandler(db Querier) *Handler {
	return &Handler{db: db}
}

type SupportProfile struct {
	IsAvailableToSupport bool       `json:"is_available_to_support"`
	SupportUpdatedAt     *time.Time `json:"support_updated_at,omitempty"`
}

type SupportRequest struct {
	ID                    uuid.UUID            `json:"id"`
	RequesterID           uuid.UUID            `json:"requester_id"`
	Username              string               `json:"username"`
	AvatarURL             *string              `json:"avatar_url"`
	City                  *string              `json:"city"`
	Type                  string               `json:"type"`
	Message               *string              `json:"message"`
	Urgency               string               `json:"urgency"`
	Status                string               `json:"status"`
	ResponseCount         int                  `json:"response_count"`
	CreatedAt             time.Time            `json:"created_at"`
	PriorityVisibility    bool                 `json:"priority_visibility"`
	PriorityExpiresAt     *time.Time           `json:"priority_expires_at,omitempty"`
	Channel               SupportChannel       `json:"channel,omitempty"`
	RoutingStatus         SupportRoutingStatus `json:"routing_status,omitempty"`
	DesiredResponseWindow string               `json:"desired_response_window,omitempty"`
	PrivacyLevel          string               `json:"privacy_level,omitempty"`
	MatchedSessionID      *uuid.UUID           `json:"matched_session_id,omitempty"`
	HasResponded          bool                 `json:"has_responded"`
	IsOwnRequest          bool                 `json:"is_own_request"`
	SortAt                time.Time            `json:"-"`
}

type SupportResponse struct {
	ID               uuid.UUID  `json:"id"`
	SupportRequestID uuid.UUID  `json:"support_request_id"`
	ResponderID      uuid.UUID  `json:"responder_id"`
	Username         string     `json:"username"`
	AvatarURL        *string    `json:"avatar_url"`
	City             *string    `json:"city"`
	ResponseType     string     `json:"response_type"`
	Message          *string    `json:"message"`
	ScheduledFor     *time.Time `json:"scheduled_for,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	ChatID           *uuid.UUID `json:"chat_id,omitempty"`
}

type SupportChatContext struct {
	SupportRequestID   uuid.UUID  `json:"support_request_id"`
	RequestType        string     `json:"request_type"`
	RequestMessage     *string    `json:"request_message,omitempty"`
	RequesterID        uuid.UUID  `json:"requester_id"`
	RequesterUsername  string     `json:"requester_username"`
	LatestResponseType *string    `json:"latest_response_type,omitempty"`
	Status             string     `json:"status"`
	AwaitingUserID     *uuid.UUID `json:"awaiting_user_id,omitempty"`
}

type ChatSummary struct {
	ID             uuid.UUID           `json:"id"`
	IsGroup        bool                `json:"is_group"`
	Name           *string             `json:"name"`
	Username       *string             `json:"username"`
	AvatarURL      *string             `json:"avatar_url"`
	CreatedAt      time.Time           `json:"created_at"`
	LastMessage    *string             `json:"last_message,omitempty"`
	LastMessageAt  *time.Time          `json:"last_message_at,omitempty"`
	Status         string              `json:"status"`
	SupportContext *SupportChatContext `json:"support_context,omitempty"`
}

type CreateSupportResponseResult struct {
	Response *SupportResponse `json:"response"`
	Chat     *ChatSummary     `json:"chat,omitempty"`
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

	profile, err := h.db.GetSupportProfile(r.Context(), userID)
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
		IsAvailableToSupport bool `json:"is_available_to_support"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	profile, err := h.db.UpdateSupportProfile(r.Context(), userID, input.IsAvailableToSupport)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update support profile")
		return
	}

	response.Success(w, http.StatusOK, profile)
}

// GetSupportHome returns the current user's routed support home payload.
func (h *Handler) GetSupportHome(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	home, err := h.db.GetSupportHome(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support home")
		return
	}

	response.Success(w, http.StatusOK, home)
}

// GetMyResponderProfile returns the richer support responder profile used by the routing platform.
func (h *Handler) GetMyResponderProfile(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	profile, err := h.db.GetSupportResponderProfile(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch responder profile")
		return
	}

	response.Success(w, http.StatusOK, profile)
}

// UpdateMyResponderProfile saves the richer support responder profile used by the routing platform.
func (h *Handler) UpdateMyResponderProfile(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		IsAvailableForImmediate bool     `json:"is_available_for_immediate"`
		IsAvailableForCommunity bool     `json:"is_available_for_community"`
		SupportsChat            bool     `json:"supports_chat"`
		SupportsCheckIns        bool     `json:"supports_check_ins"`
		SupportsInPerson        bool     `json:"supports_in_person"`
		MaxConcurrentSessions   int      `json:"max_concurrent_sessions"`
		Languages               []string `json:"languages"`
		AvailableNow            bool     `json:"available_now"`
		IsActive                bool     `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	normalized := normalizeUpdateSupportResponderProfileInput(updateSupportResponderProfileInput(input))
	if errs := validateUpdateSupportResponderProfileInput(normalized); len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}

	profile, err := h.db.UpdateSupportResponderProfile(r.Context(), userID, UpdateSupportResponderProfileInput{
		IsAvailableForImmediate: normalized.IsAvailableForImmediate,
		IsAvailableForCommunity: normalized.IsAvailableForCommunity,
		SupportsChat:            normalized.SupportsChat,
		SupportsCheckIns:        normalized.SupportsCheckIns,
		SupportsInPerson:        normalized.SupportsInPerson,
		MaxConcurrentSessions:   normalized.MaxConcurrentSessions,
		Languages:               normalized.Languages,
		AvailableNow:            normalized.AvailableNow,
		IsActive:                normalized.IsActive,
	})
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update responder profile")
		return
	}

	response.Success(w, http.StatusOK, profile)
}

// CreateSupportRequest creates a support request for the authenticated user.
func (h *Handler) CreateSupportRequest(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Type               string  `json:"type"`
		Message            *string `json:"message"`
		Urgency            string  `json:"urgency"`
		PriorityVisibility bool    `json:"priority_visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	normalized := normalizeCreateSupportRequestInput(createSupportRequestInput(input))
	errs := validateCreateSupportRequestInput(normalized)
	if len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}

	openCount, err := h.db.CountOpenSupportRequests(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not validate support request")
		return
	}
	if openCount > 0 {
		response.Error(w, http.StatusConflict, "you already have an open support request")
		return
	}

	var priorityExpiresAt *time.Time
	if input.PriorityVisibility {
		expires := time.Now().Add(time.Hour)
		priorityExpiresAt = &expires
	}

	req, err := h.db.CreateSupportRequest(
		r.Context(),
		userID,
		normalized.Type,
		normalized.Message,
		normalized.Urgency,
		input.PriorityVisibility,
		priorityExpiresAt,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create support request")
		return
	}

	response.Success(w, http.StatusCreated, req)
}

// CreateImmediateSupportRequest creates a routed immediate support request for the authenticated user.
func (h *Handler) CreateImmediateSupportRequest(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Type               string  `json:"type"`
		Message            *string `json:"message"`
		Urgency            string  `json:"urgency"`
		PrivacyLevel       string  `json:"privacy_level"`
		PriorityVisibility bool    `json:"priority_visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	normalized := normalizeCreateRoutedSupportRequestInput(createRoutedSupportRequestInput(input))
	if errs := validateCreateRoutedSupportRequestInput(normalized); len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}

	openCount, err := h.db.CountOpenSupportRequests(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not validate support request")
		return
	}
	if openCount > 0 {
		response.Error(w, http.StatusConflict, "you already have an open support request")
		return
	}

	var priorityExpiresAt *time.Time
	if normalized.PriorityVisibility {
		expires := time.Now().Add(time.Hour)
		priorityExpiresAt = &expires
	}

	req, err := h.db.CreateImmediateSupportRequest(
		r.Context(),
		userID,
		normalized.Type,
		normalized.Message,
		normalized.Urgency,
		normalized.PrivacyLevel,
		normalized.PriorityVisibility,
		priorityExpiresAt,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create immediate support request")
		return
	}

	_ = h.db.RouteSupportRequest(r.Context(), req.ID)

	response.Success(w, http.StatusCreated, req)
}

// CreateCommunitySupportRequest creates an asynchronous community support request for the authenticated user.
func (h *Handler) CreateCommunitySupportRequest(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Type               string  `json:"type"`
		Message            *string `json:"message"`
		Urgency            string  `json:"urgency"`
		PrivacyLevel       string  `json:"privacy_level"`
		PriorityVisibility bool    `json:"priority_visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	normalized := normalizeCreateRoutedSupportRequestInput(createRoutedSupportRequestInput(input))
	if errs := validateCreateRoutedSupportRequestInput(normalized); len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}

	openCount, err := h.db.CountOpenSupportRequests(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not validate support request")
		return
	}
	if openCount > 0 {
		response.Error(w, http.StatusConflict, "you already have an open support request")
		return
	}

	var priorityExpiresAt *time.Time
	if normalized.PriorityVisibility {
		expires := time.Now().Add(time.Hour)
		priorityExpiresAt = &expires
	}

	req, err := h.db.CreateCommunitySupportRequest(
		r.Context(),
		userID,
		normalized.Type,
		normalized.Message,
		normalized.Urgency,
		normalized.PrivacyLevel,
		normalized.PriorityVisibility,
		priorityExpiresAt,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create community support request")
		return
	}

	response.Success(w, http.StatusCreated, req)
}

// ListMySupportRequests returns support requests created by the authenticated user.
func (h *Handler) ListMySupportRequests(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 20, 50)

	requests, err := h.db.ListMySupportRequests(r.Context(), userID, params.Before, params.Limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support requests")
		return
	}

	page := pagination.CursorSlice(requests, params.Limit, func(sr SupportRequest) time.Time { return sr.SortAt })
	response.Success(w, http.StatusOK, SupportRequestsPage{
		Items:      page.Items,
		Limit:      page.Limit,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
	})
}

// ListSupportRequests returns the visible support page plus the lightweight tab summary counts.
func (h *Handler) ListSupportRequests(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 20, 50)

	requests, err := h.db.ListVisibleSupportRequests(r.Context(), userID, params.Before, params.Limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support requests")
		return
	}

	openCount, availableCount, err := h.db.FetchSupportSummary(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support requests")
		return
	}

	page := pagination.CursorSlice(requests, params.Limit, func(sr SupportRequest) time.Time { return sr.SortAt })
	response.Success(w, http.StatusOK, SupportRequestsPage{
		Items:                   page.Items,
		Limit:                   page.Limit,
		HasMore:                 page.HasMore,
		NextCursor:              page.NextCursor,
		OpenRequestCount:        &openCount,
		AvailableToSupportCount: &availableCount,
	})
}

// ListRespondedSupportRequests returns closed community support requests the authenticated user responded to.
func (h *Handler) ListRespondedSupportRequests(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 20, 50)

	requests, err := h.db.ListRespondedSupportRequests(r.Context(), userID, params.Before, params.Limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch responded support requests")
		return
	}

	page := pagination.CursorSlice(requests, params.Limit, func(sr SupportRequest) time.Time { return sr.SortAt })
	response.Success(w, http.StatusOK, SupportRequestsPage{
		Items:      page.Items,
		Limit:      page.Limit,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
	})
}

// ListResponderQueue returns the authenticated responder's targeted offer queue.
func (h *Handler) ListResponderQueue(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 20, 50)

	offers, err := h.db.ListResponderQueue(r.Context(), userID, params.Before, params.Limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support queue")
		return
	}

	page := pagination.CursorSlice(offers, params.Limit, func(item SupportOffer) time.Time { return item.SortAt })
	response.Success(w, http.StatusOK, SupportOfferPage{
		Items:      page.Items,
		Limit:      page.Limit,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
	})
}

// ListSupportSessions returns support sessions for the authenticated user.
func (h *Handler) ListSupportSessions(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 20, 50)

	sessions, err := h.db.ListSupportSessions(r.Context(), userID, params.Before, params.Limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support sessions")
		return
	}

	page := pagination.CursorSlice(sessions, params.Limit, func(item SupportSession) time.Time { return item.SortAt })
	response.Success(w, http.StatusOK, SupportSessionsPage{
		Items:      page.Items,
		Limit:      page.Limit,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
	})
}

// CloseSupportSession marks a support session as completed or cancelled.
func (h *Handler) CloseSupportSession(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid session id")
		return
	}

	var input struct {
		Outcome string `json:"outcome"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.Outcome = strings.TrimSpace(input.Outcome)
	if input.Outcome == "" {
		input.Outcome = "completed"
	}

	session, err := h.db.CloseSupportSession(r.Context(), userID, sessionID, input.Outcome)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "support session not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not close support session")
		return
	}

	response.Success(w, http.StatusOK, session)
}

// AcceptSupportOffer accepts a routed immediate-support offer for the authenticated responder.
func (h *Handler) AcceptSupportOffer(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	offerID, err := uuid.Parse(chi.URLParam(r, "offerID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid offer id")
		return
	}

	session, err := h.db.AcceptSupportOffer(r.Context(), userID, offerID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "support offer not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not accept support offer")
		return
	}

	response.Success(w, http.StatusOK, session)
}

// DeclineSupportOffer declines a routed immediate-support offer for the authenticated responder.
func (h *Handler) DeclineSupportOffer(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	offerID, err := uuid.Parse(chi.URLParam(r, "offerID"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid offer id")
		return
	}

	if err := h.db.DeclineSupportOffer(r.Context(), userID, offerID); err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "support offer not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not decline support offer")
		return
	}

	response.Success(w, http.StatusOK, map[string]bool{"ok": true})
}

// GetSupportRequest returns one support request with viewer-specific metadata.
func (h *Handler) GetSupportRequest(w http.ResponseWriter, r *http.Request) {
	viewerID := middleware.CurrentUserID(r)
	requestID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support request id")
		return
	}

	req, err := h.db.GetSupportRequest(r.Context(), viewerID, requestID)
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
	if !isSupportedRequestStatusUpdate(input.Status) {
		response.Error(w, http.StatusBadRequest, "unsupported support request update")
		return
	}

	if err := h.db.CloseSupportRequest(r.Context(), requestID, userID); err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "support request not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not update support request")
		return
	}

	req, err := h.db.GetSupportRequest(r.Context(), userID, requestID)
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

	var input createSupportResponseInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input = normalizeCreateSupportResponseInput(input)
	if errs := validateCreateSupportResponseInput(input); len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}

	scheduledFor, err := parseSupportResponseScheduledFor(input.ScheduledFor)
	if err != nil {
		response.ValidationError(w, map[string]string{"scheduled_for": "invalid"})
		return
	}

	requesterID, status, err := h.db.GetSupportRequestState(r.Context(), requestID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "support request not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not fetch support request")
		return
	}
	if requesterID == userID {
		response.Error(w, http.StatusBadRequest, "cannot respond to your own request")
		return
	}
	if status != "open" {
		response.Error(w, http.StatusConflict, "support request is no longer open")
		return
	}

	profile, err := h.db.GetSupportProfile(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support profile")
		return
	}
	if !profile.IsAvailableToSupport {
		response.Error(w, http.StatusForbidden, "turn on support availability to respond")
		return
	}

	res, err := h.db.CreateSupportResponse(r.Context(), requestID, userID, input.ResponseType, input.Message, scheduledFor)
	if err != nil {
		response.Error(w, http.StatusConflict, "could not create support response")
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

	ownerID, err := h.db.GetSupportRequestOwner(r.Context(), requestID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "support request not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not fetch support request")
		return
	}
	if ownerID != userID {
		response.Error(w, http.StatusForbidden, "cannot view support responses")
		return
	}

	params := pagination.Parse(r, 50, 100)

	responses, err := h.db.ListSupportResponses(r.Context(), requestID, params.Limit+1, params.Offset)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support responses")
		return
	}

	response.Success(w, http.StatusOK, pagination.Slice(responses, params))
}
