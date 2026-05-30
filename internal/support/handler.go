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
	"github.com/project_radeon/api/internal/moderation"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/pagination"
	"github.com/project_radeon/api/pkg/response"
)

// Querier is the database interface required by the support handler.
type Querier interface {
	CountOpenSupportRequests(ctx context.Context, userID uuid.UUID) (int, error)
	CountHighUrgencySupportRequestsSince(ctx context.Context, userID uuid.UUID, since time.Time) (int, error)
	CreateSupportRequest(ctx context.Context, userID uuid.UUID, input CreateSupportRequestInput) (*SupportRequest, error)
	AcceptSupportOffer(ctx context.Context, requesterID, requestID, offerID uuid.UUID) (*SupportRequest, error)
	GetSupportRequest(ctx context.Context, viewerID, requestID uuid.UUID) (*SupportRequest, error)
	CloseSupportRequest(ctx context.Context, requestID, userID uuid.UUID) ([]uuid.UUID, error)
	ListMySupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error)
	ListVisibleSupportRequests(ctx context.Context, userID uuid.UUID, filter SupportRequestFilter, cursor *SupportFeedCursor, limit int) ([]SupportRequest, error)
	GetSupportRequestState(ctx context.Context, requestID uuid.UUID) (requesterID uuid.UUID, status string, err error)
	CreateSupportOffer(ctx context.Context, requestID, userID uuid.UUID, offerType string, message *string, scheduledFor *time.Time) (*CreateSupportOfferResult, error)
	GetSupportRequestOwner(ctx context.Context, requestID uuid.UUID) (uuid.UUID, error)
	ListSupportOffers(ctx context.Context, requestID uuid.UUID, status string, limit, offset int) ([]SupportOffer, error)
	CreateSupportReply(ctx context.Context, requestID, authorID uuid.UUID, body string) (*SupportReply, error)
	ListSupportReplies(ctx context.Context, requestID uuid.UUID, cursor *SupportReplyCursor, limit int) ([]SupportReply, error)
	DeclineSupportOffer(ctx context.Context, requesterID, requestID, offerID uuid.UUID) error
	CancelSupportOffer(ctx context.Context, responderID, requestID, offerID uuid.UUID) error
	CountSupportSignalsSince(ctx context.Context, userID uuid.UUID, since time.Time) (int, error)
	GetActiveSupportSignal(ctx context.Context, viewerID, signalID uuid.UUID) (*SupportSignal, error)
	GetActiveSupportSignalForUser(ctx context.Context, viewerID, userID uuid.UUID) (*SupportSignal, error)
	ListActiveSupportSignals(ctx context.Context, viewerID uuid.UUID, before *time.Time, limit int) ([]SupportSignal, error)
	CreateSupportSignal(ctx context.Context, userID uuid.UUID, input CreateSupportSignalInput, expiresAt time.Time) (*SupportSignal, error)
	ResolveSupportSignal(ctx context.Context, userID, signalID uuid.UUID) (*SupportSignal, error)
	CancelSupportSignal(ctx context.Context, userID, signalID uuid.UUID) (*SupportSignal, error)
	RespondToSupportSignal(ctx context.Context, userID, signalID uuid.UUID) (*SupportSignalResponseResult, error)
}

type Handler struct {
	db              Querier
	chatBroadcaster ChatBroadcaster
	notifier        SupportNotifier
	moderator       moderation.Service
}

type ChatBroadcaster interface {
	BroadcastChatUpdate(ctx context.Context, chatID uuid.UUID) error
}

type SupportNotifier interface {
	NotifySupportOffer(ctx context.Context, requestID, offerID, responderID, requesterID uuid.UUID) error
	NotifySupportSignal(ctx context.Context, signalID, requesterID uuid.UUID) error
	NotifySupportSignalResponse(ctx context.Context, signalID, responderID, requesterID, chatID uuid.UUID) error
}

var validSupportTypes = map[string]bool{
	"chat":    true,
	"call":    true,
	"meetup":  true,
	"general": true,
}

var validSupportUrgencies = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
}

var validSupportPrivacyLevels = map[string]bool{
	"standard": true,
	"private":  true,
}

var validSupportOfferTypes = map[string]bool{
	"chat":   true,
	"call":   true,
	"meetup": true,
}

var validSupportTopics = map[string]bool{
	"cravings":          true,
	"relapse_risk":      true,
	"mental_health":     true,
	"loneliness":        true,
	"relationships":     true,
	"practical_support": true,
	"general":           true,
}

var validSupportSignalReasons = map[string]bool{
	"cravings":     true,
	"relapse_risk": true,
	"overwhelmed":  true,
	"lonely":       true,
	"risky_place":  true,
	"need_to_talk": true,
}

var validPreferredGenders = map[string]bool{
	"woman":         true,
	"man":           true,
	"non_binary":    true,
	"no_preference": true,
}

var validLocationVisibilities = map[string]bool{
	"hidden":      true,
	"city":        true,
	"approximate": true,
}

var validSupportRequestFilters = map[SupportRequestFilter]bool{
	SupportRequestFilterAll:        true,
	SupportRequestFilterUrgent:     true,
	SupportRequestFilterUnanswered: true,
}

// NewHandler builds a support handler. Pass support.NewPgStore(pool) for production.
func NewHandler(db Querier) *Handler {
	return &Handler{db: db, moderator: moderation.Disabled()}
}

func NewHandlerWithChatBroadcaster(db Querier, chatBroadcaster ChatBroadcaster, notifiers ...SupportNotifier) *Handler {
	h := &Handler{db: db, chatBroadcaster: chatBroadcaster, moderator: moderation.Disabled()}
	if len(notifiers) > 0 {
		h.notifier = notifiers[0]
	}
	return h
}

func (h *Handler) UseModerator(service moderation.Service) {
	if service == nil {
		service = moderation.Disabled()
	}
	h.moderator = service
}

type SupportRequest struct {
	ID                  uuid.UUID        `json:"id"`
	RequesterID         uuid.UUID        `json:"requester_id"`
	Username            string           `json:"username"`
	AvatarURL           *string          `json:"avatar_url"`
	City                *string          `json:"city"`
	SupportType         string           `json:"support_type"`
	Topics              []string         `json:"topics"`
	PreferredGender     *string          `json:"preferred_gender,omitempty"`
	Location            *SupportLocation `json:"location,omitempty"`
	Message             *string          `json:"message"`
	Urgency             string           `json:"urgency"`
	Status              string           `json:"status"`
	ReplyCount          int              `json:"reply_count"`
	OfferCount          int              `json:"offer_count"`
	ResponseCount       int              `json:"-"`
	ViewCount           int              `json:"view_count"`
	IsPriority          bool             `json:"is_priority"`
	GroupPostID         *uuid.UUID       `json:"group_post_id,omitempty"`
	CreatedAt           time.Time        `json:"created_at"`
	PrivacyLevel        string           `json:"privacy_level,omitempty"`
	AcceptedResponseID  *uuid.UUID       `json:"-"`
	AcceptedResponderID *uuid.UUID       `json:"-"`
	AcceptedAt          *time.Time       `json:"-"`
	ClosedAt            *time.Time       `json:"closed_at,omitempty"`
	ResponderID         *uuid.UUID       `json:"-"`
	ResponderUsername   *string          `json:"-"`
	ResponderAvatarURL  *string          `json:"-"`
	ChatID              *uuid.UUID       `json:"chat_id,omitempty"`
	HasResponded        bool             `json:"-"`
	HasOffered          bool             `json:"has_offered"`
	HasReplied          bool             `json:"has_replied"`
	AlreadyChatting     bool             `json:"already_chatting"`
	ExistingChatID      *uuid.UUID       `json:"-"`
	IsOwnRequest        bool             `json:"is_own_request"`
	SortAt              time.Time        `json:"-"`
	AttentionBucket     int              `json:"-"`
	UrgencyRank         int              `json:"-"`
	FeedScore           float64          `json:"-"`
}

type SupportSignal struct {
	ID            uuid.UUID  `json:"id"`
	UserID        uuid.UUID  `json:"user_id"`
	Username      string     `json:"username"`
	AvatarURL     *string    `json:"avatar_url,omitempty"`
	City          *string    `json:"city,omitempty"`
	Reason        string     `json:"reason"`
	Status        string     `json:"status"`
	ExpiresAt     time.Time  `json:"expires_at"`
	ResponseCount int        `json:"response_count"`
	CreatedAt     time.Time  `json:"created_at"`
	ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
	CancelledAt   *time.Time `json:"cancelled_at,omitempty"`
	IsOwnSignal   bool       `json:"is_own_signal"`
	IsFriend      bool       `json:"is_friend"`
}

type CreateSupportSignalInput struct {
	Reason string `json:"reason"`
}

type SupportSignalsPage struct {
	Items      []SupportSignal `json:"items"`
	Limit      int             `json:"limit"`
	HasMore    bool            `json:"has_more"`
	NextCursor *string         `json:"next_cursor,omitempty"`
}

type SupportSignalResponseResult struct {
	Signal *SupportSignal `json:"signal"`
	ChatID uuid.UUID      `json:"chat_id"`
}

type SupportLocation struct {
	City           *string  `json:"city,omitempty"`
	Region         *string  `json:"region,omitempty"`
	Country        *string  `json:"country,omitempty"`
	ApproximateLat *float64 `json:"approximate_lat,omitempty"`
	ApproximateLng *float64 `json:"approximate_lng,omitempty"`
	Visibility     string   `json:"visibility"`
}

type SupportRequestFilter string

const (
	SupportRequestFilterAll        SupportRequestFilter = "all"
	SupportRequestFilterUrgent     SupportRequestFilter = "urgent"
	SupportRequestFilterUnanswered SupportRequestFilter = "unanswered"
)

type SupportFeedCursor struct {
	Score     float64   `json:"score"`
	CreatedAt time.Time `json:"created_at"`
	ID        uuid.UUID `json:"id"`
	ServedAt  time.Time `json:"served_at"`
}

type SupportReplyCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        uuid.UUID `json:"id"`
}

type SupportOffer struct {
	ID               uuid.UUID  `json:"id"`
	SupportRequestID uuid.UUID  `json:"support_request_id"`
	ResponderID      uuid.UUID  `json:"responder_id"`
	Username         string     `json:"username"`
	AvatarURL        *string    `json:"avatar_url"`
	City             *string    `json:"city"`
	OfferType        string     `json:"offer_type"`
	Message          *string    `json:"message"`
	Status           string     `json:"status"`
	ScheduledFor     *time.Time `json:"scheduled_for,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	ChatID           *uuid.UUID `json:"chat_id,omitempty"`
}

type SupportReply struct {
	ID               uuid.UUID `json:"id"`
	SupportRequestID uuid.UUID `json:"support_request_id"`
	AuthorID         uuid.UUID `json:"author_id"`
	Username         string    `json:"username"`
	AvatarURL        *string   `json:"avatar_url"`
	Body             string    `json:"body"`
	CreatedAt        time.Time `json:"created_at"`
}

type CreateSupportOfferResult struct {
	Offer *SupportOffer `json:"offer"`
}

type SupportRequestsPage struct {
	Items      []SupportRequest `json:"items"`
	Limit      int              `json:"limit"`
	HasMore    bool             `json:"has_more"`
	NextCursor *string          `json:"next_cursor,omitempty"`
}

type AcceptSupportOfferResult struct {
	Request *SupportRequest `json:"request"`
}

func (h *Handler) ListActiveSupportSignals(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 20, 50)

	signals, err := h.db.ListActiveSupportSignals(r.Context(), userID, params.Before, params.Limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch reach out signals")
		return
	}

	page := pagination.CursorSlice(signals, params.Limit, func(signal SupportSignal) time.Time { return signal.CreatedAt })
	response.Success(w, http.StatusOK, SupportSignalsPage{
		Items:      page.Items,
		Limit:      page.Limit,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
	})
}

func (h *Handler) GetMySupportSignal(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	signal, err := h.db.GetActiveSupportSignalForUser(r.Context(), userID, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Success(w, http.StatusOK, map[string]any{"signal": nil})
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not fetch reach out signal")
		return
	}
	response.Success(w, http.StatusOK, map[string]any{"signal": signal})
}

func (h *Handler) GetSupportSignal(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	signalID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid reach out signal id")
		return
	}

	signal, err := h.db.GetActiveSupportSignal(r.Context(), userID, signalID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "reach out signal not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not fetch reach out signal")
		return
	}
	response.Success(w, http.StatusOK, signal)
}

func (h *Handler) CreateSupportSignal(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	var input CreateSupportSignalInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input = normalizeCreateSupportSignalInput(input)
	if errs := validateCreateSupportSignalInput(input); len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}
	recentCount, err := h.db.CountSupportSignalsSince(r.Context(), userID, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not validate reach out signal")
		return
	}
	if recentCount >= 3 {
		response.Error(w, http.StatusTooManyRequests, "you've used your reach out signals for today")
		return
	}

	signal, err := h.db.CreateSupportSignal(r.Context(), userID, input, time.Now().UTC().Add(2*time.Hour))
	if err != nil {
		if errors.Is(err, ErrConflict) {
			response.Error(w, http.StatusConflict, "you already have an active reach out signal")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not create reach out signal")
		return
	}
	if h.notifier != nil {
		_ = h.notifier.NotifySupportSignal(r.Context(), signal.ID, userID)
	}
	response.Success(w, http.StatusCreated, signal)
}

func (h *Handler) RespondToSupportSignal(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	signalID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid reach out signal id")
		return
	}

	result, err := h.db.RespondToSupportSignal(r.Context(), userID, signalID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "reach out signal not found")
			return
		}
		if errors.Is(err, ErrConflict) {
			response.Error(w, http.StatusConflict, "reach out signal is no longer active")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not respond to reach out signal")
		return
	}
	if h.chatBroadcaster != nil {
		_ = h.chatBroadcaster.BroadcastChatUpdate(r.Context(), result.ChatID)
	}
	if h.notifier != nil && result.Signal != nil {
		_ = h.notifier.NotifySupportSignalResponse(r.Context(), signalID, userID, result.Signal.UserID, result.ChatID)
	}
	response.Success(w, http.StatusOK, result)
}

func (h *Handler) ResolveSupportSignal(w http.ResponseWriter, r *http.Request) {
	h.updateSupportSignalStatus(w, r, "resolve")
}

func (h *Handler) CancelSupportSignal(w http.ResponseWriter, r *http.Request) {
	h.updateSupportSignalStatus(w, r, "cancel")
}

func (h *Handler) updateSupportSignalStatus(w http.ResponseWriter, r *http.Request, action string) {
	userID := middleware.CurrentUserID(r)
	signalID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid reach out signal id")
		return
	}
	var signal *SupportSignal
	if action == "resolve" {
		signal, err = h.db.ResolveSupportSignal(r.Context(), userID, signalID)
	} else {
		signal, err = h.db.CancelSupportSignal(r.Context(), userID, signalID)
	}
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "reach out signal not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not update reach out signal")
		return
	}
	response.Success(w, http.StatusOK, signal)
}

func normalizeCreateSupportSignalInput(input CreateSupportSignalInput) CreateSupportSignalInput {
	input.Reason = strings.TrimSpace(input.Reason)
	return input
}

func validateCreateSupportSignalInput(input CreateSupportSignalInput) map[string]string {
	errs := map[string]string{}
	if !validSupportSignalReasons[input.Reason] {
		errs["reason"] = "invalid"
	}
	return errs
}

// CreateSupportRequest creates a unified support request for the authenticated user.
func (h *Handler) CreateSupportRequest(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input CreateSupportRequestInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req, ok := h.createSupportRequest(w, r, userID, normalizeCreateSupportRequestInput(input))
	if !ok {
		return
	}

	response.Success(w, http.StatusCreated, req)
}

func (h *Handler) createSupportRequest(w http.ResponseWriter, r *http.Request, userID uuid.UUID, normalized CreateSupportRequestInput) (*SupportRequest, bool) {
	if errs := validateCreateSupportRequestInput(normalized); len(errs) > 0 {
		response.ValidationError(w, errs)
		return nil, false
	}
	if normalized.Message != nil {
		if err := h.moderator.CheckText(r.Context(), "support_request", *normalized.Message); err != nil {
			response.Error(w, http.StatusUnprocessableEntity, moderation.UserMessage(err))
			return nil, false
		}
	}

	openCount, err := h.db.CountOpenSupportRequests(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not validate support request")
		return nil, false
	}
	if openCount > 0 {
		response.Error(w, http.StatusConflict, "you already have an open support request")
		return nil, false
	}
	if normalized.Urgency == "high" {
		recentCount, err := h.db.CountHighUrgencySupportRequestsSince(r.Context(), userID, time.Now().UTC().Add(-30*time.Minute))
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "could not validate support request")
			return nil, false
		}
		if recentCount > 0 {
			response.Error(w, http.StatusTooManyRequests, "please wait before creating another high-urgency request")
			return nil, false
		}
		dailyCount, err := h.db.CountHighUrgencySupportRequestsSince(r.Context(), userID, time.Now().UTC().Add(-24*time.Hour))
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "could not validate support request")
			return nil, false
		}
		if dailyCount >= 3 {
			response.Error(w, http.StatusTooManyRequests, "you've used your high-urgency requests for today")
			return nil, false
		}
	}

	req, err := h.db.CreateSupportRequest(r.Context(), userID, normalized)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create support request")
		return nil, false
	}

	return req, true
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
	filter, ok := parseSupportRequestFilterQuery(r)
	if !ok {
		response.Error(w, http.StatusBadRequest, "invalid support request filter")
		return
	}
	cursor, err := parseSupportFeedCursor(strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support cursor")
		return
	}
	params := pagination.Parse(r, 20, 50)

	requests, err := h.db.ListVisibleSupportRequests(r.Context(), userID, filter, cursor, params.Limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support requests")
		return
	}

	page := supportFeedSlice(requests, params.Limit, cursor)
	response.Success(w, http.StatusOK, SupportRequestsPage{
		Items:      page.Items,
		Limit:      page.Limit,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
	})
}

func parseSupportRequestFilterQuery(r *http.Request) (SupportRequestFilter, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("filter"))
	if raw == "" {
		// Older clients sent a channel instead of a feed filter. Channels now map
		// to the same unified feed, so the compatibility default remains "all".
		return SupportRequestFilterAll, true
	}
	filter := SupportRequestFilter(raw)
	return filter, validSupportRequestFilters[filter]
}

func parseSupportFeedCursor(raw string) (*SupportFeedCursor, error) {
	if raw == "" {
		return nil, nil
	}

	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}

	var cursor SupportFeedCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return nil, err
	}
	return &cursor, nil
}

func encodeSupportFeedCursor(cursor SupportFeedCursor) (*string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return nil, err
	}

	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return &encoded, nil
}

func supportFeedSlice(items []SupportRequest, limit int, previous *SupportFeedCursor) pagination.CursorResponse[SupportRequest] {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		servedAt := time.Now().UTC()
		if previous != nil && !previous.ServedAt.IsZero() {
			servedAt = previous.ServedAt
		}
		cursor, err := encodeSupportFeedCursor(SupportFeedCursor{
			Score:     last.FeedScore,
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
			ServedAt:  servedAt,
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

// CreateSupportOffer records one user's private offer to help on an open support request.
func (h *Handler) CreateSupportOffer(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	requestID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support request id")
		return
	}

	var input createSupportOfferInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input = normalizeCreateSupportOfferInput(input)
	if errs := validateCreateSupportOfferInput(input); len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}
	if input.Message != nil {
		if err := h.moderator.CheckText(r.Context(), "support_offer", *input.Message); err != nil {
			response.Error(w, http.StatusUnprocessableEntity, moderation.UserMessage(err))
			return
		}
	}

	scheduledFor, err := parseSupportOfferScheduledFor(input.ScheduledFor)
	if err != nil {
		response.ValidationError(w, map[string]string{"scheduled_for": "invalid"})
		return
	}

	req, err := h.db.GetSupportRequest(r.Context(), userID, requestID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "support request not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not fetch support request")
		return
	}
	if req.RequesterID == userID {
		response.Error(w, http.StatusBadRequest, "cannot respond to your own request")
		return
	}
	if req.Status != "open" {
		response.Error(w, http.StatusConflict, "support request is no longer open")
		return
	}
	if req.AlreadyChatting {
		response.Error(w, http.StatusConflict, "you are already chatting with this user")
		return
	}

	res, err := h.db.CreateSupportOffer(r.Context(), requestID, userID, input.OfferType, input.Message, scheduledFor)
	if err != nil {
		response.Error(w, http.StatusConflict, "could not create support offer")
		return
	}
	if h.notifier != nil && res != nil && res.Offer != nil {
		_ = h.notifier.NotifySupportOffer(r.Context(), requestID, res.Offer.ID, userID, req.RequesterID)
	}

	response.Success(w, http.StatusCreated, res)
}

// AcceptSupportOffer lets the requester choose one private offer and only then open the support chat.
func (h *Handler) AcceptSupportOffer(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	requestID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support request id")
		return
	}
	offerID, err := uuid.Parse(chi.URLParam(r, "offerId"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support offer id")
		return
	}

	req, err := h.db.AcceptSupportOffer(r.Context(), userID, requestID, offerID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "support offer not found")
			return
		}
		if errors.Is(err, ErrConflict) {
			response.Error(w, http.StatusConflict, "support offer is no longer available")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not accept support offer")
		return
	}

	response.Success(w, http.StatusOK, AcceptSupportOfferResult{Request: req})
}

// ListSupportOffers returns a paginated list of private offers for a support request owned by the caller.
func (h *Handler) ListSupportOffers(w http.ResponseWriter, r *http.Request) {
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
		response.Error(w, http.StatusForbidden, "cannot view support offers")
		return
	}

	params := pagination.Parse(r, 50, 100)
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && status != "pending" && status != "accepted" {
		response.Error(w, http.StatusBadRequest, "invalid support offer status")
		return
	}

	offers, err := h.db.ListSupportOffers(r.Context(), requestID, status, params.Limit+1, params.Offset)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support offers")
		return
	}

	response.Success(w, http.StatusOK, pagination.Slice(offers, params))
}

// DeclineSupportOffer lets the requester pass on one pending private offer.
func (h *Handler) DeclineSupportOffer(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	requestID, offerID, ok := supportRequestAndOfferIDs(w, r)
	if !ok {
		return
	}
	if err := h.db.DeclineSupportOffer(r.Context(), userID, requestID, offerID); err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "support offer not found")
			return
		}
		response.Error(w, http.StatusConflict, "could not decline support offer")
		return
	}
	response.Success(w, http.StatusOK, map[string]string{"status": "declined"})
}

// CancelSupportOffer lets a helper cancel their own pending private offer.
func (h *Handler) CancelSupportOffer(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	requestID, offerID, ok := supportRequestAndOfferIDs(w, r)
	if !ok {
		return
	}
	if err := h.db.CancelSupportOffer(r.Context(), userID, requestID, offerID); err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "support offer not found")
			return
		}
		response.Error(w, http.StatusConflict, "could not cancel support offer")
		return
	}
	response.Success(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func supportRequestAndOfferIDs(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	requestID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support request id")
		return uuid.Nil, uuid.Nil, false
	}
	offerID, err := uuid.Parse(chi.URLParam(r, "offerId"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support offer id")
		return uuid.Nil, uuid.Nil, false
	}
	return requestID, offerID, true
}

// CreateSupportReply adds a public reply to a support request thread.
func (h *Handler) CreateSupportReply(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	requestID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support request id")
		return
	}
	var input struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body := strings.TrimSpace(input.Body)
	if body == "" || len(body) > 1000 {
		response.ValidationError(w, map[string]string{"body": "must be between 1 and 1000 characters"})
		return
	}
	if err := h.moderator.CheckText(r.Context(), "support_reply", body); err != nil {
		response.Error(w, http.StatusUnprocessableEntity, moderation.UserMessage(err))
		return
	}
	_, status, err := h.db.GetSupportRequestState(r.Context(), requestID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "support request not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not fetch support request")
		return
	}
	if status != "open" && status != "active" {
		response.Error(w, http.StatusConflict, "support request is closed")
		return
	}
	reply, err := h.db.CreateSupportReply(r.Context(), requestID, userID, body)
	if err != nil {
		response.Error(w, http.StatusConflict, "could not create support reply")
		return
	}
	response.Success(w, http.StatusCreated, reply)
}

// ListSupportReplies returns public replies in a support request thread.
func (h *Handler) ListSupportReplies(w http.ResponseWriter, r *http.Request) {
	requestID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support request id")
		return
	}
	cursor, err := parseSupportReplyCursor(strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid support reply cursor")
		return
	}
	params := pagination.Parse(r, 20, 100)
	replies, err := h.db.ListSupportReplies(r.Context(), requestID, cursor, params.Limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch support replies")
		return
	}
	page := supportReplySlice(replies, params.Limit)
	response.Success(w, http.StatusOK, page)
}

func parseSupportReplyCursor(raw string) (*SupportReplyCursor, error) {
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	var cursor SupportReplyCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return nil, err
	}
	return &cursor, nil
}

func encodeSupportReplyCursor(cursor SupportReplyCursor) (*string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return nil, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return &encoded, nil
}

func supportReplySlice(items []SupportReply, limit int) pagination.CursorResponse[SupportReply] {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		cursor, err := encodeSupportReplyCursor(SupportReplyCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err == nil {
			nextCursor = cursor
		}
	}
	return pagination.CursorResponse[SupportReply]{
		Items:      items,
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}
}
