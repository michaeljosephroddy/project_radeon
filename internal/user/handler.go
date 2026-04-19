package user

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/disintegration/imaging"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/pagination"
	"github.com/project_radeon/api/pkg/response"
	"github.com/project_radeon/api/pkg/username"
)

// Uploader is implemented by *storage.S3Uploader.
type Uploader interface {
	Upload(ctx context.Context, key, contentType string, body io.Reader) (string, error)
}

// Querier is the database interface required by the user handler.
type Querier interface {
	GetUser(ctx context.Context, viewerID, userID uuid.UUID) (*User, error)
	UsernameExistsForOthers(ctx context.Context, username string, userID uuid.UUID) (bool, error)
	UpdateUser(ctx context.Context, userID uuid.UUID, username, city, country *string) error
	UpdateAvatarURL(ctx context.Context, userID uuid.UUID, avatarURL string) error
	DiscoverUsers(ctx context.Context, currentUserID uuid.UUID, city, query string, limit, offset int) ([]User, error)
}

type Handler struct {
	db       Querier
	uploader Uploader
}

// NewHandler builds a user handler. Pass user.NewPgStore(pool) for production.
func NewHandler(db Querier, uploader Uploader) *Handler {
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
	user, err := h.db.GetUser(r.Context(), userID, userID)
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
	user, err := h.db.GetUser(r.Context(), viewerID, id)
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
		exists, err := h.db.UsernameExistsForOthers(r.Context(), normalized, userID)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "could not validate username")
			return
		}
		if exists {
			response.Error(w, http.StatusConflict, "username already taken")
			return
		}

		input.Username = &normalized
	}

	if err := h.db.UpdateUser(r.Context(), userID, input.Username, input.City, input.Country); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update profile")
		return
	}

	user, _ := h.db.GetUser(r.Context(), userID, userID)
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

	if err := h.db.UpdateAvatarURL(r.Context(), userID, avatarURL); err != nil {
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

	users, err := h.db.DiscoverUsers(r.Context(), currentUserID, city, username.Normalize(query), params.Limit+1, params.Offset)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch users")
		return
	}

	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "could not fetch users")
		return
	}

	response.Success(w, http.StatusOK, pagination.Slice(users, params))
}
