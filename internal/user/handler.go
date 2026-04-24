package user

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
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
	UpdateUser(ctx context.Context, userID uuid.UUID, username, city, country, bio *string, soberSince *time.Time, replaceSoberSince bool, interests []string, replaceInterests bool, lat, lng *float64) error
	UpdateAvatarURL(ctx context.Context, userID uuid.UUID, avatarURL string) error
	UpdateBannerURL(ctx context.Context, userID uuid.UUID, bannerURL string) error
	UpdateCurrentLocation(ctx context.Context, userID uuid.UUID, lat, lng float64, city string) error
	DiscoverUsers(ctx context.Context, currentUserID uuid.UUID, city, query string, lat, lng *float64, limit, offset int) ([]User, error)
	ListInterests(ctx context.Context) ([]string, error)
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
	BannerURL               *string    `json:"banner_url"`
	City                    *string    `json:"city"`
	Country                 *string    `json:"country"`
	Bio                     *string    `json:"bio"`
	Interests               []string   `json:"interests"`
	SoberSince              *time.Time `json:"sober_since"`
	CreatedAt               time.Time  `json:"created_at"`
	FriendshipStatus        string     `json:"friendship_status"`
	FriendCount             int        `json:"friend_count"`
	IncomingFriendRequestCt int        `json:"incoming_friend_request_count"`
	OutgoingFriendRequestCt int        `json:"outgoing_friend_request_count"`
	CurrentCity             *string    `json:"current_city,omitempty"`
	LocationUpdatedAt       *time.Time `json:"location_updated_at,omitempty"`
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
		Username   *string   `json:"username"`
		City       *string   `json:"city"`
		Country    *string   `json:"country"`
		Bio        *string   `json:"bio"`
		SoberSince *string   `json:"sober_since"`
		Interests  *[]string `json:"interests"`
		Lat        *float64  `json:"lat"`
		Lng        *float64  `json:"lng"`
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
			log.Printf("update profile username validation failed for user %s: %v", userID, err)
			response.Error(w, http.StatusInternalServerError, "could not validate username")
			return
		}
		if exists {
			response.Error(w, http.StatusConflict, "username already taken")
			return
		}

		input.Username = &normalized
	}

	if input.Bio != nil {
		trimmedBio := strings.TrimSpace(*input.Bio)
		if len(trimmedBio) > 160 {
			response.ValidationError(w, map[string]string{"bio": "bio must be 160 characters or fewer"})
			return
		}
		input.Bio = &trimmedBio
	}

	var parsedSoberSince *time.Time
	if input.SoberSince != nil {
		trimmedSoberSince := strings.TrimSpace(*input.SoberSince)
		if trimmedSoberSince == "" {
			input.SoberSince = &trimmedSoberSince
		} else {
			parsed, err := parseSoberSince(trimmedSoberSince)
			if err != nil {
				response.Error(w, http.StatusBadRequest, "sober_since must be YYYY-MM-DD")
				return
			}
			parsedSoberSince = parsed
			input.SoberSince = &trimmedSoberSince
		}
	}

	normalizedInterests := make([]string, 0)
	if input.Interests != nil {
		if len(*input.Interests) > 5 {
			response.ValidationError(w, map[string]string{"interests": "pick up to 5 interests"})
			return
		}

		allowedInterests, err := h.db.ListInterests(r.Context())
		if err != nil {
			log.Printf("update profile interests lookup failed for user %s: %v", userID, err)
			response.Error(w, http.StatusInternalServerError, "could not load interests")
			return
		}

		allowedSet := make(map[string]struct{}, len(allowedInterests))
		for _, interest := range allowedInterests {
			allowedSet[interest] = struct{}{}
		}

		seen := make(map[string]struct{}, len(*input.Interests))
		for _, rawInterest := range *input.Interests {
			interest := strings.TrimSpace(rawInterest)
			if interest == "" {
				response.ValidationError(w, map[string]string{"interests": "interests cannot contain empty values"})
				return
			}
			if _, exists := allowedSet[interest]; !exists {
				response.ValidationError(w, map[string]string{"interests": "one or more interests are invalid"})
				return
			}
			if _, exists := seen[interest]; exists {
				response.ValidationError(w, map[string]string{"interests": "duplicate interests are not allowed"})
				return
			}
			seen[interest] = struct{}{}
			normalizedInterests = append(normalizedInterests, interest)
		}

		slices.Sort(normalizedInterests)
	}

	if err := h.db.UpdateUser(
		r.Context(),
		userID,
		input.Username,
		input.City,
		input.Country,
		input.Bio,
		parsedSoberSince,
		input.SoberSince != nil,
		normalizedInterests,
		input.Interests != nil,
		input.Lat,
		input.Lng,
	); err != nil {
		log.Printf("update profile failed for user %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not update profile")
		return
	}

	user, _ := h.db.GetUser(r.Context(), userID, userID)
	response.Success(w, http.StatusOK, user)
}

// ListInterests returns the curated interest tags available for user profiles.
func (h *Handler) ListInterests(w http.ResponseWriter, r *http.Request) {
	interests, err := h.db.ListInterests(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch interests")
		return
	}

	response.Success(w, http.StatusOK, map[string][]string{"items": interests})
}

// UpdateMyCurrentLocation silently records the caller's live GPS position and reverse-geocoded city.
func (h *Handler) UpdateMyCurrentLocation(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Lat  float64 `json:"lat"`
		Lng  float64 `json:"lng"`
		City string  `json:"city"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.db.UpdateCurrentLocation(r.Context(), userID, input.Lat, input.Lng, input.City); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update location")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func parseSoberSince(raw string) (*time.Time, error) {
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, err
	}

	return &parsed, nil
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

// UploadBanner validates, resizes, uploads, and saves the caller's banner image.
func (h *Handler) UploadBanner(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		response.Error(w, http.StatusBadRequest, "file too large or invalid form data")
		return
	}

	file, header, err := r.FormFile("banner")
	if err != nil {
		response.Error(w, http.StatusBadRequest, "banner field is required")
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType != "image/jpeg" && contentType != "image/png" {
		response.Error(w, http.StatusBadRequest, "banner must be a JPEG or PNG image")
		return
	}

	img, err := imaging.Decode(file)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "could not decode image")
		return
	}

	img = imaging.Fit(img, 2048, 683, imaging.Lanczos)

	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not encode image")
		return
	}

	key := fmt.Sprintf("banners/%s/original.jpg", userID)
	bannerURL, err := h.uploader.Upload(r.Context(), key, "image/jpeg", &buf)
	if err != nil {
		log.Printf("banner upload failed for user %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not upload image")
		return
	}

	if err := h.db.UpdateBannerURL(r.Context(), userID, bannerURL); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not save banner")
		return
	}

	response.Success(w, http.StatusOK, map[string]string{"banner_url": bannerURL})
}

// Discover returns one page of ranked user results plus the caller's
// friendship state for each row so the app does not need global friend sets.
func (h *Handler) Discover(w http.ResponseWriter, r *http.Request) {
	currentUserID := middleware.CurrentUserID(r)
	city := r.URL.Query().Get("city")
	query := r.URL.Query().Get("q")
	params := pagination.Parse(r, 20, 50)

	var lat, lng *float64
	if s := r.URL.Query().Get("lat"); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			lat = &v
		}
	}
	if s := r.URL.Query().Get("lng"); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			lng = &v
		}
	}

	users, err := h.db.DiscoverUsers(r.Context(), currentUserID, city, username.Normalize(query), lat, lng, params.Limit+1, params.Offset)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch users")
		return
	}

	response.Success(w, http.StatusOK, pagination.Slice(users, params))
}
