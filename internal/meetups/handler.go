package meetups

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/pagination"
	"github.com/project_radeon/api/pkg/response"
)

// Querier is the database interface required by the meetups handler.
type Querier interface {
	ListMeetups(ctx context.Context, userID uuid.UUID, cityFilter, queryFilter string, limit, offset int) ([]Meetup, error)
	ListMyMeetups(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Meetup, error)
	AttachAttendeePreviews(ctx context.Context, meetups []Meetup, previewLimit int) error
	GetMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error)
	CreateMeetup(ctx context.Context, userID uuid.UUID, title string, description *string, city string, startsAt time.Time, capacity *int) (*Meetup, error)
	GetAttendees(ctx context.Context, meetupID uuid.UUID, limit, offset int) ([]Attendee, error)
	GetMeetupCapacity(ctx context.Context, meetupID uuid.UUID) (capacity *int, attendeeCount int, err error)
	IsRSVPd(ctx context.Context, meetupID, userID uuid.UUID) (bool, error)
	AddRSVP(ctx context.Context, meetupID, userID uuid.UUID) error
	RemoveRSVP(ctx context.Context, meetupID, userID uuid.UUID) error
}

type Handler struct {
	db Querier
}

// NewHandler builds a meetups handler. Pass meetups.NewPgStore(pool) for production.
func NewHandler(db Querier) *Handler {
	return &Handler{db: db}
}

type Meetup struct {
	ID          uuid.UUID         `json:"id"`
	OrganizerID uuid.UUID         `json:"organizer_id"`
	Title       string            `json:"title"`
	Description *string           `json:"description"`
	City        string            `json:"city"`
	StartsAt    time.Time         `json:"starts_at"`
	Capacity    *int              `json:"capacity"`
	AttendeeCt  int               `json:"attendee_count"`
	IsAttending bool              `json:"is_attending"`
	Attendees   []AttendeePreview `json:"attendee_preview,omitempty"`
}

type AttendeePreview struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url"`
}

type Attendee struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url"`
	City      *string   `json:"city"`
	RSVPAt    time.Time `json:"rsvp_at"`
}

type meetupInput struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
	City        string  `json:"city"`
	StartsAt    string  `json:"starts_at"`
	Capacity    *int    `json:"capacity"`
}

// ListMeetups returns upcoming meetups, optionally filtered by city, with attendee state for the caller.
func (h *Handler) ListMeetups(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	city := strings.TrimSpace(r.URL.Query().Get("city"))
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	cityFilter := ""
	queryFilter := ""
	if city != "" {
		cityFilter = "%" + city + "%"
	}
	if query != "" {
		queryFilter = "%" + query + "%"
	}
	params := pagination.Parse(r, 20, 50)

	meetups, err := h.db.ListMeetups(r.Context(), userID, cityFilter, queryFilter, params.Limit+1, params.Offset)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch meetups")
		return
	}
	if err := h.db.AttachAttendeePreviews(r.Context(), meetups, 3); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read meetup attendees")
		return
	}

	response.Success(w, http.StatusOK, pagination.Slice(meetups, params))
}

// ListMyMeetups returns the meetups organised by the authenticated user.
func (h *Handler) ListMyMeetups(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.Parse(r, 20, 50)

	meetups, err := h.db.ListMyMeetups(r.Context(), userID, params.Limit+1, params.Offset)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch your meetups")
		return
	}
	if err := h.db.AttachAttendeePreviews(r.Context(), meetups, 3); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read meetup attendees")
		return
	}

	response.Success(w, http.StatusOK, pagination.Slice(meetups, params))
}

// GetMeetup returns the full details for a single meetup and the caller's RSVP state.
func (h *Handler) GetMeetup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	meetupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meetup id")
		return
	}

	meetup, err := h.db.GetMeetup(r.Context(), meetupID, userID)
	if err != nil {
		response.Error(w, http.StatusNotFound, "meetup not found")
		return
	}

	response.Success(w, http.StatusOK, meetup)
}

// CreateMeetup validates meetup input and inserts a new meetup owned by the caller.
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

	startsAt, err := parseMeetupStartsAt(input.StartsAt)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "starts_at must be ISO 8601 (e.g. 2025-06-01T19:00:00Z)")
		return
	}
	if msg := validateMeetupCapacity(input.Capacity); msg != "" {
		response.ValidationError(w, map[string]string{"capacity": msg})
		return
	}

	// Creation and the organiser RSVP happen together so newly created meetups
	// are immediately visible as events the organiser is attending.
	meetup, err := h.db.CreateMeetup(r.Context(), userID, input.Title, input.Description, input.City, startsAt, input.Capacity)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create meetup")
		return
	}

	response.Success(w, http.StatusCreated, meetup)
}

// GetAttendees returns a paginated list of users who have RSVP'd to a meetup.
func (h *Handler) GetAttendees(w http.ResponseWriter, r *http.Request) {
	meetupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meetup id")
		return
	}

	params := pagination.Parse(r, 50, 100)

	attendees, err := h.db.GetAttendees(r.Context(), meetupID, params.Limit+1, params.Offset)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch attendees")
		return
	}

	response.Success(w, http.StatusOK, pagination.Slice(attendees, params))
}

// RSVP toggles the caller's attendance for a meetup while enforcing capacity limits.
func (h *Handler) RSVP(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	meetupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meetup id")
		return
	}

	capacity, attendeeCount, err := h.db.GetMeetupCapacity(r.Context(), meetupID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(w, http.StatusNotFound, "meetup not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "could not fetch meetup")
		return
	}
	if capacity != nil && attendeeCount >= *capacity {
		response.Error(w, http.StatusConflict, "meetup is at capacity")
		return
	}

	alreadyRSVPd, err := h.db.IsRSVPd(r.Context(), meetupID, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not check RSVP status")
		return
	}

	if alreadyRSVPd {
		if err := h.db.RemoveRSVP(r.Context(), meetupID, userID); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not remove RSVP")
			return
		}
		response.Success(w, http.StatusOK, map[string]bool{"attending": false})
	} else {
		if err := h.db.AddRSVP(r.Context(), meetupID, userID); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not RSVP")
			return
		}
		response.Success(w, http.StatusOK, map[string]bool{"attending": true})
	}
}
