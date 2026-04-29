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
	ListSupportOffers(ctx context.Context, requestID uuid.UUID, limit, offset int) ([]SupportOffer, error)
	CreateSupportReply(ctx context.Context, requestID, authorID uuid.UUID, body string) (*SupportReply, error)
	ListSupportReplies(ctx context.Context, requestID uuid.UUID, cursor *SupportReplyCursor, limit int) ([]SupportReply, error)
	DeclineSupportOffer(ctx context.Context, requesterID, requestID, offerID uuid.UUID) error
	CancelSupportOffer(ctx context.Context, responderID, requestID, offerID uuid.UUID) error
}

type Handler struct {
	db              Querier
	chatBroadcaster ChatBroadcaster
}

type ChatBroadcaster interface {
	BroadcastChatUpdate(ctx context.Context, chatID uuid.UUID) error
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
	"anxiety":      true,
	"relapse_risk": true,
	"loneliness":   true,
	"cravings":     true,
	"depression":   true,
	"family":       true,
	"work":         true,
	"sleep":        true,
	"celebration":  true,
	"general":      true,
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
	return &Handler{db: db}
}

func NewHandlerWithChatBroadcaster(db Querier, chatBroadcaster ChatBroadcaster) *Handler {
	return &Handler{db: db, chatBroadcaster: chatBroadcaster}
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
	CreatedAt           time.Time        `json:"created_at"`
	PrivacyLevel        string           `json:"privacy_level,omitempty"`
	AcceptedResponseID  *uuid.UUID       `json:"-"`
	AcceptedResponderID *uuid.UUID       `json:"accepted_responder_id,omitempty"`
	AcceptedAt          *time.Time       `json:"accepted_at,omitempty"`
	ClosedAt            *time.Time       `json:"closed_at,omitempty"`
	ResponderID         *uuid.UUID       `json:"responder_id,omitempty"`
	ResponderUsername   *string          `json:"responder_username,omitempty"`
	ResponderAvatarURL  *string          `json:"responder_avatar_url,omitempty"`
	ChatID              *uuid.UUID       `json:"chat_id,omitempty"`
	HasResponded        bool             `json:"-"`
	HasOffered          bool             `json:"has_offered"`
	HasReplied          bool             `json:"has_replied"`
	IsOwnRequest        bool             `json:"is_own_request"`
	SortAt              time.Time        `json:"-"`
	AttentionBucket     int              `json:"-"`
	UrgencyRank         int              `json:"-"`
	FeedScore           float64          `json:"-"`
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

type SupportChatContext struct {
	SupportRequestID  uuid.UUID  `json:"support_request_id"`
	RequestType       string     `json:"request_type"`
	RequestMessage    *string    `json:"request_message,omitempty"`
	RequesterID       uuid.UUID  `json:"requester_id"`
	RequesterUsername string     `json:"requester_username"`
	LatestOfferType   *string    `json:"latest_offer_type,omitempty"`
	Status            string     `json:"status"`
	AwaitingUserID    *uuid.UUID `json:"awaiting_user_id,omitempty"`
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

type CreateSupportOfferResult struct {
	Offer *SupportOffer `json:"offer"`
	Chat  *ChatSummary  `json:"chat,omitempty"`
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

	scheduledFor, err := parseSupportOfferScheduledFor(input.ScheduledFor)
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

	res, err := h.db.CreateSupportOffer(r.Context(), requestID, userID, input.OfferType, input.Message, scheduledFor)
	if err != nil {
		response.Error(w, http.StatusConflict, "could not create support offer")
		return
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

	offers, err := h.db.ListSupportOffers(r.Context(), requestID, params.Limit+1, params.Offset)
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
