package support

import (
	"context"
	"encoding/base64"
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
	CountOpenSupportRequests(ctx context.Context, userID uuid.UUID) (int, error)
	CreateImmediateSupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string) (*SupportRequest, error)
	CreateCommunitySupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string) (*SupportRequest, error)
	AcceptSupportResponse(ctx context.Context, requesterID, requestID, responseID uuid.UUID) (*SupportRequest, error)
	GetSupportRequest(ctx context.Context, viewerID, requestID uuid.UUID) (*SupportRequest, error)
	CloseSupportRequest(ctx context.Context, requestID, userID uuid.UUID) ([]uuid.UUID, error)
	ListMySupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error)
	ListVisibleSupportRequests(ctx context.Context, userID uuid.UUID, channel SupportChannel, cursor *SupportQueueCursor, limit int) ([]SupportRequest, error)
	GetSupportRequestState(ctx context.Context, requestID uuid.UUID) (requesterID uuid.UUID, status string, err error)
	CreateSupportResponse(ctx context.Context, requestID, userID uuid.UUID, responseType string, message *string, scheduledFor *time.Time) (*CreateSupportResponseResult, error)
	GetSupportRequestOwner(ctx context.Context, requestID uuid.UUID) (uuid.UUID, error)
	ListSupportResponses(ctx context.Context, requestID uuid.UUID, limit, offset int) ([]SupportResponse, error)
}

type Handler struct {
	db              Querier
	chatBroadcaster ChatBroadcaster
}

type ChatBroadcaster interface {
	BroadcastChatUpdate(ctx context.Context, chatID uuid.UUID) error
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

var validSupportChannels = map[SupportChannel]bool{
	SupportChannelImmediate: true,
	SupportChannelCommunity: true,
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

func NewHandlerWithChatBroadcaster(db Querier, chatBroadcaster ChatBroadcaster) *Handler {
	return &Handler{db: db, chatBroadcaster: chatBroadcaster}
}

type SupportRequest struct {
	ID                  uuid.UUID      `json:"id"`
	RequesterID         uuid.UUID      `json:"requester_id"`
	Username            string         `json:"username"`
	AvatarURL           *string        `json:"avatar_url"`
	City                *string        `json:"city"`
	Type                string         `json:"type"`
	Message             *string        `json:"message"`
	Urgency             string         `json:"urgency"`
	Status              string         `json:"status"`
	ResponseCount       int            `json:"response_count"`
	CreatedAt           time.Time      `json:"created_at"`
	Channel             SupportChannel `json:"channel,omitempty"`
	PrivacyLevel        string         `json:"privacy_level,omitempty"`
	AcceptedResponseID  *uuid.UUID     `json:"accepted_response_id,omitempty"`
	AcceptedResponderID *uuid.UUID     `json:"accepted_responder_id,omitempty"`
	AcceptedAt          *time.Time     `json:"accepted_at,omitempty"`
	ClosedAt            *time.Time     `json:"closed_at,omitempty"`
	ResponderID         *uuid.UUID     `json:"responder_id,omitempty"`
	ResponderUsername   *string        `json:"responder_username,omitempty"`
	ResponderAvatarURL  *string        `json:"responder_avatar_url,omitempty"`
	ChatID              *uuid.UUID     `json:"chat_id,omitempty"`
	HasResponded        bool           `json:"has_responded"`
	IsOwnRequest        bool           `json:"is_own_request"`
	SortAt              time.Time      `json:"-"`
	AttentionBucket     int            `json:"-"`
	UrgencyRank         int            `json:"-"`
}

type SupportQueueCursor struct {
	AttentionBucket int       `json:"attention_bucket"`
	UrgencyRank     int       `json:"urgency_rank"`
	CreatedAt       time.Time `json:"created_at"`
	ID              uuid.UUID `json:"id"`
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
	Status           string     `json:"status"`
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
	Items      []SupportRequest `json:"items"`
	Limit      int              `json:"limit"`
	HasMore    bool             `json:"has_more"`
	NextCursor *string          `json:"next_cursor,omitempty"`
}

type AcceptSupportResponseResult struct {
	Request *SupportRequest `json:"request"`
}

// CreateImmediateSupportRequest creates an immediate support request for the authenticated user.
func (h *Handler) CreateImmediateSupportRequest(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Type         string  `json:"type"`
		Message      *string `json:"message"`
		Urgency      string  `json:"urgency"`
		PrivacyLevel string  `json:"privacy_level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	normalized := normalizeCreateChannelSupportRequestInput(createChannelSupportRequestInput(input))
	if errs := validateCreateChannelSupportRequestInput(normalized); len(errs) > 0 {
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

	req, err := h.db.CreateImmediateSupportRequest(
		r.Context(),
		userID,
		normalized.Type,
		normalized.Message,
		normalized.Urgency,
		normalized.PrivacyLevel,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create immediate support request")
		return
	}

	response.Success(w, http.StatusCreated, req)
}

// CreateCommunitySupportRequest creates an asynchronous community support request for the authenticated user.
func (h *Handler) CreateCommunitySupportRequest(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Type         string  `json:"type"`
		Message      *string `json:"message"`
		Urgency      string  `json:"urgency"`
		PrivacyLevel string  `json:"privacy_level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	normalized := normalizeCreateChannelSupportRequestInput(createChannelSupportRequestInput(input))
	if errs := validateCreateChannelSupportRequestInput(normalized); len(errs) > 0 {
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

	req, err := h.db.CreateCommunitySupportRequest(
		r.Context(),
		userID,
		normalized.Type,
		normalized.Message,
		normalized.Urgency,
		normalized.PrivacyLevel,
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

// ListSupportRequests returns the visible support queue.
func (h *Handler) ListSupportRequests(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	channel, ok := parseSupportChannelQuery(r)
	if !ok {
		response.Error(w, http.StatusBadRequest, "invalid support channel")
		return
	}
	cursor, err := parseSupportQueueCursor(strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support cursor")
		return
	}
	params := pagination.Parse(r, 20, 50)

	requests, err := h.db.ListVisibleSupportRequests(r.Context(), userID, channel, cursor, params.Limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support requests")
		return
	}

	page := supportQueueSlice(requests, params.Limit)
	response.Success(w, http.StatusOK, SupportRequestsPage{
		Items:      page.Items,
		Limit:      page.Limit,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
	})
}

func parseSupportChannelQuery(r *http.Request) (SupportChannel, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("channel"))
	if raw == "" {
		return SupportChannelCommunity, true
	}
	channel := SupportChannel(raw)
	return channel, validSupportChannels[channel]
}

func parseSupportQueueCursor(raw string) (*SupportQueueCursor, error) {
	if raw == "" {
		return nil, nil
	}

	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}

	var cursor SupportQueueCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return nil, err
	}
	return &cursor, nil
}

func encodeSupportQueueCursor(cursor SupportQueueCursor) (*string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return nil, err
	}

	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return &encoded, nil
}

func supportQueueSlice(items []SupportRequest, limit int) pagination.CursorResponse[SupportRequest] {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		cursor, err := encodeSupportQueueCursor(SupportQueueCursor{
			AttentionBucket: last.AttentionBucket,
			UrgencyRank:     last.UrgencyRank,
			CreatedAt:       last.CreatedAt,
			ID:              last.ID,
		})
		if err == nil {
			nextCursor = cursor
		}
	}

	return pagination.CursorResponse[SupportRequest]{
		Items:      items,
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}
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

// UpdateSupportRequest lets a requester or accepted responder close a support request.
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

	closedChatIDs, err := h.db.CloseSupportRequest(r.Context(), requestID, userID)
	if err != nil {
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

	if h.chatBroadcaster != nil {
		for _, chatID := range closedChatIDs {
			_ = h.chatBroadcaster.BroadcastChatUpdate(r.Context(), chatID)
		}
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

	res, err := h.db.CreateSupportResponse(r.Context(), requestID, userID, input.ResponseType, input.Message, scheduledFor)
	if err != nil {
		response.Error(w, http.StatusConflict, "could not create support response")
		return
	}

	response.Success(w, http.StatusCreated, res)
}

// AcceptSupportResponse lets the requester choose one response and only then open the support chat.
func (h *Handler) AcceptSupportResponse(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	requestID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support request id")
		return
	}
	responseID, err := uuid.Parse(chi.URLParam(r, "responseId"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support response id")
		return
	}

	req, err := h.db.AcceptSupportResponse(r.Context(), userID, requestID, responseID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "support response not found")
			return
		}
		if errors.Is(err, ErrConflict) {
			response.Error(w, http.StatusConflict, "support response is no longer available")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not accept support response")
		return
	}

	response.Success(w, http.StatusOK, AcceptSupportResponseResult{Request: req})
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
