package user

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/disintegration/imaging"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/pagination"
	"github.com/project_radeon/api/pkg/response"
	"github.com/project_radeon/api/pkg/username"
)

// Uploader is implemented by *storage.S3Uploader.
type Uploader interface {
	Upload(ctx context.Context, key, contentType string, body io.Reader) (string, error)
}

type Handler struct {
	db       *pgxpool.Pool
	uploader Uploader
}

// NewHandler builds a user handler with database access and avatar upload support.
func NewHandler(db *pgxpool.Pool, uploader Uploader) *Handler {
	return &Handler{db: db, uploader: uploader}
}

type User struct {
	ID                      uuid.UUID  `json:"id"`
	Username                string     `json:"username"`
	AvatarURL               *string    `json:"avatar_url"`
	City                    *string    `json:"city"`
	Country                 *string    `json:"country"`
	SoberSince              *time.Time `json:"sober_since"`
	CreatedAt               time.Time  `json:"created_at"`
	FriendshipStatus        string     `json:"friendship_status"`
	FriendCount             int        `json:"friend_count"`
	IncomingFriendRequestCt int        `json:"incoming_friend_request_count"`
	OutgoingFriendRequestCt int        `json:"outgoing_friend_request_count"`
}

// GetMe returns the authenticated user's profile record.
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	user, err := h.fetchUser(r.Context(), userID, userID)
	if err != nil {
		response.Error(w, http.StatusNotFound, "user not found")
		return
	}
	response.Success(w, http.StatusOK, user)
}

// GetUser returns a public profile record for the requested user ID.
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	viewerID := middleware.CurrentUserID(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}
	user, err := h.fetchUser(r.Context(), viewerID, id)
	if err != nil {
		response.Error(w, http.StatusNotFound, "user not found")
		return
	}
	response.Success(w, http.StatusOK, user)
}

// UpdateMe applies profile edits for the authenticated user and returns the updated record.
func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Username *string `json:"username"`
		City     *string `json:"city"`
		Country  *string `json:"country"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if input.Username != nil {
		normalized := username.Normalize(*input.Username)
		if msg := username.ValidationError(normalized); msg != "" {
			response.ValidationError(w, map[string]string{"username": msg})
			return
		}

		// Username updates keep the same normalization and uniqueness rules as
		// registration so profile edits cannot bypass signup constraints.
		var exists bool
		if err := h.db.QueryRow(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM users WHERE username = $1 AND id != $2)`,
			normalized, userID,
		).Scan(&exists); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not validate username")
			return
		}
		if exists {
			response.Error(w, http.StatusConflict, "username already taken")
			return
		}

		input.Username = &normalized
	}

	_, err := h.db.Exec(r.Context(),
		`UPDATE users
		SET
			username = COALESCE($1, username),
			city = COALESCE($2, city),
			country = COALESCE($3, country)
		WHERE id = $4`,
		input.Username, input.City, input.Country, userID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update profile")
		return
	}

	user, _ := h.fetchUser(r.Context(), userID, userID)
	response.Success(w, http.StatusOK, user)
}

// UploadAvatar validates, resizes, uploads, and saves the caller's avatar image.
func (h *Handler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		response.Error(w, http.StatusBadRequest, "file too large or invalid form data")
		return
	}

	file, header, err := r.FormFile("avatar")
	if err != nil {
		response.Error(w, http.StatusBadRequest, "avatar field is required")
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType != "image/jpeg" && contentType != "image/png" {
		response.Error(w, http.StatusBadRequest, "avatar must be a JPEG or PNG image")
		return
	}

	img, err := imaging.Decode(file)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "could not decode image")
		return
	}

	// Images are resized server-side before upload so the app does not depend on
	// clients to enforce avatar dimensions or output format.
	img = imaging.Fit(img, 1024, 1024, imaging.Lanczos)

	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not encode image")
		return
	}

	key := fmt.Sprintf("avatars/%s/original.jpg", userID)
	avatarURL, err := h.uploader.Upload(r.Context(), key, "image/jpeg", &buf)
	if err != nil {
		log.Printf("avatar upload failed for user %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not upload image")
		return
	}

	if _, err := h.db.Exec(r.Context(),
		`UPDATE users
		SET avatar_url = $1
		WHERE id = $2`,
		avatarURL, userID,
	); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not save avatar")
		return
	}

	response.Success(w, http.StatusOK, map[string]string{"avatar_url": avatarURL})
}

// Discover returns one page of ranked user search results plus the caller's
// friendship state for each row so the app does not need global friend sets.
func (h *Handler) Discover(w http.ResponseWriter, r *http.Request) {
	currentUserID := middleware.CurrentUserID(r)
	city := r.URL.Query().Get("city")
	query := r.URL.Query().Get("q")
	params := pagination.Parse(r, 20, 50)

	// The ORDER BY prioritises exact and prefix username matches before falling
	// back to newest users, which gives search results a predictable ranking.
	rows, err := h.db.Query(r.Context(),
		`SELECT
			u.id,
			u.username,
			u.avatar_url,
			u.city,
			u.country,
			u.sober_since,
			u.created_at,
			CASE
				WHEN f.status = 'accepted' THEN 'friends'
				WHEN f.requester_id = $1 THEN 'outgoing'
				WHEN f.requester_id = u.id THEN 'incoming'
				ELSE 'none'
			END AS friendship_status
		FROM users u
		LEFT JOIN friendships f
			ON (
				(f.user_a_id = $1 AND f.user_b_id = u.id)
				OR (f.user_b_id = $1 AND f.user_a_id = u.id)
			)
		WHERE u.id != $1
			AND ($2 = '' OR u.city ILIKE $2)
			AND (
				$3 = ''
				OR u.username ILIKE '%' || $3 || '%'
			)
		ORDER BY
			CASE
				WHEN u.username = $3 THEN 0
				WHEN u.username ILIKE $3 || '%' THEN 1
				ELSE 2
			END,
			u.created_at DESC
		LIMIT $4 OFFSET $5`,
		currentUserID, city, username.Normalize(query), params.Limit+1, params.Offset,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch users")
		return
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.AvatarURL, &u.City, &u.Country, &u.SoberSince, &u.CreatedAt, &u.FriendshipStatus); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not read users")
			return
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read users")
		return
	}

	response.Success(w, http.StatusOK, pagination.Slice(users, params))
}

// fetchUser hydrates one profile plus relationship and summary fields used by
// both /users/me and /users/{id} without separate follow-up queries.
func (h *Handler) fetchUser(ctx context.Context, viewerID uuid.UUID, id uuid.UUID) (*User, error) {
	var u User
	// Centralising the profile query keeps /users/me and /users/{id} in sync and
	// avoids subtly diverging response fields over time.
	err := h.db.QueryRow(ctx,
		`SELECT
			u.id,
			u.username,
			u.avatar_url,
			u.city,
			u.country,
			u.sober_since,
			u.created_at,
			CASE
				WHEN u.id = $1 THEN 'self'
				WHEN f.status = 'accepted' THEN 'friends'
				WHEN f.requester_id = $1 THEN 'outgoing'
				WHEN f.requester_id = u.id THEN 'incoming'
				ELSE 'none'
			END AS friendship_status,
			fc.cnt AS friend_count,
			ic.cnt AS incoming_friend_request_count,
			oc.cnt AS outgoing_friend_request_count
		FROM users u
		LEFT JOIN friendships f
			ON (
				(f.user_a_id = $1 AND f.user_b_id = u.id)
				OR (f.user_b_id = $1 AND f.user_a_id = u.id)
			)
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS cnt
			FROM friendships f2
			WHERE (f2.user_a_id = u.id OR f2.user_b_id = u.id)
				AND f2.status = 'accepted'
		) fc ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS cnt
			FROM friendships f3
			WHERE (f3.user_a_id = u.id OR f3.user_b_id = u.id)
				AND f3.status = 'pending'
				AND u.id = $1
				AND f3.requester_id != u.id
		) ic ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS cnt
			FROM friendships f4
			WHERE (f4.user_a_id = u.id OR f4.user_b_id = u.id)
				AND f4.status = 'pending'
				AND u.id = $1
				AND f4.requester_id = u.id
		) oc ON true
		WHERE u.id = $2`,
		viewerID, id,
	).Scan(
		&u.ID,
		&u.Username,
		&u.AvatarURL,
		&u.City,
		&u.Country,
		&u.SoberSince,
		&u.CreatedAt,
		&u.FriendshipStatus,
		&u.FriendCount,
		&u.IncomingFriendRequestCt,
		&u.OutgoingFriendRequestCt,
	)
	if err != nil {
		return nil, err
	}
	return &u, nil
}
