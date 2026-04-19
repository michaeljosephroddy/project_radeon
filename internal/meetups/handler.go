package meetups

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/pagination"
	"github.com/project_radeon/api/pkg/response"
)

type Handler struct {
	db *pgxpool.Pool
}

// NewHandler builds a meetups handler backed by the shared database pool.
func NewHandler(db *pgxpool.Pool) *Handler {
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

type meetupInput struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
	City        string  `json:"city"`
	StartsAt    string  `json:"starts_at"`
	Capacity    *int    `json:"capacity"`
}

func (h *Handler) attachAttendeePreviews(r *http.Request, meetups []Meetup, limit int) error {
	if len(meetups) == 0 {
		return nil
	}

	meetupIDs := make([]uuid.UUID, 0, len(meetups))
	meetupIndex := make(map[uuid.UUID]*Meetup, len(meetups))
	for i := range meetups {
		meetupIDs = append(meetupIDs, meetups[i].ID)
		meetupIndex[meetups[i].ID] = &meetups[i]
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT meetup_id, id, username, avatar_url
		FROM (
			SELECT
				ma.meetup_id,
				u.id,
				u.username,
				u.avatar_url,
				ROW_NUMBER() OVER (PARTITION BY ma.meetup_id ORDER BY ma.rsvp_at ASC) AS rn
			FROM meetup_attendees ma
			JOIN users u ON u.id = ma.user_id
			WHERE ma.meetup_id = ANY($1)
		) attendee_preview
		WHERE rn <= $2
		ORDER BY meetup_id, rn`,
		meetupIDs, limit,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var meetupID uuid.UUID
		var attendee AttendeePreview
		if err := rows.Scan(&meetupID, &attendee.ID, &attendee.Username, &attendee.AvatarURL); err != nil {
			return err
		}
		if meetup := meetupIndex[meetupID]; meetup != nil {
			meetup.Attendees = append(meetup.Attendees, attendee)
		}
	}

	return rows.Err()
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

	rows, err := h.db.Query(r.Context(),
		`SELECT
			m.id,
			m.organiser_id,
			m.title,
			m.description,
			m.city,
			m.starts_at,
			m.capacity,
			m.attendee_count,
			EXISTS(
				SELECT 1 FROM meetup_attendees
				WHERE meetup_id = m.id AND user_id = $1
			) AS is_attending
		FROM meetups m
		WHERE m.starts_at > NOW()
			AND ($2 = '' OR m.city ILIKE $2)
			AND (
				$3 = ''
				OR m.title ILIKE $3
				OR COALESCE(m.description, '') ILIKE $3
			)
		ORDER BY m.starts_at ASC
		LIMIT $4 OFFSET $5`,
		userID, cityFilter, queryFilter, params.Limit+1, params.Offset,
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
	if err := h.attachAttendeePreviews(r, meetups, 3); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read meetup attendees")
		return
	}

	response.Success(w, http.StatusOK, pagination.Slice(meetups, params))
}

// ListMyMeetups returns the meetups organised by the authenticated user.
func (h *Handler) ListMyMeetups(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.Parse(r, 20, 50)

	rows, err := h.db.Query(r.Context(),
		`SELECT
			m.id,
			m.organiser_id,
			m.title,
			m.description,
			m.city,
			m.starts_at,
			m.capacity,
			m.attendee_count,
			EXISTS(
				SELECT 1 FROM meetup_attendees
				WHERE meetup_id = m.id AND user_id = $1
			) AS is_attending
		FROM meetups m
		WHERE m.organiser_id = $1
		ORDER BY m.starts_at ASC
		LIMIT $2 OFFSET $3`,
		userID, params.Limit+1, params.Offset,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch your meetups")
		return
	}
	defer rows.Close()

	var meetups []Meetup
	for rows.Next() {
		var meetup Meetup
		if err := rows.Scan(&meetup.ID, &meetup.OrganizerID, &meetup.Title, &meetup.Description, &meetup.City, &meetup.StartsAt, &meetup.Capacity, &meetup.AttendeeCt, &meetup.IsAttending); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not read your meetups")
			return
		}
		meetups = append(meetups, meetup)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read your meetups")
		return
	}
	if err := h.attachAttendeePreviews(r, meetups, 3); err != nil {
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
			m.attendee_count,
			EXISTS(
				SELECT 1 FROM meetup_attendees
				WHERE meetup_id = m.id AND user_id = $2
			) AS is_attending
		FROM meetups m
		WHERE m.id = $1`,
		meetupID, userID,
	).Scan(&meetup.ID, &meetup.OrganizerID, &meetup.Title, &meetup.Description, &meetup.City, &meetup.StartsAt, &meetup.Capacity, &meetup.AttendeeCt, &meetup.IsAttending)
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

	input.Title = strings.TrimSpace(input.Title)
	input.City = strings.TrimSpace(input.City)
	if input.Description != nil {
		description := strings.TrimSpace(*input.Description)
		input.Description = &description
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
	if input.Capacity != nil && *input.Capacity < 1 {
		response.ValidationError(w, map[string]string{"capacity": "must be greater than 0"})
		return
	}

	// Creation and the organiser RSVP happen together so newly created meetups
	// are immediately visible as events the organiser is attending.
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create meetup")
		return
	}
	defer tx.Rollback(r.Context())

	var meetup Meetup
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO meetups (
			organiser_id,
			title,
			description,
			city,
			starts_at,
			capacity
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING
			id,
			organiser_id,
			title,
			description,
			city,
			starts_at,
			capacity`,
		userID, input.Title, input.Description, input.City, startsAt, input.Capacity,
	).Scan(&meetup.ID, &meetup.OrganizerID, &meetup.Title, &meetup.Description, &meetup.City, &meetup.StartsAt, &meetup.Capacity); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create meetup")
		return
	}

	if _, err := tx.Exec(r.Context(),
		`INSERT INTO meetup_attendees (meetup_id, user_id) VALUES ($1, $2)`,
		meetup.ID, userID,
	); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create meetup")
		return
	}

	if _, err := tx.Exec(r.Context(),
		`UPDATE meetups SET attendee_count = 1 WHERE id = $1`,
		meetup.ID,
	); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create meetup")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create meetup")
		return
	}

	meetup.AttendeeCt = 1
	meetup.IsAttending = true

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
		ORDER BY ma.rsvp_at ASC
		LIMIT $2 OFFSET $3`,
		meetupID, params.Limit+1, params.Offset,
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

	var capacity *int
	var attendeeCount int
	if err := h.db.QueryRow(r.Context(),
		`SELECT capacity, attendee_count FROM meetups WHERE id = $1`,
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
			SELECT 1 FROM meetup_attendees
			WHERE meetup_id = $1 AND user_id = $2
		)`,
		meetupID, userID,
	).Scan(&alreadyRSVPd); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not check RSVP status")
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update RSVP")
		return
	}
	defer tx.Rollback(r.Context())

	if alreadyRSVPd {
		if _, err := tx.Exec(r.Context(),
			`DELETE FROM meetup_attendees WHERE meetup_id = $1 AND user_id = $2`,
			meetupID, userID,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not remove RSVP")
			return
		}
		if _, err := tx.Exec(r.Context(),
			`UPDATE meetups SET attendee_count = GREATEST(attendee_count - 1, 0) WHERE id = $1`,
			meetupID,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not remove RSVP")
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not remove RSVP")
			return
		}
		response.Success(w, http.StatusOK, map[string]bool{"attending": false})
	} else {
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO meetup_attendees (meetup_id, user_id) VALUES ($1, $2)`,
			meetupID, userID,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not RSVP")
			return
		}
		if _, err := tx.Exec(r.Context(),
			`UPDATE meetups SET attendee_count = attendee_count + 1 WHERE id = $1`,
			meetupID,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not RSVP")
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not RSVP")
			return
		}
		response.Success(w, http.StatusOK, map[string]bool{"attending": true})
	}
}
