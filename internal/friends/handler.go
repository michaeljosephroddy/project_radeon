package friends

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/pagination"
	"github.com/project_radeon/api/pkg/response"
)

type Handler struct {
	db *pgxpool.Pool
}

type friendUser struct {
	UserID    uuid.UUID `json:"user_id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url"`
	City      *string   `json:"city"`
	CreatedAt time.Time `json:"created_at"`
}

// NewHandler builds a friends handler backed by the shared database pool.
func NewHandler(db *pgxpool.Pool) *Handler {
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

	var status string
	var requesterID uuid.UUID
	err = h.db.QueryRow(r.Context(),
		`SELECT status, requester_id
		FROM friendships
		WHERE user_a_id = $1
			AND user_b_id = $2`,
		userAID, userBID,
	).Scan(&status, &requesterID)
	if err == nil {
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
	if !errors.Is(err, pgx.ErrNoRows) {
		response.Error(w, http.StatusInternalServerError, "could not inspect friendship state")
		return
	}

	_, err = h.db.Exec(r.Context(),
		`INSERT INTO friendships (
			user_a_id,
			user_b_id,
			requester_id,
			status
		)
		VALUES ($1, $2, $3, 'pending')`,
		userAID, userBID, userID,
	)
	if err != nil {
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
		result, err := h.db.Exec(r.Context(),
			`UPDATE friendships
			SET
				status = 'accepted',
				accepted_at = NOW()
			WHERE user_a_id = $1
				AND user_b_id = $2
				AND requester_id = $3
				AND status = 'pending'`,
			userAID, userBID, otherUserID,
		)
		if err != nil || result.RowsAffected() == 0 {
			response.Error(w, http.StatusNotFound, "friend request not found")
			return
		}

		response.Success(w, http.StatusOK, map[string]any{
			"status":    "accepted",
			"is_friend": true,
		})
		return
	}

	result, err := h.db.Exec(r.Context(),
		`DELETE FROM friendships
		WHERE user_a_id = $1
			AND user_b_id = $2
			AND requester_id = $3
			AND status = 'pending'`,
		userAID, userBID, otherUserID,
	)
	if err != nil || result.RowsAffected() == 0 {
		response.Error(w, http.StatusNotFound, "friend request not found")
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
	result, err := h.db.Exec(r.Context(),
		`DELETE FROM friendships
		WHERE user_a_id = $1
			AND user_b_id = $2
			AND requester_id = $3
			AND status = 'pending'`,
		userAID, userBID, userID,
	)
	if err != nil || result.RowsAffected() == 0 {
		response.Error(w, http.StatusNotFound, "friend request not found")
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
	result, err := h.db.Exec(r.Context(),
		`DELETE FROM friendships
		WHERE user_a_id = $1
			AND user_b_id = $2
			AND status = 'accepted'`,
		userAID, userBID,
	)
	if err != nil || result.RowsAffected() == 0 {
		response.Error(w, http.StatusNotFound, "friend not found")
		return
	}

	response.Success(w, http.StatusOK, map[string]any{
		"status":    "none",
		"is_friend": false,
	})
}

// ListFriends returns the caller's accepted friends ordered by acceptance time.
// Paginate with ?before=<next_cursor> from the previous response.
func (h *Handler) ListFriends(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.ParseCursor(r, 25, 100)

	users, err := h.queryFriendUsers(r.Context(),
		`SELECT
			u.id,
			u.username,
			u.avatar_url,
			u.city,
			COALESCE(f.accepted_at, f.created_at) AS created_at
		FROM friendships f
		JOIN users u ON u.id = CASE
			WHEN f.user_a_id = $1 THEN f.user_b_id
			ELSE f.user_a_id
		END
		WHERE (f.user_a_id = $1 OR f.user_b_id = $1)
			AND f.status = 'accepted'
			AND ($2::timestamptz IS NULL OR COALESCE(f.accepted_at, f.created_at) < $2)
		ORDER BY COALESCE(f.accepted_at, f.created_at) DESC
		LIMIT $3`,
		userID, params.Before, params.Limit+1,
	)
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

	requesterFilter := "AND f.requester_id != $1"
	if outgoing {
		requesterFilter = "AND f.requester_id = $1"
	}

	query := `SELECT
		u.id,
		u.username,
		u.avatar_url,
		u.city,
		f.created_at
	FROM friendships f
	JOIN users u ON u.id = CASE
		WHEN f.user_a_id = $1 THEN f.user_b_id
		ELSE f.user_a_id
	END
	WHERE (f.user_a_id = $1 OR f.user_b_id = $1)
		AND f.status = 'pending'
		` + requesterFilter + `
		AND ($2::timestamptz IS NULL OR f.created_at < $2)
	ORDER BY f.created_at DESC
	LIMIT $3`

	users, err := h.queryFriendUsers(r.Context(), query, userID, params.Before, params.Limit+1)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch friend requests")
		return
	}

	response.Success(w, http.StatusOK, pagination.CursorSlice(users, params.Limit, func(u friendUser) time.Time { return u.CreatedAt }))
}

// queryFriendUsers keeps the paging scan logic in one place for accepted
// friends and pending-request lists, which all share the same row shape.
func (h *Handler) queryFriendUsers(ctx context.Context, query string, args ...any) ([]friendUser, error) {
	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []friendUser
	for rows.Next() {
		var u friendUser
		if err := rows.Scan(&u.UserID, &u.Username, &u.AvatarURL, &u.City, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}
