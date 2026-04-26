package meetups

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/response"
	_ "image/jpeg"
	_ "image/png"
)

type Querier interface {
	ListCategories(ctx context.Context) ([]MeetupCategory, error)
	DiscoverMeetups(ctx context.Context, userID uuid.UUID, params DiscoverMeetupsParams) (*CursorPage[Meetup], error)
	ListMyMeetups(ctx context.Context, userID uuid.UUID, params MyMeetupsParams) (*CursorPage[Meetup], error)
	GetMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error)
	CreateMeetup(ctx context.Context, userID uuid.UUID, input CreateMeetupInput) (*Meetup, error)
	UpdateMeetup(ctx context.Context, meetupID, userID uuid.UUID, input UpdateMeetupInput) (*Meetup, error)
	PublishMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error)
	CancelMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error)
	DeleteMeetup(ctx context.Context, meetupID, userID uuid.UUID) error
	GetAttendees(ctx context.Context, meetupID uuid.UUID, limit, offset int) ([]Attendee, error)
	GetWaitlist(ctx context.Context, meetupID, userID uuid.UUID, limit, offset int) ([]Attendee, error)
	ToggleRSVP(ctx context.Context, meetupID, userID uuid.UUID) (*RSVPResult, error)
}

type Handler struct {
	db       Querier
	uploader Uploader
}

type Uploader interface {
	Upload(ctx context.Context, key, contentType string, body io.Reader) (string, error)
}

func NewHandler(db Querier, uploaders ...Uploader) *Handler {
	var uploader Uploader
	if len(uploaders) > 0 {
		uploader = uploaders[0]
	}
	return &Handler{db: db, uploader: uploader}
}

func (h *Handler) ListCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := h.db.ListCategories(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch meetup categories")
		return
	}
	response.Success(w, http.StatusOK, categories)
}

func (h *Handler) ListMeetups(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := parseDiscoverMeetupsParams(r)
	meetups, err := h.db.DiscoverMeetups(r.Context(), userID, params)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch meetups")
		return
	}
	response.Success(w, http.StatusOK, meetups)
}

func (h *Handler) ListMyMeetups(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	scope := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("scope")))
	if scope == "" {
		scope = "upcoming"
	}
	if !validMyMeetupScopes[scope] {
		response.ValidationError(w, map[string]string{"scope": "invalid"})
		return
	}
	params := MyMeetupsParams{
		Scope:  scope,
		Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")),
		Limit:  parseLimit(r, 20, 50),
	}
	meetups, err := h.db.ListMyMeetups(r.Context(), userID, params)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch your meetups")
		return
	}
	response.Success(w, http.StatusOK, meetups)
}

func (h *Handler) GetMeetup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	meetupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meetup id")
		return
	}

	meetup, err := h.db.GetMeetup(r.Context(), meetupID, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "meetup not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not fetch meetup")
		return
	}

	response.Success(w, http.StatusOK, meetup)
}

func (h *Handler) CreateMeetup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input meetupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	input = normalizeMeetupInput(input)
	errs := validateMeetupInput(input)
	if len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}
	exists, err := h.categoryExists(r.Context(), input.CategorySlug)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not validate meetup category")
		return
	}
	if !exists {
		response.ValidationError(w, map[string]string{"category_slug": "invalid"})
		return
	}

	startsAt, err := parseMeetupStartsAt(input.StartsAt)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "starts_at must be ISO 8601 (e.g. 2026-05-01T19:00:00Z)")
		return
	}
	endsAt, err := parseMeetupEndsAt(input.EndsAt)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "ends_at must be ISO 8601 (e.g. 2026-05-01T21:00:00Z)")
		return
	}
	if msg := validateMeetupCapacity(input.Capacity); msg != "" {
		response.ValidationError(w, map[string]string{"capacity": msg})
		return
	}
	if msg := validateMeetupEndsAt(startsAt, endsAt); msg != "" {
		response.ValidationError(w, map[string]string{"ends_at": msg})
		return
	}
	coHostIDs, hostErrs := parseCoHostIDs(input.CoHostIDs)
	if len(hostErrs) > 0 {
		response.ValidationError(w, hostErrs)
		return
	}

	meetup, err := h.db.CreateMeetup(r.Context(), userID, CreateMeetupInput{
		Title:           input.Title,
		Description:     input.Description,
		CategorySlug:    input.CategorySlug,
		CoHostIDs:       coHostIDs,
		EventType:       input.EventType,
		Status:          input.Status,
		Visibility:      input.Visibility,
		City:            input.City,
		Country:         input.Country,
		VenueName:       input.VenueName,
		AddressLine1:    input.AddressLine1,
		AddressLine2:    input.AddressLine2,
		HowToFindUs:     input.HowToFindUs,
		OnlineURL:       input.OnlineURL,
		CoverImageURL:   input.CoverImageURL,
		StartsAt:        startsAt,
		EndsAt:          endsAt,
		Timezone:        input.Timezone,
		Latitude:        input.Latitude,
		Longitude:       input.Longitude,
		Capacity:        input.Capacity,
		WaitlistEnabled: input.WaitlistEnabled,
	})
	if err != nil {
		log.Printf("create meetup failed for user %s: %v", userID, err)
		response.Error(w, http.StatusInternalServerError, "could not create meetup")
		return
	}

	response.Success(w, http.StatusCreated, meetup)
}

func (h *Handler) UploadCoverImage(w http.ResponseWriter, r *http.Request) {
	if h.uploader == nil {
		response.Error(w, http.StatusInternalServerError, "image uploads are not configured")
		return
	}

	userID := middleware.CurrentUserID(r)
	if err := r.ParseMultipartForm(24 << 20); err != nil {
		response.Error(w, http.StatusBadRequest, "file too large or invalid form data")
		return
	}

	imageFile, err := readUploadedMeetupImage(r, "cover")
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	imageConfig, _, err := image.DecodeConfig(bytes.NewReader(imageFile.body))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "could not decode image")
		return
	}
	if imageConfig.Width <= 0 || imageConfig.Height <= 0 {
		response.Error(w, http.StatusBadRequest, "image dimensions are required")
		return
	}

	key := fmt.Sprintf("meetups/%s/%s%s", userID, uuid.New(), imageFile.extension)
	imageURL, err := h.uploader.Upload(r.Context(), key, imageFile.contentType, bytes.NewReader(imageFile.body))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not upload image")
		return
	}

	response.Success(w, http.StatusOK, map[string]string{"cover_image_url": imageURL})
}

func (h *Handler) UpdateMeetup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	meetupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meetup id")
		return
	}

	var input meetupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	input = normalizeMeetupInput(input)
	errs := validateMeetupInput(input)
	if len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}
	exists, err := h.categoryExists(r.Context(), input.CategorySlug)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not validate meetup category")
		return
	}
	if !exists {
		response.ValidationError(w, map[string]string{"category_slug": "invalid"})
		return
	}

	startsAt, err := parseMeetupStartsAt(input.StartsAt)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "starts_at must be ISO 8601 (e.g. 2026-05-01T19:00:00Z)")
		return
	}
	endsAt, err := parseMeetupEndsAt(input.EndsAt)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "ends_at must be ISO 8601 (e.g. 2026-05-01T21:00:00Z)")
		return
	}
	if msg := validateMeetupCapacity(input.Capacity); msg != "" {
		response.ValidationError(w, map[string]string{"capacity": msg})
		return
	}
	if msg := validateMeetupEndsAt(startsAt, endsAt); msg != "" {
		response.ValidationError(w, map[string]string{"ends_at": msg})
		return
	}
	coHostIDs, hostErrs := parseCoHostIDs(input.CoHostIDs)
	if len(hostErrs) > 0 {
		response.ValidationError(w, hostErrs)
		return
	}

	meetup, err := h.db.UpdateMeetup(r.Context(), meetupID, userID, UpdateMeetupInput{
		Title:           input.Title,
		Description:     input.Description,
		CategorySlug:    input.CategorySlug,
		CoHostIDs:       coHostIDs,
		EventType:       input.EventType,
		Status:          input.Status,
		Visibility:      input.Visibility,
		City:            input.City,
		Country:         input.Country,
		VenueName:       input.VenueName,
		AddressLine1:    input.AddressLine1,
		AddressLine2:    input.AddressLine2,
		HowToFindUs:     input.HowToFindUs,
		OnlineURL:       input.OnlineURL,
		CoverImageURL:   input.CoverImageURL,
		StartsAt:        startsAt,
		EndsAt:          endsAt,
		Timezone:        input.Timezone,
		Latitude:        input.Latitude,
		Longitude:       input.Longitude,
		Capacity:        input.Capacity,
		WaitlistEnabled: input.WaitlistEnabled,
	})
	if err != nil {
		log.Printf("update meetup failed for user %s on meetup %s: %v", userID, meetupID, err)
		status := http.StatusInternalServerError
		message := "could not update meetup"
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
			message = "meetup not found"
		} else if errors.Is(err, ErrForbidden) {
			status = http.StatusForbidden
			message = "forbidden"
		} else if errors.Is(err, ErrInvalidTransition) {
			status = http.StatusConflict
			message = "published events stay live when edited"
		}
		response.Error(w, status, message)
		return
	}

	response.Success(w, http.StatusOK, meetup)
}

func (h *Handler) DeleteMeetup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	meetupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meetup id")
		return
	}

	if err := h.db.DeleteMeetup(r.Context(), meetupID, userID); err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			response.Error(w, http.StatusNotFound, "meetup not found")
		case errors.Is(err, ErrForbidden):
			response.Error(w, http.StatusForbidden, "forbidden")
		case errors.Is(err, ErrDeleteNotAllowed):
			response.Error(w, http.StatusConflict, "event cannot be deleted while attendees remain")
		default:
			response.Error(w, http.StatusInternalServerError, "could not delete meetup")
		}
		return
	}

	response.Success(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h *Handler) categoryExists(ctx context.Context, slug string) (bool, error) {
	categories, err := h.db.ListCategories(ctx)
	if err != nil {
		log.Printf("meetup category lookup failed for slug %s: %v", slug, err)
		return false, err
	}
	for _, category := range categories {
		if category.Slug == slug {
			return true, nil
		}
	}
	return false, nil
}

func (h *Handler) PublishMeetup(w http.ResponseWriter, r *http.Request) {
	h.transitionMeetupStatus(w, r, "publish")
}

func (h *Handler) CancelMeetup(w http.ResponseWriter, r *http.Request) {
	h.transitionMeetupStatus(w, r, "cancel")
}

func (h *Handler) transitionMeetupStatus(w http.ResponseWriter, r *http.Request, action string) {
	userID := middleware.CurrentUserID(r)
	meetupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meetup id")
		return
	}

	var meetup *Meetup
	if action == "publish" {
		meetup, err = h.db.PublishMeetup(r.Context(), meetupID, userID)
	} else {
		meetup, err = h.db.CancelMeetup(r.Context(), meetupID, userID)
	}
	if err != nil {
		status := http.StatusInternalServerError
		message := "could not update meetup"
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
			message = "meetup not found"
		} else if errors.Is(err, ErrForbidden) {
			status = http.StatusForbidden
			message = "forbidden"
		}
		response.Error(w, status, message)
		return
	}

	response.Success(w, http.StatusOK, meetup)
}

type uploadedMeetupImage struct {
	body        []byte
	contentType string
	extension   string
}

func readUploadedMeetupImage(r *http.Request, fieldName string) (*uploadedMeetupImage, error) {
	file, header, err := r.FormFile(fieldName)
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			return nil, fmt.Errorf("%s field is required", fieldName)
		}
		return nil, fmt.Errorf("%s field is invalid", fieldName)
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return nil, errors.New("could not read image")
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(fileBytes)
	}

	switch contentType {
	case "image/jpeg", "image/png":
	default:
		return nil, errors.New("only jpeg and png images are supported")
	}

	if len(fileBytes) > 20<<20 {
		return nil, errors.New("image must be 20MB or smaller")
	}

	extension := filepath.Ext(header.Filename)
	if extension == "" {
		switch contentType {
		case "image/png":
			extension = ".png"
		default:
			extension = ".jpg"
		}
	}

	return &uploadedMeetupImage{
		body:        fileBytes,
		contentType: contentType,
		extension:   strings.ToLower(extension),
	}, nil
}

func (h *Handler) GetAttendees(w http.ResponseWriter, r *http.Request) {
	meetupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meetup id")
		return
	}

	limit := parseLimit(r, 50, 100)
	offset := decodeCursorToOffset(strings.TrimSpace(r.URL.Query().Get("cursor")))
	attendees, err := h.db.GetAttendees(r.Context(), meetupID, limit+1, offset)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch attendees")
		return
	}

	response.Success(w, http.StatusOK, sliceAttendees(attendees, limit, offset))
}

func (h *Handler) GetWaitlist(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	meetupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meetup id")
		return
	}

	limit := parseLimit(r, 50, 100)
	offset := decodeCursorToOffset(strings.TrimSpace(r.URL.Query().Get("cursor")))
	waitlist, err := h.db.GetWaitlist(r.Context(), meetupID, userID, limit+1, offset)
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			response.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not fetch waitlist")
		return
	}

	response.Success(w, http.StatusOK, sliceAttendees(waitlist, limit, offset))
}

func (h *Handler) RSVP(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	meetupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meetup id")
		return
	}

	result, err := h.db.ToggleRSVP(r.Context(), meetupID, userID)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			response.Error(w, http.StatusNotFound, "meetup not found")
		case errors.Is(err, ErrForbidden):
			response.Error(w, http.StatusForbidden, "forbidden")
		case errors.Is(err, ErrCapacityReached):
			response.Error(w, http.StatusConflict, "meetup is at capacity")
		default:
			response.Error(w, http.StatusInternalServerError, "could not update RSVP")
		}
		return
	}

	response.Success(w, http.StatusOK, result)
}

func parseDiscoverMeetupsParams(r *http.Request) DiscoverMeetupsParams {
	query := r.URL.Query()
	params := DiscoverMeetupsParams{
		Query:         strings.TrimSpace(query.Get("q")),
		CategorySlug:  strings.TrimSpace(strings.ToLower(query.Get("category"))),
		City:          strings.TrimSpace(query.Get("city")),
		EventType:     strings.TrimSpace(strings.ToLower(query.Get("event_type"))),
		DatePreset:    strings.TrimSpace(strings.ToLower(query.Get("date_preset"))),
		OpenSpotsOnly: parseBoolQuery(query.Get("open_spots_only")),
		Sort:          strings.TrimSpace(strings.ToLower(query.Get("sort"))),
		Cursor:        strings.TrimSpace(query.Get("cursor")),
		Limit:         parseLimit(r, 20, 50),
		DayOfWeek:     parseIntList(query.Get("day_of_week")),
		TimeOfDay:     parseStringList(query.Get("time_of_day")),
	}

	if params.Sort == "" {
		params.Sort = "recommended"
	}
	if !validEventSorts[params.Sort] {
		params.Sort = "recommended"
	}
	if !validEventTypes[params.EventType] {
		params.EventType = ""
	}
	if !validDatePresets[params.DatePreset] {
		params.DatePreset = ""
	}

	if distanceRaw := strings.TrimSpace(query.Get("distance_km")); distanceRaw != "" {
		if value, err := strconv.Atoi(distanceRaw); err == nil && value > 0 {
			params.DistanceKM = &value
		}
	}
	if dateRaw := strings.TrimSpace(query.Get("date_from")); dateRaw != "" {
		if value, err := time.Parse("2006-01-02", dateRaw); err == nil {
			params.DateFrom = &value
		}
	}
	if dateRaw := strings.TrimSpace(query.Get("date_to")); dateRaw != "" {
		if value, err := time.Parse("2006-01-02", dateRaw); err == nil {
			params.DateTo = &value
		}
	}

	filteredTimes := params.TimeOfDay[:0]
	for _, slot := range params.TimeOfDay {
		if validTimeOfDay[slot] {
			filteredTimes = append(filteredTimes, slot)
		}
	}
	params.TimeOfDay = filteredTimes

	filteredDays := params.DayOfWeek[:0]
	for _, day := range params.DayOfWeek {
		if day >= 0 && day <= 6 {
			filteredDays = append(filteredDays, day)
		}
	}
	params.DayOfWeek = filteredDays

	return params
}

func parseLimit(r *http.Request, defaultValue, maxValue int) int {
	value := strings.TrimSpace(r.URL.Query().Get("limit"))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return defaultValue
	}
	if parsed > maxValue {
		return maxValue
	}
	return parsed
}

func parseBoolQuery(raw string) bool {
	value := strings.TrimSpace(strings.ToLower(raw))
	return value == "1" || value == "true" || value == "yes"
}

func parseIntList(raw string) []int {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		parsed, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil {
			values = append(values, parsed)
		}
	}
	return values
}

func parseStringList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(strings.ToLower(part))
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func parseCoHostIDs(raw []string) ([]uuid.UUID, map[string]string) {
	if len(raw) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(raw))
	for index, value := range raw {
		parsed, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil {
			return nil, map[string]string{
				fmt.Sprintf("co_host_ids.%d", index): "invalid",
			}
		}
		ids = append(ids, parsed)
	}
	return ids, nil
}

func encodeOffsetCursor(offset int) *string {
	if offset <= 0 {
		return nil
	}
	encoded := base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
	return &encoded
}

func decodeCursorToOffset(cursor string) int {
	if cursor == "" {
		return 0
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0
	}
	value, err := strconv.Atoi(string(decoded))
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func sliceAttendees(attendees []Attendee, limit, offset int) CursorPage[Attendee] {
	hasMore := len(attendees) > limit
	items := attendees
	if hasMore {
		items = attendees[:limit]
	}
	nextCursor := (*string)(nil)
	if hasMore {
		nextCursor = encodeOffsetCursor(offset + limit)
	}
	return CursorPage[Attendee]{
		Items:      items,
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}
}
