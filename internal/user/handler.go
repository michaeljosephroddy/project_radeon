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

func NewHandler(db *pgxpool.Pool, uploader Uploader) *Handler {
	return &Handler{db: db, uploader: uploader}
}

type User struct {
	ID         uuid.UUID  `json:"id"`
	Username   string     `json:"username"`
	AvatarURL  *string    `json:"avatar_url"`
	City       *string    `json:"city"`
	Country    *string    `json:"country"`
	SoberSince *time.Time `json:"sober_since"`
	CreatedAt  time.Time  `json:"created_at"`
}

// GET /users/me
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	user, err := h.fetchUser(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusNotFound, "user not found")
		return
	}
	response.Success(w, http.StatusOK, user)
}

// GET /users/{id}
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}
	user, err := h.fetchUser(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusNotFound, "user not found")
		return
	}
	response.Success(w, http.StatusOK, user)
}

// PATCH /users/me
func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Username   *string `json:"username"`
		City       *string `json:"city"`
		Country    *string `json:"country"`
		SoberSince *string `json:"sober_since"`
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
		`UPDATE users SET
			username = COALESCE($1, username),
			city     = COALESCE($2, city),
			country  = COALESCE($3, country)
		WHERE id = $4`,
		input.Username, input.City, input.Country, userID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update profile")
		return
	}

	user, _ := h.fetchUser(r.Context(), userID)
	response.Success(w, http.StatusOK, user)
}

// POST /users/me/avatar
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
		`UPDATE users SET avatar_url = $1 WHERE id = $2`,
		avatarURL, userID,
	); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not save avatar")
		return
	}

	response.Success(w, http.StatusOK, map[string]string{"avatar_url": avatarURL})
}

// GET /users/discover?city=Dublin
func (h *Handler) Discover(w http.ResponseWriter, r *http.Request) {
	currentUserID := middleware.CurrentUserID(r)
	city := r.URL.Query().Get("city")
	query := r.URL.Query().Get("q")

	rows, err := h.db.Query(r.Context(),
		`SELECT u.id, u.username, u.avatar_url, u.city, u.country, u.sober_since, u.created_at
		 FROM users u
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
		 LIMIT 20`,
		currentUserID, city, username.Normalize(query),
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch users")
		return
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		rows.Scan(&u.ID, &u.Username, &u.AvatarURL, &u.City, &u.Country, &u.SoberSince, &u.CreatedAt)
		users = append(users, u)
	}

	response.Success(w, http.StatusOK, users)
}

func (h *Handler) fetchUser(ctx context.Context, id uuid.UUID) (*User, error) {
	var u User
	err := h.db.QueryRow(ctx,
		`SELECT id, username, avatar_url, city, country, sober_since, created_at
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Username, &u.AvatarURL, &u.City, &u.Country, &u.SoberSince, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}
