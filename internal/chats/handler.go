package chats

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/pagination"
	"github.com/project_radeon/api/pkg/response"
)

// Querier is the database interface required by the chats handler.
type Querier interface {
	ListChats(ctx context.Context, userID uuid.UUID, query string, limit, offset int) ([]Chat, error)
	ListChatRequests(ctx context.Context, userID uuid.UUID) ([]Chat, error)
	GetChat(ctx context.Context, userID, chatID uuid.UUID) (*Chat, error)
	GetChatStatus(ctx context.Context, chatID uuid.UUID) (string, error)
	FindDirectChat(ctx context.Context, userID, otherUserID uuid.UUID) (uuid.UUID, bool, error)
	CreateChat(ctx context.Context, userID uuid.UUID, isGroup bool, name *string, memberIDs []uuid.UUID) (uuid.UUID, error)
	IsAddresseeOfChat(ctx context.Context, chatID, userID uuid.UUID) (bool, error)
	AcceptChatRequest(ctx context.Context, chatID uuid.UUID) error
	DeclineChatRequest(ctx context.Context, chatID uuid.UUID) error
	IsMemberOfChat(ctx context.Context, chatID, userID uuid.UUID) (bool, error)
	ListMessages(ctx context.Context, chatID, userID uuid.UUID, before *time.Time, limit int) ([]Message, *uuid.UUID, error)
	InsertMessage(ctx context.Context, chatID, userID uuid.UUID, body string) (uuid.UUID, error)
	DeleteOrLeaveChat(ctx context.Context, chatID, userID uuid.UUID) (string, error)
}

type Notifier interface {
	NotifyChatMessage(ctx context.Context, chatID, messageID, senderID uuid.UUID, body string) error
	MarkChatRead(ctx context.Context, chatID, userID uuid.UUID, lastReadMessageID *uuid.UUID, readAt time.Time) error
}

type Handler struct {
	db       Querier
	notifier Notifier
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

type Chat struct {
	ID             uuid.UUID           `json:"id"`
	IsGroup        bool                `json:"is_group"`
	Name           *string             `json:"name"`
	Username       *string             `json:"username"`
	AvatarURL      *string             `json:"avatar_url"`
	CreatedAt      time.Time           `json:"created_at"`
	LastMessage    *string             `json:"last_message"`
	LastMessageAt  *time.Time          `json:"last_message_at"`
	UnreadCount    int                 `json:"unread_count"`
	Status         string              `json:"status"`
	SupportContext *SupportChatContext `json:"support_context,omitempty"`
}

type Message struct {
	ID        uuid.UUID `json:"id"`
	SenderID  uuid.UUID `json:"sender_id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url"`
	Body      string    `json:"body"`
	SentAt    time.Time `json:"sent_at"`
}

type MessagePage struct {
	Items                      []Message  `json:"items"`
	Limit                      int        `json:"limit"`
	HasMore                    bool       `json:"has_more"`
	NextBefore                 *time.Time `json:"next_before,omitempty"`
	OtherUserLastReadMessageID *uuid.UUID `json:"other_user_last_read_message_id,omitempty"`
}

// NewHandler builds a chats handler. Pass chats.NewPgStore(pool) for production.
func NewHandler(db Querier) *Handler {
	return &Handler{db: db}
}

func NewHandlerWithNotifier(db Querier, notifier Notifier) *Handler {
	return &Handler{db: db, notifier: notifier}
}

// ListChats returns one page of the caller's active chats.
func (h *Handler) ListChats(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.Parse(r, 20, 50)
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	chats, err := h.db.ListChats(r.Context(), userID, query, params.Limit+1, params.Offset)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch chats")
		return
	}

	response.Success(w, http.StatusOK, pagination.Slice(chats, params))
}

// ListChatRequests returns pending direct-message requests addressed to the current user.
func (h *Handler) ListChatRequests(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	chats, err := h.db.ListChatRequests(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch chat requests")
		return
	}

	response.Success(w, http.StatusOK, chats)
}

// GetChat returns a single chat summary for the current member.
func (h *Handler) GetChat(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	chatID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid chat id")
		return
	}

	chat, err := h.db.GetChat(r.Context(), userID, chatID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "chat not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not fetch chat")
		return
	}

	response.Success(w, http.StatusOK, chat)
}

// CreateChat creates a new direct or group chat unless an equivalent direct chat already exists.
func (h *Handler) CreateChat(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		MemberIDs []uuid.UUID `json:"member_ids"`
		Name      *string     `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	isGroup := len(input.MemberIDs) > 1

	if !isGroup && len(input.MemberIDs) == 1 {
		existingID, found, err := h.db.FindDirectChat(r.Context(), userID, input.MemberIDs[0])
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "could not create chat")
			return
		}
		if found {
			response.Success(w, http.StatusOK, map[string]any{"id": existingID, "is_group": false})
			return
		}
	}

	chatID, err := h.db.CreateChat(r.Context(), userID, isGroup, input.Name, input.MemberIDs)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create chat")
		return
	}

	response.Success(w, http.StatusCreated, map[string]any{"id": chatID, "is_group": isGroup})
}

// UpdateChatStatus lets an addressee accept or decline a pending chat request.
func (h *Handler) UpdateChatStatus(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	chatID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid chat id")
		return
	}

	var input struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.Status != "active" && input.Status != "declined" {
		response.Error(w, http.StatusBadRequest, "status must be 'active' or 'declined'")
		return
	}

	isAddressee, err := h.db.IsAddresseeOfChat(r.Context(), chatID, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not check chat membership")
		return
	}
	if !isAddressee {
		response.Error(w, http.StatusForbidden, "not authorised")
		return
	}

	if input.Status == "declined" {
		if err := h.db.DeclineChatRequest(r.Context(), chatID); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not update chat")
			return
		}
	} else {
		if err := h.db.AcceptChatRequest(r.Context(), chatID); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not update chat")
			return
		}
	}

	chat, err := h.db.GetChat(r.Context(), userID, chatID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch chat")
		return
	}

	response.Success(w, http.StatusOK, chat)
}

// GetMessages pages backwards through a chat transcript using an optional "before" cursor.
func (h *Handler) GetMessages(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	chatID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid chat id")
		return
	}

	isMember, err := h.db.IsMemberOfChat(r.Context(), chatID, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not check chat membership")
		return
	}
	if !isMember {
		response.Error(w, http.StatusForbidden, "not a member of this chat")
		return
	}

	limit := 50
	if parsed := pagination.Parse(r, 50, 100); parsed.Limit > 0 {
		limit = parsed.Limit
	}

	var before *time.Time
	if beforeRaw := strings.TrimSpace(r.URL.Query().Get("before")); beforeRaw != "" {
		parsed, parseErr := time.Parse(time.RFC3339, beforeRaw)
		if parseErr != nil {
			response.Error(w, http.StatusBadRequest, "before must be an RFC3339 timestamp")
			return
		}
		before = &parsed
	}

	msgs, otherUserLastReadMessageID, err := h.db.ListMessages(r.Context(), chatID, userID, before, limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch messages")
		return
	}

	hasMore := len(msgs) > limit
	if hasMore {
		msgs = msgs[:limit]
	}
	for left, right := 0, len(msgs)-1; left < right; left, right = left+1, right-1 {
		msgs[left], msgs[right] = msgs[right], msgs[left]
	}

	var nextBefore *time.Time
	if hasMore && len(msgs) > 0 {
		oldest := msgs[0].SentAt
		nextBefore = &oldest
	}

	response.Success(w, http.StatusOK, MessagePage{
		Items:                      msgs,
		Limit:                      limit,
		HasMore:                    hasMore,
		NextBefore:                 nextBefore,
		OtherUserLastReadMessageID: otherUserLastReadMessageID,
	})
}

// SendMessage appends a new text message to a chat for an authorised member.
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	chatID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid chat id")
		return
	}

	isMember, err := h.db.IsMemberOfChat(r.Context(), chatID, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not check chat membership")
		return
	}
	if !isMember {
		response.Error(w, http.StatusForbidden, "not a member of this chat")
		return
	}

	status, err := h.db.GetChatStatus(r.Context(), chatID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch chat")
		return
	}
	if status != "active" {
		response.Error(w, http.StatusConflict, "chat is not open for messaging")
		return
	}

	var input struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "body is required")
		return
	}

	input.Body = strings.TrimSpace(input.Body)
	if input.Body == "" {
		response.Error(w, http.StatusBadRequest, "body is required")
		return
	}

	msgID, err := h.db.InsertMessage(r.Context(), chatID, userID, input.Body)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not send message")
		return
	}

	if h.notifier != nil {
		// Message delivery is the primary action; notification failures should not
		// turn a successful send into a retried duplicate from the client.
		_ = h.notifier.NotifyChatMessage(r.Context(), chatID, msgID, userID, input.Body)
		_ = h.notifier.MarkChatRead(r.Context(), chatID, userID, &msgID, time.Now().UTC())
	}

	response.Success(w, http.StatusCreated, map[string]any{"id": msgID})
}

// MarkRead records that the caller has caught up with the current thread.
func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	if h.notifier == nil {
		response.Success(w, http.StatusOK, map[string]bool{"read": true})
		return
	}

	userID := middleware.CurrentUserID(r)
	chatID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid chat id")
		return
	}

	isMember, err := h.db.IsMemberOfChat(r.Context(), chatID, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not check chat membership")
		return
	}
	if !isMember {
		response.Error(w, http.StatusForbidden, "not a member of this chat")
		return
	}

	var input struct {
		LastReadMessageID *uuid.UUID `json:"last_read_message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.notifier.MarkChatRead(r.Context(), chatID, userID, input.LastReadMessageID, time.Now().UTC()); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update read state")
		return
	}
	response.Success(w, http.StatusOK, map[string]bool{"read": true})
}

// DeleteChat deletes a direct chat or removes the caller from a group chat.
func (h *Handler) DeleteChat(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	chatID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid chat id")
		return
	}

	action, err := h.db.DeleteOrLeaveChat(r.Context(), chatID, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "chat not found")
			return
		}
		if errors.Is(err, ErrForbidden) {
			response.Error(w, http.StatusForbidden, "not a member of this chat")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not update chat")
		return
	}

	response.Success(w, http.StatusOK, map[string]string{"action": action})
}
