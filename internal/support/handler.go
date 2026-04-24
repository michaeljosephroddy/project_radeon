package support

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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
	CountOpenSupportRequests(ctx context.Context, userID uuid.UUID) (int, error)
	CreateSupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, priorityVisibility bool, priorityExpiresAt *time.Time) (*SupportRequest, error)
	GetSupportRequest(ctx context.Context, viewerID, requestID uuid.UUID) (*SupportRequest, error)
	CloseSupportRequest(ctx context.Context, requestID, userID uuid.UUID) error
	ListMySupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error)
	ListVisibleSupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error)
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
	"need_to_talk":         true,
	"need_distraction":     true,
	"need_encouragement":   true,
	"need_in_person_help":  true,
}

var validSupportUrgencies = map[string]bool{
	"when_you_can": true,
	"soon":         true,
	"right_now":    true,
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
	ID                 uuid.UUID  `json:"id"`
	RequesterID        uuid.UUID  `json:"requester_id"`
	Username           string     `json:"username"`
	AvatarURL          *string    `json:"avatar_url"`
	City               *string    `json:"city"`
	Type               string     `json:"type"`
	Message            *string    `json:"message"`
	Urgency            string     `json:"urgency"`
	Status             string     `json:"status"`
	ResponseCount      int        `json:"response_count"`
	CreatedAt          time.Time  `json:"created_at"`
	PriorityVisibility bool       `json:"priority_visibility"`
	PriorityExpiresAt  *time.Time `json:"priority_expires_at,omitempty"`
	HasResponded       bool       `json:"has_responded"`
	IsOwnRequest       bool       `json:"is_own_request"`
	SortAt             time.Time  `json:"-"`
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
