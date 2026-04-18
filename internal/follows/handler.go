package follows

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/response"
)

type Handler struct {
	db *pgxpool.Pool
}

func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{db: db}
}

type followUser struct {
	UserID    uuid.UUID `json:"user_id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url"`
	City      *string   `json:"city"`
	CreatedAt time.Time `json:"created_at"`
}

// POST /users/{id}/follow
func (h *Handler) Follow(w http.ResponseWriter, r *http.Request) {
	followerID := middleware.CurrentUserID(r)
	followingID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}

	if followerID == followingID {
		response.Error(w, http.StatusBadRequest, "cannot follow yourself")
		return
	}

	_, err = h.db.Exec(r.Context(),
		`INSERT INTO follows (
			follower_id,
			following_id
		)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`,
		followerID, followingID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not follow user")
		return
	}

	// Following is idempotent: duplicate requests still report success and leave
	// the relationship in the desired state.
	response.Success(w, http.StatusCreated, map[string]bool{"following": true})
}

// DELETE /users/{id}/follow
func (h *Handler) Unfollow(w http.ResponseWriter, r *http.Request) {
	followerID := middleware.CurrentUserID(r)
	followingID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}

	result, err := h.db.Exec(r.Context(),
		`DELETE FROM follows
		WHERE follower_id = $1
			AND following_id = $2`,
		followerID, followingID,
	)
	if err != nil || result.RowsAffected() == 0 {
		response.Error(w, http.StatusNotFound, "follow not found")
		return
	}

	response.Success(w, http.StatusOK, map[string]bool{"following": false})
}

// GET /users/me/following
func (h *Handler) ListFollowing(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	// The response is ordered by follow creation time so clients can show the
	// newest follow relationships first without additional sorting.
	rows, err := h.db.Query(r.Context(),
		`SELECT
			u.id,
			u.username,
			u.avatar_url,
			u.city,
			f.created_at
		FROM follows f
		JOIN users u ON u.id = f.following_id
		WHERE f.follower_id = $1
		ORDER BY f.created_at DESC`,
		userID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch following")
		return
	}
	defer rows.Close()

	var users []followUser
	for rows.Next() {
		var u followUser
		if err := rows.Scan(&u.UserID, &u.Username, &u.AvatarURL, &u.City, &u.CreatedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not read following")
			return
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read following")
		return
	}

	response.Success(w, http.StatusOK, users)
}

// GET /users/me/followers
func (h *Handler) ListFollowers(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	// Followers and following share the same payload shape to simplify client
	// rendering for both lists.
	rows, err := h.db.Query(r.Context(),
		`SELECT
			u.id,
			u.username,
			u.avatar_url,
			u.city,
			f.created_at
		FROM follows f
		JOIN users u ON u.id = f.follower_id
		WHERE f.following_id = $1
		ORDER BY f.created_at DESC`,
		userID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch followers")
		return
	}
	defer rows.Close()

	var users []followUser
	for rows.Next() {
		var u followUser
		if err := rows.Scan(&u.UserID, &u.Username, &u.AvatarURL, &u.City, &u.CreatedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not read followers")
			return
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read followers")
		return
	}

	response.Success(w, http.StatusOK, users)
}
