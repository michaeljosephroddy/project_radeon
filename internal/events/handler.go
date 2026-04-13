package events

import (
	"encoding/json"
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

type Event struct {
	ID          uuid.UUID `json:"id"`
	OrganizerID uuid.UUID `json:"organizer_id"`
	Title       string    `json:"title"`
	Description *string   `json:"description"`
	City        string    `json:"city"`
	StartsAt    time.Time `json:"starts_at"`
	Capacity    *int      `json:"capacity"`
	AttendeeCt  int       `json:"attendee_count"`
	IsAttending bool      `json:"is_attending"`
}

// GET /events?city=Dublin
func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	city := r.URL.Query().Get("city")

	rows, err := h.db.Query(r.Context(),
		`SELECT e.id, e.organiser_id, e.title, e.description, e.city, e.starts_at, e.capacity,
		   COUNT(ea.user_id) AS attendee_count,
		   BOOL_OR(ea.user_id = $1) AS is_attending
		 FROM events e
		 LEFT JOIN event_attendees ea ON ea.event_id = e.id
		 WHERE e.starts_at > NOW()
		 AND ($2 = '' OR e.city ILIKE $2)
		 GROUP BY e.id
		 ORDER BY e.starts_at ASC
		 LIMIT 50`,
		userID, city,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch events")
		return
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		rows.Scan(&e.ID, &e.OrganizerID, &e.Title, &e.Description, &e.City, &e.StartsAt, &e.Capacity, &e.AttendeeCt, &e.IsAttending)
		events = append(events, e)
	}

	response.Success(w, http.StatusOK, events)
}

// GET /events/{id}
func (h *Handler) GetEvent(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	eventID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid event id")
		return
	}

	var e Event
	err = h.db.QueryRow(r.Context(),
		`SELECT 
    e.id, e.organiser_id, e.title, e.description, e.city, e.starts_at, e.capacity,
    COUNT(ea.user_id) AS attendee_count,
    COALESCE(BOOL_OR(ea.user_id = $2), false) AS is_attending
FROM events e
LEFT JOIN event_attendees ea ON ea.event_id = e.id
WHERE e.id = $1
GROUP BY e.id`, eventID, userID,
	).Scan(&e.ID, &e.OrganizerID, &e.Title, &e.Description, &e.City, &e.StartsAt, &e.Capacity, &e.AttendeeCt, &e.IsAttending)
	if err != nil {
		response.Error(w, http.StatusNotFound, "event not found")
		return
	}

	response.Success(w, http.StatusOK, e)
}

// POST /events
func (h *Handler) CreateEvent(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Title       string  `json:"title"`
		Description *string `json:"description"`
		City        string  `json:"city"`
		StartsAt    string  `json:"starts_at"` // ISO 8601
		Capacity    *int    `json:"capacity"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	errs := map[string]string{}
	if input.Title == "" {
		errs["title"] = "required"
	}
	if input.City == "" {
		errs["city"] = "required"
	}
	if input.StartsAt == "" {
		errs["starts_at"] = "required"
	}
	if len(errs) > 0 {
		response.ValidationError(w, errs)
		return
	}

	startsAt, err := time.Parse(time.RFC3339, input.StartsAt)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "starts_at must be ISO 8601 (e.g. 2025-06-01T19:00:00Z)")
		return
	}

	var eventID uuid.UUID
	h.db.QueryRow(r.Context(),
		`INSERT INTO events (organiser_id, title, description, city, starts_at, capacity)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		userID, input.Title, input.Description, input.City, startsAt, input.Capacity,
	).Scan(&eventID)

	response.Success(w, http.StatusCreated, map[string]any{"id": eventID})
}

// GET /events/{id}/attendees
func (h *Handler) GetAttendees(w http.ResponseWriter, r *http.Request) {
	eventID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid event id")
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT u.id, u.first_name, u.last_name, u.avatar_url, u.city, ea.rsvp_at
		 FROM event_attendees ea
		 JOIN users u ON u.id = ea.user_id
		 WHERE ea.event_id = $1
		 ORDER BY ea.rsvp_at ASC`, eventID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch attendees")
		return
	}
	defer rows.Close()

	type Attendee struct {
		ID        uuid.UUID `json:"id"`
		FirstName string    `json:"first_name"`
		LastName  string    `json:"last_name"`
		AvatarURL *string   `json:"avatar_url"`
		City      *string   `json:"city"`
		RSVPAt    time.Time `json:"rsvp_at"`
	}

	var attendees []Attendee
	for rows.Next() {
		var a Attendee
		rows.Scan(&a.ID, &a.FirstName, &a.LastName, &a.AvatarURL, &a.City, &a.RSVPAt)
		attendees = append(attendees, a)
	}

	response.Success(w, http.StatusOK, attendees)
}

// POST /events/{id}/rsvp
func (h *Handler) RSVP(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	eventID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid event id")
		return
	}

	// Check capacity
	var capacity *int
	var attendeeCount int
	h.db.QueryRow(r.Context(),
		`SELECT e.capacity, COUNT(ea.user_id)
		 FROM events e
		 LEFT JOIN event_attendees ea ON ea.event_id = e.id
		 WHERE e.id = $1
		 GROUP BY e.capacity`, eventID,
	).Scan(&capacity, &attendeeCount)

	if capacity != nil && attendeeCount >= *capacity {
		response.Error(w, http.StatusConflict, "event is at capacity")
		return
	}

	// Toggle RSVP
	var alreadyRSVPd bool
	h.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM event_attendees WHERE event_id=$1 AND user_id=$2)`,
		eventID, userID,
	).Scan(&alreadyRSVPd)

	if alreadyRSVPd {
		h.db.Exec(r.Context(),
			`DELETE FROM event_attendees WHERE event_id=$1 AND user_id=$2`, eventID, userID,
		)
		response.Success(w, http.StatusOK, map[string]bool{"attending": false})
	} else {
		h.db.Exec(r.Context(),
			`INSERT INTO event_attendees (event_id, user_id) VALUES ($1, $2)`, eventID, userID,
		)
		response.Success(w, http.StatusOK, map[string]bool{"attending": true})
	}
}
