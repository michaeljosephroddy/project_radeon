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

type DiscoverUsersParams struct {
	CurrentUserID uuid.UUID
	City          string
	Query         string
	Gender        string
	Sobriety      string
	AgeMin        *int
	AgeMax        *int
	DistanceKm    *int
	Interests     []string
	Lat           *float64
	Lng           *float64
	DisplayLimit  int
	Limit         int
	Offset        int
}

type DiscoverPreviewResponse struct {
	ExactCount         int                    `json:"exact_count"`
	BroadenedCount     *int                   `json:"broadened_count,omitempty"`
	BroadenedAvailable bool                   `json:"broadened_available"`
	RelaxedFilters     []string               `json:"relaxed_filters,omitempty"`
	LikelyTooNarrow    []string               `json:"likely_too_narrow_fields,omitempty"`
	EffectiveFilters   DiscoverPreviewFilters `json:"effective_filters"`
}

type DiscoverPreviewFilters struct {
	Gender     string   `json:"gender,omitempty"`
	Sobriety   string   `json:"sobriety,omitempty"`
	AgeMin     *int     `json:"age_min,omitempty"`
	AgeMax     *int     `json:"age_max,omitempty"`
	DistanceKm *int     `json:"distance_km,omitempty"`
	Interests  []string `json:"interests,omitempty"`
}

// Querier is the database interface required by the user handler.
type Querier interface {
	GetUser(ctx context.Context, viewerID, userID uuid.UUID) (*User, error)
	UsernameExistsForOthers(ctx context.Context, username string, userID uuid.UUID) (bool, error)
	UpdateUser(ctx context.Context, userID uuid.UUID, username, city, country, gender, bio *string, soberSince *time.Time, replaceSoberSince bool, birthDate *time.Time, replaceBirthDate bool, interests []string, replaceInterests bool, lat, lng *float64) error
	UpdateAvatarURL(ctx context.Context, userID uuid.UUID, avatarURL string) error
	UpdateBannerURL(ctx context.Context, userID uuid.UUID, bannerURL string) error
	UpdateCurrentLocation(ctx context.Context, userID uuid.UUID, lat, lng float64, city string) error
	DiscoverUsers(ctx context.Context, params DiscoverUsersParams) ([]User, error)
	CountDiscoverUsers(ctx context.Context, params DiscoverUsersParams) (int, error)
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
	IsPlus                  bool       `json:"is_plus"`
	SubscriptionTier        string     `json:"subscription_tier"`
	SubscriptionStatus      string     `json:"subscription_status"`
	City                    *string    `json:"city"`
	Country                 *string    `json:"country"`
	Bio                     *string    `json:"bio"`
	Interests               []string   `json:"interests"`
	Gender                  *string    `json:"gender"`
	BirthDate               *string    `json:"birth_date"`
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
		Gender     *string   `json:"gender"`
		Bio        *string   `json:"bio"`
		BirthDate  *string   `json:"birth_date"`
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

	if input.Gender != nil {
		trimmedGender := strings.TrimSpace(*input.Gender)
		if trimmedGender == "" {
			input.Gender = &trimmedGender
		} else {
			normalizedGender, ok := normalizeProfileGender(trimmedGender)
			if !ok {
				response.ValidationError(w, map[string]string{"gender": "gender must be woman, man, or non_binary"})
				return
			}
			input.Gender = &normalizedGender
		}
	}

	var parsedBirthDate *time.Time
	if input.BirthDate != nil {
		trimmedBirthDate := strings.TrimSpace(*input.BirthDate)
		if trimmedBirthDate == "" {
			input.BirthDate = &trimmedBirthDate
		} else {
			parsed, err := parseCalendarDate(trimmedBirthDate)
			if err != nil {
				response.Error(w, http.StatusBadRequest, "birth_date must be YYYY-MM-DD")
				return
			}
			parsedBirthDate = parsed
			input.BirthDate = &trimmedBirthDate
		}
	}

	var parsedSoberSince *time.Time
	if input.SoberSince != nil {
		trimmedSoberSince := strings.TrimSpace(*input.SoberSince)
		if trimmedSoberSince == "" {
			input.SoberSince = &trimmedSoberSince
		} else {
			parsed, err := parseCalendarDate(trimmedSoberSince)
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
		input.Gender,
		input.Bio,
		parsedSoberSince,
		input.SoberSince != nil,
		parsedBirthDate,
		input.BirthDate != nil,
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
	return parseCalendarDate(raw)
}

func parseCalendarDate(raw string) (*time.Time, error) {
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, err
	}

	return &parsed, nil
}

func normalizeProfileGender(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "woman", "women":
		return "woman", true
	case "man", "men":
		return "man", true
	case "non_binary", "non-binary", "nonbinary":
		return "non_binary", true
	default:
		return "", false
	}
}

func normalizeDiscoverGender(raw string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", true
	}
	normalized, ok := normalizeProfileGender(raw)
	if ok {
		return normalized, true
	}
	if strings.EqualFold(strings.TrimSpace(raw), "any") {
		return "", true
	}
	return "", false
}

func normalizeSobrietyFilter(raw string) (string, bool) {
	switch strings.TrimSpace(raw) {
	case "", "Any", "any":
		return "", true
	case "30+ days", "days_30":
		return "days_30", true
	case "90+ days", "days_90":
		return "days_90", true
	case "1+ year", "years_1":
		return "years_1", true
	case "5+ years", "years_5":
		return "years_5", true
	default:
		return "", false
	}
}

func parseInterestFilters(r *http.Request) []string {
	values := append([]string{}, r.URL.Query()["interest"]...)
	if raw := strings.TrimSpace(r.URL.Query().Get("interests")); raw != "" {
		values = append(values, strings.Split(raw, ",")...)
	}

	seen := make(map[string]struct{}, len(values))
	interests := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		interests = append(interests, trimmed)
	}

	return interests
}

func parseDiscoverRequest(r *http.Request, allowAdvanced bool) (DiscoverUsersParams, error) {
	params := pagination.Parse(r, 20, 50)
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	request := DiscoverUsersParams{
		City:         strings.TrimSpace(r.URL.Query().Get("city")),
		Query:        username.Normalize(query),
		DisplayLimit: params.Limit,
		Limit:        params.Limit + 1,
		Offset:       params.Offset,
	}

	if s := r.URL.Query().Get("lat"); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			request.Lat = &v
		}
	}
	if s := r.URL.Query().Get("lng"); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			request.Lng = &v
		}
	}

	if !allowAdvanced {
		return request, nil
	}

	var ok bool
	request.Gender, ok = normalizeDiscoverGender(r.URL.Query().Get("gender"))
	if !ok {
		return DiscoverUsersParams{}, fmt.Errorf("gender must be woman, man, or non_binary")
	}
	request.Sobriety, ok = normalizeSobrietyFilter(r.URL.Query().Get("sobriety"))
	if !ok {
		return DiscoverUsersParams{}, fmt.Errorf("sobriety must be 30+ days, 90+ days, 1+ year, or 5+ years")
	}

	var err error
	request.AgeMin, err = parseOptionalIntParam(r, "age_min")
	if err != nil {
		return DiscoverUsersParams{}, err
	}
	request.AgeMax, err = parseOptionalIntParam(r, "age_max")
	if err != nil {
		return DiscoverUsersParams{}, err
	}
	if request.AgeMin != nil && request.AgeMax != nil && *request.AgeMin > *request.AgeMax {
		return DiscoverUsersParams{}, fmt.Errorf("age_min cannot be greater than age_max")
	}
	request.DistanceKm, err = parseOptionalIntParam(r, "distance_km")
	if err != nil {
		return DiscoverUsersParams{}, err
	}
	request.Interests = parseInterestFilters(r)

	return request, nil
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func relaxDistance(distanceKm *int) (*int, bool) {
	if distanceKm == nil || *distanceKm <= 0 {
		return distanceKm, false
	}

	current := *distanceKm
	switch {
	case current < 50:
		next := 50
		return &next, true
	case current < 100:
		next := 100
		return &next, true
	case current < 200:
		next := 200
		return &next, true
	default:
		return nil, true
	}
}

func relaxAgeBounds(ageMin, ageMax *int) (*int, *int, bool) {
	if ageMin == nil && ageMax == nil {
		return ageMin, ageMax, false
	}

	nextMin := cloneInt(ageMin)
	nextMax := cloneInt(ageMax)
	changed := false

	if nextMin != nil {
		value := *nextMin - 5
		if value <= 18 {
			nextMin = nil
		} else {
			nextMin = &value
		}
		changed = true
	}

	if nextMax != nil {
		value := *nextMax + 5
		if value >= 99 {
			nextMax = nil
		} else {
			nextMax = &value
		}
		changed = true
	}

	return nextMin, nextMax, changed
}

func relaxSobriety(raw string) (string, bool) {
	switch raw {
	case "years_5":
		return "years_1", true
	case "years_1":
		return "days_90", true
	case "days_90":
		return "days_30", true
	case "days_30":
		return "", true
	default:
		return raw, false
	}
}

func discoverActiveFieldNames(params DiscoverUsersParams) []string {
	fields := make([]string, 0, 5)
	if params.DistanceKm != nil && *params.DistanceKm > 0 {
		fields = append(fields, "distance")
	}
	if params.AgeMin != nil || params.AgeMax != nil {
		fields = append(fields, "age")
	}
	if len(params.Interests) > 0 {
		fields = append(fields, "interests")
	}
	if params.Sobriety != "" {
		fields = append(fields, "sobriety")
	}
	if params.Gender != "" {
		fields = append(fields, "gender")
	}
	return fields
}

func buildBroadenedDiscoverParams(params DiscoverUsersParams) (DiscoverUsersParams, []string) {
	broadened := params
	relaxed := make([]string, 0, 4)

	if nextDistance, changed := relaxDistance(params.DistanceKm); changed {
		broadened.DistanceKm = nextDistance
		relaxed = append(relaxed, "distance")
	}

	if nextMin, nextMax, changed := relaxAgeBounds(params.AgeMin, params.AgeMax); changed {
		broadened.AgeMin = nextMin
		broadened.AgeMax = nextMax
		relaxed = append(relaxed, "age")
	}

	if len(params.Interests) > 0 {
		broadened.Interests = nil
		relaxed = append(relaxed, "interests")
	}

	if nextSobriety, changed := relaxSobriety(params.Sobriety); changed {
		broadened.Sobriety = nextSobriety
		relaxed = append(relaxed, "sobriety")
	}

	return broadened, relaxed
}

func discoverPreviewFiltersFromParams(params DiscoverUsersParams) DiscoverPreviewFilters {
	return DiscoverPreviewFilters{
		Gender:     params.Gender,
		Sobriety:   params.Sobriety,
		AgeMin:     cloneInt(params.AgeMin),
		AgeMax:     cloneInt(params.AgeMax),
		DistanceKm: cloneInt(params.DistanceKm),
		Interests:  append([]string{}, params.Interests...),
	}
}

func (h *Handler) buildDiscoverPreview(ctx context.Context, params DiscoverUsersParams) (*DiscoverPreviewResponse, error) {
	exactCount, err := h.db.CountDiscoverUsers(ctx, params)
	if err != nil {
		return nil, err
	}

	preview := &DiscoverPreviewResponse{
		ExactCount:         exactCount,
		BroadenedAvailable: false,
		EffectiveFilters:   discoverPreviewFiltersFromParams(params),
		LikelyTooNarrow:    discoverActiveFieldNames(params),
	}

	if exactCount > 0 {
		return preview, nil
	}

	working := params
	relaxedFields := make([]string, 0, 4)
	for steps := 0; steps < 4; steps++ {
		next, relaxed := buildBroadenedDiscoverParams(working)
		if len(relaxed) == 0 {
			break
		}

		working = next
		for _, field := range relaxed {
			if !slices.Contains(relaxedFields, field) {
				relaxedFields = append(relaxedFields, field)
			}
		}

		broadenedCount, err := h.db.CountDiscoverUsers(ctx, working)
		if err != nil {
			return nil, err
		}
		preview.BroadenedCount = &broadenedCount
		if broadenedCount > exactCount {
			preview.BroadenedAvailable = broadenedCount > exactCount
			preview.RelaxedFilters = relaxedFields
			preview.EffectiveFilters = discoverPreviewFiltersFromParams(working)
			preview.LikelyTooNarrow = relaxedFields
			return preview, nil
		}
	}

	if preview.BroadenedCount != nil && *preview.BroadenedCount > exactCount {
		preview.BroadenedAvailable = true
		preview.RelaxedFilters = relaxedFields
		preview.EffectiveFilters = discoverPreviewFiltersFromParams(working)
		preview.LikelyTooNarrow = relaxedFields
	}

	return preview, nil
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
	pageParams := pagination.Parse(r, 20, 50)

	viewer, err := h.db.GetUser(r.Context(), currentUserID, currentUserID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not load discover access")
		return
	}

	discoverParams, err := parseDiscoverRequest(r, hasDiscoverAdvancedAccess(viewer))
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	discoverParams.CurrentUserID = currentUserID

	users, err := h.db.DiscoverUsers(r.Context(), discoverParams)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch users")
		return
	}

	response.Success(w, http.StatusOK, pagination.Slice(users, pageParams))
}

func (h *Handler) DiscoverPreview(w http.ResponseWriter, r *http.Request) {
	currentUserID := middleware.CurrentUserID(r)

	discoverParams, err := parseDiscoverRequest(r, true)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	discoverParams.CurrentUserID = currentUserID
	discoverParams.Limit = 1
	discoverParams.Offset = 0

	preview, err := h.buildDiscoverPreview(r.Context(), discoverParams)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not preview discover filters")
		return
	}

	response.Success(w, http.StatusOK, preview)
}

func hasDiscoverAdvancedAccess(user *User) bool {
	if user == nil {
		return false
	}
	return user.IsPlus || (user.SubscriptionTier == "plus" && user.SubscriptionStatus == "active")
}

func parseOptionalIntParam(r *http.Request, key string) (*int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be a valid integer", key)
	}
	return &value, nil
}
