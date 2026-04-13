package connections

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

type Connection struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	FirstName   string    `json:"first_name"`
	LastName    string    `json:"last_name"`
	AvatarURL   *string   `json:"avatar_url"`
	City        *string   `json:"city"`
	Status      string    `json:"status"`
	ConnectedAt time.Time `json:"connected_at"`
}

// POST /connections — send a connection request
func (h *Handler) SendRequest(w http.ResponseWriter, r *http.Request) {
	requesterID := middleware.CurrentUserID(r)

	var input struct {
		AddresseeID uuid.UUID `json:"addressee_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if requesterID == input.AddresseeID {
		response.Error(w, http.StatusBadRequest, "cannot connect with yourself")
		return
	}

	// Prevent duplicate requests in either direction
	var exists bool
	h.db.QueryRow(r.Context(),
		`SELECT EXISTS(
		   SELECT 1 FROM connections
		   WHERE (requester_id=$1 AND addressee_id=$2)
		   OR (requester_id=$2 AND addressee_id=$1)
		 )`, requesterID, input.AddresseeID,
	).Scan(&exists)

	if exists {
		response.Error(w, http.StatusConflict, "connection already exists")
		return
	}

	var connID uuid.UUID
	h.db.QueryRow(r.Context(),
		`INSERT INTO connections (requester_id, addressee_id, status)
		 VALUES ($1, $2, 'pending') RETURNING id`,
		requesterID, input.AddresseeID,
	).Scan(&connID)

	response.Success(w, http.StatusCreated, map[string]any{"id": connID, "status": "pending"})
}

// PATCH /connections/{id} — accept or decline
func (h *Handler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	connID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid connection id")
		return
	}

	var input struct {
		Status string `json:"status"`
	}
	json.NewDecoder(r.Body).Decode(&input)

	if input.Status != "accepted" && input.Status != "declined" {
		response.Error(w, http.StatusBadRequest, "status must be 'accepted' or 'declined'")
		return
	}

	if input.Status == "declined" {
		// Delete the row entirely so the requester can try again later
		result, err := h.db.Exec(r.Context(),
			`DELETE FROM connections
             WHERE id=$1 AND addressee_id=$2 AND status='pending'`,
			connID, userID,
		)
		if err != nil || result.RowsAffected() == 0 {
			response.Error(w, http.StatusNotFound, "pending connection not found")
			return
		}
		response.Success(w, http.StatusOK, map[string]string{"status": "declined"})
		return
	}

	// accepted — update as before
	result, err := h.db.Exec(r.Context(),
		`UPDATE connections SET status=$1
         WHERE id=$2 AND addressee_id=$3 AND status='pending'`,
		input.Status, connID, userID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if result.RowsAffected() == 0 {
		response.Error(w, http.StatusNotFound, "pending connection not found")
		return
	}
	response.Success(w, http.StatusOK, map[string]string{"status": input.Status})
}

// GET /connections — list accepted connections for current user
func (h *Handler) ListConnections(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	rows, err := h.db.Query(r.Context(),
		`SELECT c.id,
		   CASE WHEN c.requester_id=$1 THEN c.addressee_id ELSE c.requester_id END AS other_id,
		   u.first_name, u.last_name, u.avatar_url, u.city,
		   c.status, c.created_at
		 FROM connections c
		 JOIN users u ON u.id = CASE WHEN c.requester_id=$1 THEN c.addressee_id ELSE c.requester_id END
		 WHERE (c.requester_id=$1 OR c.addressee_id=$1)
		 AND c.status='accepted'
		 ORDER BY c.created_at DESC`, userID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch connections")
		return
	}
	defer rows.Close()

	var conns []Connection
	for rows.Next() {
		var c Connection
		rows.Scan(&c.ID, &c.UserID, &c.FirstName, &c.LastName, &c.AvatarURL, &c.City, &c.Status, &c.ConnectedAt)
		conns = append(conns, c)
	}

	response.Success(w, http.StatusOK, conns)
}

// GET /connections/pending — incoming requests waiting on you
func (h *Handler) ListPending(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	rows, err := h.db.Query(r.Context(),
		`SELECT c.id, c.requester_id, u.first_name, u.last_name, u.avatar_url, u.city, c.status, c.created_at
		 FROM connections c
		 JOIN users u ON u.id = c.requester_id
		 WHERE c.addressee_id=$1 AND c.status='pending'
		 ORDER BY c.created_at DESC`, userID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch pending requests")
		return
	}
	defer rows.Close()

	var conns []Connection
	for rows.Next() {
		var c Connection
		rows.Scan(&c.ID, &c.UserID, &c.FirstName, &c.LastName, &c.AvatarURL, &c.City, &c.Status, &c.ConnectedAt)
		conns = append(conns, c)
	}

	response.Success(w, http.StatusOK, conns)
}

// DELETE /connections/{id} — remove a connection (either party can remove)
func (h *Handler) RemoveConnection(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	connID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid connection id")
		return
	}

	// Only allow deletion if the current user is one of the two parties
	result, err := h.db.Exec(r.Context(),
		`DELETE FROM connections
		 WHERE id = $1
		 AND (requester_id = $2 OR addressee_id = $2)`,
		connID, userID,
	)
	if err != nil || result.RowsAffected() == 0 {
		response.Error(w, http.StatusNotFound, "connection not found")
		return
	}

	response.Success(w, http.StatusOK, map[string]bool{"removed": true})
}
