package friends

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

// Querier is the database interface required by the friends handler.
// Defined here so tests can substitute a mock without a real Postgres connection.
type Querier interface {
	GetFriendshipState(ctx context.Context, userAID, userBID uuid.UUID) (found bool, status string, requesterID uuid.UUID, err error)
	InsertFriendship(ctx context.Context, userAID, userBID, requesterID uuid.UUID) error
	AcceptFriendRequest(ctx context.Context, userAID, userBID, userID, otherUserID uuid.UUID) error
	DeletePendingFriendship(ctx context.Context, userAID, userBID, requesterID uuid.UUID) error
	RemoveFriend(ctx context.Context, userAID, userBID, userID, otherUserID uuid.UUID) error
	ListFriendUsers(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]friendUser, error)
	ListPendingRequests(ctx context.Context, userID uuid.UUID, outgoing bool, before *time.Time, limit int) ([]friendUser, error)
}

type Handler struct {
	db Querier
}

type friendUser struct {
	UserID    uuid.UUID `json:"user_id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url"`
	City      *string   `json:"city"`
	CreatedAt time.Time `json:"created_at"`
}

// NewHandler builds a friends handler. Pass friends.NewPgStore(pool) for production.
func NewHandler(db Querier) *Handler {
	return &Handler{db: db}
}

func sortPair(a, b uuid.UUID) (uuid.UUID, uuid.UUID) {
	if a.String() < b.String() {
		return a, b
	}
	return b, a
}

// SendRequest creates a new pending friend request from the current user.
func (h *Handler) SendRequest(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	otherUserID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if userID == otherUserID {
		response.Error(w, http.StatusBadRequest, "cannot friend yourself")
		return
	}

	userAID, userBID := sortPair(userID, otherUserID)

	found, status, requesterID, err := h.db.GetFriendshipState(r.Context(), userAID, userBID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not inspect friendship state")
		return
	}
	if found {
		switch {
		case status == "accepted":
			response.Success(w, http.StatusOK, map[string]any{
				"status":    "accepted",
				"is_friend": true,
			})
		case requesterID == userID:
			response.Success(w, http.StatusOK, map[string]any{
				"status":    "pending_outgoing",
				"is_friend": false,
			})
		default:
			response.Error(w, http.StatusConflict, "friend request already pending from this user")
		}
		return
	}

	if err := h.db.InsertFriendship(r.Context(), userAID, userBID, userID); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not send friend request")
		return
	}

	response.Success(w, http.StatusCreated, map[string]any{
		"status":    "pending_outgoing",
		"is_friend": false,
	})
}

// UpdateRequest accepts or declines an incoming friend request.
func (h *Handler) UpdateRequest(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	otherUserID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var input struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.Action != "accept" && input.Action != "decline" {
		response.Error(w, http.StatusBadRequest, "action must be accept or decline")
		return
	}

	userAID, userBID := sortPair(userID, otherUserID)

	if input.Action == "accept" {
		if err := h.db.AcceptFriendRequest(r.Context(), userAID, userBID, userID, otherUserID); err != nil {
			if errors.Is(err, ErrNotFound) {
				response.Error(w, http.StatusNotFound, "friend request not found")
				return
			}
			response.Error(w, http.StatusInternalServerError, "could not accept friend request")
			return
		}
		response.Success(w, http.StatusOK, map[string]any{
			"status":    "accepted",
			"is_friend": true,
		})
		return
	}

	if err := h.db.DeletePendingFriendship(r.Context(), userAID, userBID, otherUserID); err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "friend request not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not decline friend request")
		return
	}

	response.Success(w, http.StatusOK, map[string]any{
		"status":    "none",
		"is_friend": false,
	})
}

// CancelRequest removes an outgoing pending friend request.
func (h *Handler) CancelRequest(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	otherUserID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}

	userAID, userBID := sortPair(userID, otherUserID)
	if err := h.db.DeletePendingFriendship(r.Context(), userAID, userBID, userID); err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "friend request not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not cancel friend request")
		return
	}

	response.Success(w, http.StatusOK, map[string]any{
		"status":    "none",
		"is_friend": false,
	})
}

// RemoveFriend deletes an accepted friendship between the current user and another user.
func (h *Handler) RemoveFriend(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	otherUserID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}

	userAID, userBID := sortPair(userID, otherUserID)
	if err := h.db.RemoveFriend(r.Context(), userAID, userBID, userID, otherUserID); err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "friend not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not remove friend")
		return
	}

	response.Success(w, http.StatusOK, map[string]any{
		"status":    "none",
		"is_friend": false,
	})
}

// ListFriends returns the caller's accepted friends ordered by acceptance time.
func (h *Handler) ListFriends(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 25, 100)

	users, err := h.db.ListFriendUsers(r.Context(), userID, params.Before, params.Limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch friends")
		return
	}

	response.Success(w, http.StatusOK, pagination.CursorSlice(users, params.Limit, func(u friendUser) time.Time { return u.CreatedAt }))
}

// ListIncomingRequests returns pending requests addressed to the caller.
func (h *Handler) ListIncomingRequests(w http.ResponseWriter, r *http.Request) {
	h.listPendingRequests(w, r, false)
}

// ListOutgoingRequests returns pending requests sent by the caller.
func (h *Handler) ListOutgoingRequests(w http.ResponseWriter, r *http.Request) {
	h.listPendingRequests(w, r, true)
}

func (h *Handler) listPendingRequests(w http.ResponseWriter, r *http.Request, outgoing bool) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 25, 100)

	users, err := h.db.ListPendingRequests(r.Context(), userID, outgoing, params.Before, params.Limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch friend requests")
		return
	}

	response.Success(w, http.StatusOK, pagination.CursorSlice(users, params.Limit, func(u friendUser) time.Time { return u.CreatedAt }))
}
