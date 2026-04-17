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
)

// CacheInvalidator is implemented by *discovery.Handler. Defined here so
// internal/user does not import internal/discovery.
type CacheInvalidator interface {
	InvalidateSuggestions(userID uuid.UUID)
}

// Uploader is implemented by *storage.S3Uploader.
type Uploader interface {
	Upload(ctx context.Context, key, contentType string, body io.Reader) (string, error)
}

type Handler struct {
	db          *pgxpool.Pool
	invalidator CacheInvalidator
	uploader    Uploader
}

func NewHandler(db *pgxpool.Pool, invalidator CacheInvalidator, uploader Uploader) *Handler {
	return &Handler{db: db, invalidator: invalidator, uploader: uploader}
}

type User struct {
	ID                uuid.UUID  `json:"id"`
	FirstName         string     `json:"first_name"`
	LastName          string     `json:"last_name"`
	AvatarURL         *string    `json:"avatar_url"`
	AvatarURLBlurred  *string    `json:"avatar_url_blurred"`
	City              *string    `json:"city"`
	Country           *string    `json:"country"`
	SoberSince        *time.Time `json:"sober_since"`
	Lat               *float64   `json:"lat,omitempty"`
	Lng               *float64   `json:"lng,omitempty"`
	DiscoveryRadiusKm int        `json:"discovery_radius_km"`
	CreatedAt         time.Time  `json:"created_at"`
	Interests         []string   `json:"interests"`
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
		FirstName         *string  `json:"first_name"`
		LastName          *string  `json:"last_name"`
		City              *string  `json:"city"`
		Country           *string  `json:"country"`
		AvatarURL         *string  `json:"avatar_url"`
		SoberSince        *string  `json:"sober_since"`
		Lat               *float64 `json:"lat"`
		Lng               *float64 `json:"lng"`
		DiscoveryRadiusKm *int     `json:"discovery_radius_km"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if input.DiscoveryRadiusKm != nil && *input.DiscoveryRadiusKm < 1 {
		response.Error(w, http.StatusBadRequest, "discovery_radius_km must be at least 1")
		return
	}

	_, err := h.db.Exec(r.Context(),
		`UPDATE users SET
			first_name          = COALESCE($1, first_name),
			last_name           = COALESCE($2, last_name),
			city                = COALESCE($3, city),
			country             = COALESCE($4, country),
			avatar_url          = COALESCE($5, avatar_url),
			lat                 = COALESCE($6, lat),
			lng                 = COALESCE($7, lng),
			discovery_radius_km = COALESCE($8, discovery_radius_km)
		WHERE id = $9`,
		input.FirstName, input.LastName, input.City, input.Country, input.AvatarURL,
		input.Lat, input.Lng, input.DiscoveryRadiusKm, userID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update profile")
		return
	}

	// Location or radius change makes cached suggestions stale.
	if input.Lat != nil || input.Lng != nil || input.DiscoveryRadiusKm != nil {
		h.invalidator.InvalidateSuggestions(userID)
	}

	user, _ := h.fetchUser(r.Context(), userID)
	response.Success(w, http.StatusOK, user)
}

// POST /users/me/avatar — upload a profile photo
//
// Accepts multipart/form-data with an "avatar" field (JPEG or PNG, max 10 MB).
// Resizes to 1024px max dimension, then generates a heavily blurred copy.
// Both are uploaded to S3 and the URLs are stored on the user record.
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

	// Cap at 1024px on the longest side to avoid storing giant originals.
	img = imaging.Fit(img, 1024, 1024, imaging.Lanczos)

	var origBuf bytes.Buffer
	if err := imaging.Encode(&origBuf, img, imaging.JPEG); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not encode image")
		return
	}

	blurred := imaging.Blur(img, 100)
	var blurBuf bytes.Buffer
	if err := imaging.Encode(&blurBuf, blurred, imaging.JPEG); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not encode blurred image")
		return
	}

	origKey := fmt.Sprintf("avatars/%s/original.jpg", userID)
	blurKey := fmt.Sprintf("avatars/%s/blurred.jpg", userID)

	origURL, err := h.uploader.Upload(r.Context(), origKey, "image/jpeg", &origBuf)
	if err != nil {
		log.Printf("avatar upload failed (original) for user %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not upload image")
		return
	}

	blurURL, err := h.uploader.Upload(r.Context(), blurKey, "image/jpeg", &blurBuf)
	if err != nil {
		log.Printf("avatar upload failed (blurred) for user %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not upload blurred image")
		return
	}

	if _, err := h.db.Exec(r.Context(),
		`UPDATE users SET avatar_url = $1, avatar_url_blurred = $2 WHERE id = $3`,
		origURL, blurURL, userID,
	); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not save avatar")
		return
	}

	response.Success(w, http.StatusOK, map[string]string{
		"avatar_url":         origURL,
		"avatar_url_blurred": blurURL,
	})
}

// GET /users/discover?city=Dublin
func (h *Handler) Discover(w http.ResponseWriter, r *http.Request) {
	currentUserID := middleware.CurrentUserID(r)
	city := r.URL.Query().Get("city")

	rows, err := h.db.Query(r.Context(),
		`SELECT u.id, u.first_name, u.last_name, u.avatar_url, u.city, u.country, u.sober_since, u.created_at, u.lat, u.lng
		 FROM users u
		 WHERE u.id != $1
			AND NOT EXISTS (
				SELECT 1 FROM connections c
				WHERE (c.requester_id = $1 AND c.addressee_id = u.id)
				OR    (c.addressee_id = $1 AND c.requester_id = u.id)
				AND c.status IN ('pending', 'accepted')
			)
		 AND ($2 = '' OR u.city ILIKE $2)
		 ORDER BY u.created_at DESC
		 LIMIT 20`,
		currentUserID, city,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch users")
		return
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		rows.Scan(&u.ID, &u.FirstName, &u.LastName, &u.AvatarURL, &u.City, &u.Country, &u.SoberSince, &u.CreatedAt, &u.Lat, &u.Lng)
		u.Interests = h.fetchInterests(r.Context(), u.ID)
		users = append(users, u)
	}

	response.Success(w, http.StatusOK, users)
}

// --- helpers ---

func (h *Handler) fetchUser(ctx context.Context, id uuid.UUID) (*User, error) {
	var u User
	err := h.db.QueryRow(ctx,
		`SELECT id, first_name, last_name, avatar_url, avatar_url_blurred, city, country, sober_since, created_at, lat, lng, discovery_radius_km
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.FirstName, &u.LastName, &u.AvatarURL, &u.AvatarURLBlurred, &u.City, &u.Country, &u.SoberSince, &u.CreatedAt, &u.Lat, &u.Lng, &u.DiscoveryRadiusKm)
	if err != nil {
		return nil, err
	}
	u.Interests = h.fetchInterests(ctx, id)
	return &u, nil
}

func (h *Handler) fetchInterests(ctx context.Context, userID uuid.UUID) []string {
	rows, err := h.db.Query(ctx,
		`SELECT i.name FROM interests i
		 JOIN user_interests ui ON ui.interest_id = i.id
		 WHERE ui.user_id = $1`, userID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var interests []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		interests = append(interests, name)
	}
	return interests
}
