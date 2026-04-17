package likes

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/response"
)

type CacheInvalidator interface {
	InvalidateSuggestions(userID uuid.UUID)
}

type Handler struct {
	db    *pgxpool.Pool
	cache CacheInvalidator
}

func NewHandler(db *pgxpool.Pool, cache CacheInvalidator) *Handler {
	return &Handler{db: db, cache: cache}
}

// GET /users/me/likes — users who have liked the current user's profile
//
// Returns basic profile info only. The frontend is responsible for blurring
// avatars until the current user likes them back (forming a match).
func (h *Handler) GetMyLikes(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	rows, err := h.db.Query(r.Context(),
		`SELECT u.id, u.first_name, u.last_name, u.avatar_url, u.avatar_url_blurred, u.city, l.created_at
		 FROM likes l
		 JOIN users u ON u.id = l.liker_id
		 WHERE l.liked_id = $1
		 ORDER BY l.created_at DESC`,
		userID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch likes")
		return
	}
	defer rows.Close()

	type Liker struct {
		ID               uuid.UUID `json:"id"`
		FirstName        string    `json:"first_name"`
		LastName         string    `json:"last_name"`
		AvatarURL        *string   `json:"avatar_url"`
		AvatarURLBlurred *string   `json:"avatar_url_blurred"`
		City             *string   `json:"city"`
		LikedAt          time.Time `json:"liked_at"`
	}

	var likers []Liker
	for rows.Next() {
		var l Liker
		if err := rows.Scan(&l.ID, &l.FirstName, &l.LastName, &l.AvatarURL, &l.AvatarURLBlurred, &l.City, &l.LikedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not read likes")
			return
		}
		likers = append(likers, l)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch likes")
		return
	}

	response.Success(w, http.StatusOK, likers)
}

// POST /users/{id}/like
//
// Records a like from the current user toward the target. If the target has
// already liked the current user (mutual like) and no connection exists yet,
// a MATCH connection is created immediately with status='accepted'.
func (h *Handler) LikeUser(w http.ResponseWriter, r *http.Request) {
	log.Println("LikeUser hit")
	likerID := middleware.CurrentUserID(r)

	likedID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}

	if likerID == likedID {
		response.Error(w, http.StatusBadRequest, "cannot like yourself")
		return
	}

	// Record the like. Idempotent — liking the same person twice is a no-op.
	if _, err := h.db.Exec(r.Context(),
		`INSERT INTO likes (liker_id, liked_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		likerID, likedID,
	); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not record like")
		return
	}
	h.cache.InvalidateSuggestions(likerID)

	// Check whether the target has already liked us back.
	var isMutual bool
	if err := h.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM likes WHERE liker_id = $1 AND liked_id = $2)`,
		likedID, likerID,
	).Scan(&isMutual); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not check mutual like")
		return
	}

	if !isMutual {
		response.Success(w, http.StatusOK, map[string]any{"matched": false})
		return
	}

	// Mutual like — create a MATCH connection if none exists.
	// ON CONFLICT DO NOTHING handles the race where both users like simultaneously.
	var connID *uuid.UUID
	err = h.db.QueryRow(r.Context(),
		`INSERT INTO connections (requester_id, addressee_id, status, type)
		 VALUES ($1, $2, 'accepted', 'MATCH')
		 ON CONFLICT (requester_id, addressee_id) DO NOTHING
		 RETURNING id`,
		likerID, likedID,
	).Scan(&connID)

	// pgx returns ErrNoRows when ON CONFLICT DO NOTHING suppresses the insert
	// (RETURNING produces no row). That means a connection already existed — not an error.
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		response.Error(w, http.StatusInternalServerError, "could not create match")
		return
	}

	created := connID != nil
	res := map[string]any{"matched": true, "connection_created": created}
	if created {
		res["connection_id"] = connID
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	response.Success(w, status, res)
}
