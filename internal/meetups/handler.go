package meetups

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

type Meetup struct {
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

// GET /meetups?city=Dublin
func (h *Handler) ListMeetups(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	city := r.URL.Query().Get("city")

	rows, err := h.db.Query(r.Context(),
		`SELECT
			m.id,
			m.organiser_id,
			m.title,
			m.description,
			m.city,
			m.starts_at,
			m.capacity,
			COUNT(ma.user_id) AS attendee_count,
			COALESCE(BOOL_OR(ma.user_id = $1), false) AS is_attending
		FROM meetups m
		LEFT JOIN meetup_attendees ma ON ma.meetup_id = m.id
		WHERE m.starts_at > NOW()
			AND ($2 = '' OR m.city ILIKE $2)
		GROUP BY m.id
		ORDER BY m.starts_at ASC
		LIMIT 50`,
		userID, city,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch meetups")
		return
	}
	defer rows.Close()

	var meetups []Meetup
	for rows.Next() {
		var meetup Meetup
		if err := rows.Scan(&meetup.ID, &meetup.OrganizerID, &meetup.Title, &meetup.Description, &meetup.City, &meetup.StartsAt, &meetup.Capacity, &meetup.AttendeeCt, &meetup.IsAttending); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not read meetups")
			return
		}
		meetups = append(meetups, meetup)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read meetups")
		return
	}

	response.Success(w, http.StatusOK, meetups)
}

// GET /meetups/{id}
func (h *Handler) GetMeetup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	meetupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meetup id")
		return
	}

	var meetup Meetup
	err = h.db.QueryRow(r.Context(),
		`SELECT
			m.id,
			m.organiser_id,
			m.title,
			m.description,
			m.city,
			m.starts_at,
			m.capacity,
			COUNT(ma.user_id) AS attendee_count,
			COALESCE(BOOL_OR(ma.user_id = $2), false) AS is_attending
		FROM meetups m
		LEFT JOIN meetup_attendees ma ON ma.meetup_id = m.id
		WHERE m.id = $1
		GROUP BY m.id`,
		meetupID, userID,
	).Scan(&meetup.ID, &meetup.OrganizerID, &meetup.Title, &meetup.Description, &meetup.City, &meetup.StartsAt, &meetup.Capacity, &meetup.AttendeeCt, &meetup.IsAttending)
	if err != nil {
		response.Error(w, http.StatusNotFound, "meetup not found")
		return
	}

	response.Success(w, http.StatusOK, meetup)
}

// POST /meetups
func (h *Handler) CreateMeetup(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Title       string  `json:"title"`
		Description *string `json:"description"`
		City        string  `json:"city"`
		StartsAt    string  `json:"starts_at"`
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

	var meetupID uuid.UUID
	if err := h.db.QueryRow(r.Context(),
		`INSERT INTO meetups (
			organiser_id,
			title,
			description,
			city,
			starts_at,
			capacity
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		userID, input.Title, input.Description, input.City, startsAt, input.Capacity,
	).Scan(&meetupID); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create meetup")
		return
	}

	response.Success(w, http.StatusCreated, map[string]any{"id": meetupID})
}

// GET /meetups/{id}/attendees
func (h *Handler) GetAttendees(w http.ResponseWriter, r *http.Request) {
	meetupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meetup id")
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT
			u.id,
			u.username,
			u.avatar_url,
			u.city,
			ma.rsvp_at
		FROM meetup_attendees ma
		JOIN users u ON u.id = ma.user_id
		WHERE ma.meetup_id = $1
		ORDER BY ma.rsvp_at ASC`,
		meetupID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch attendees")
		return
	}
	defer rows.Close()

	type Attendee struct {
		ID        uuid.UUID `json:"id"`
		Username  string    `json:"username"`
		AvatarURL *string   `json:"avatar_url"`
		City      *string   `json:"city"`
		RSVPAt    time.Time `json:"rsvp_at"`
	}

	var attendees []Attendee
	for rows.Next() {
		var attendee Attendee
		if err := rows.Scan(&attendee.ID, &attendee.Username, &attendee.AvatarURL, &attendee.City, &attendee.RSVPAt); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not read attendees")
			return
		}
		attendees = append(attendees, attendee)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read attendees")
		return
	}

	response.Success(w, http.StatusOK, attendees)
}

// POST /meetups/{id}/rsvp
func (h *Handler) RSVP(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	meetupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid meetup id")
		return
	}

	var capacity *int
	var attendeeCount int
	if err := h.db.QueryRow(r.Context(),
		`SELECT
			m.capacity,
			COUNT(ma.user_id)
		FROM meetups m
		LEFT JOIN meetup_attendees ma ON ma.meetup_id = m.id
		WHERE m.id = $1
		GROUP BY m.capacity`,
		meetupID,
	).Scan(&capacity, &attendeeCount); err != nil {
		response.Error(w, http.StatusNotFound, "meetup not found")
		return
	}

	if capacity != nil && attendeeCount >= *capacity {
		response.Error(w, http.StatusConflict, "meetup is at capacity")
		return
	}

	var alreadyRSVPd bool
	if err := h.db.QueryRow(r.Context(),
		`SELECT EXISTS(
			SELECT 1
			FROM meetup_attendees
			WHERE meetup_id = $1
				AND user_id = $2
		)`,
		meetupID, userID,
	).Scan(&alreadyRSVPd); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not check RSVP status")
		return
	}

	if alreadyRSVPd {
		if _, err := h.db.Exec(r.Context(),
			`DELETE FROM meetup_attendees
			WHERE meetup_id = $1
				AND user_id = $2`,
			meetupID, userID,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not remove RSVP")
			return
		}
		response.Success(w, http.StatusOK, map[string]bool{"attending": false})
	} else {
		if _, err := h.db.Exec(r.Context(),
			`INSERT INTO meetup_attendees (
				meetup_id,
				user_id
			)
			VALUES ($1, $2)`,
			meetupID, userID,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not RSVP")
			return
		}
		response.Success(w, http.StatusOK, map[string]bool{"attending": true})
	}
}
