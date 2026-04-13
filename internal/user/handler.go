package user

import (
	"context"
	"encoding/json"
	"log"
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

type User struct {
	ID         uuid.UUID  `json:"id"`
	FirstName  string     `json:"first_name"`
	LastName   string     `json:"last_name"`
	AvatarURL  *string    `json:"avatar_url"`
	City       *string    `json:"city"`
	Country    *string    `json:"country"`
	SoberSince *time.Time `json:"sober_since"`
	CreatedAt  time.Time  `json:"created_at"`
	Interests  []string   `json:"interests"`
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
		FirstName  *string `json:"first_name"`
		LastName   *string `json:"last_name"`
		City       *string `json:"city"`
		Country    *string `json:"country"`
		AvatarURL  *string `json:"avatar_url"`
		SoberSince *string `json:"sober_since"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Only update fields that are present in the request
	_, err := h.db.Exec(r.Context(),
		`UPDATE users SET
			first_name  = COALESCE($1, first_name),
			last_name   = COALESCE($2, last_name),
			city        = COALESCE($3, city),
			country     = COALESCE($4, country),
			avatar_url  = COALESCE($5, avatar_url)
		WHERE id = $6`,
		input.FirstName, input.LastName, input.City, input.Country, input.AvatarURL, userID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update profile")
		return
	}

	user, _ := h.fetchUser(r.Context(), userID)
	response.Success(w, http.StatusOK, user)
}

// GET /users/discover?city=Dublin
func (h *Handler) Discover(w http.ResponseWriter, r *http.Request) {
	currentUserID := middleware.CurrentUserID(r)
	log.Printf("%s", currentUserID)
	city := r.URL.Query().Get("city")
	log.Printf("discover called: city=%q", city)

	rows, err := h.db.Query(r.Context(),
		`SELECT u.id, u.first_name, u.last_name, u.avatar_url, u.city, u.country, u.sober_since, u.created_at
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
		rows.Scan(&u.ID, &u.FirstName, &u.LastName, &u.AvatarURL, &u.City, &u.Country, &u.SoberSince, &u.CreatedAt)
		u.Interests = h.fetchInterests(r.Context(), u.ID)
		users = append(users, u)
	}

	response.Success(w, http.StatusOK, users)
}

// --- helpers ---

func (h *Handler) fetchUser(ctx context.Context, id uuid.UUID) (*User, error) {
	var u User
	err := h.db.QueryRow(ctx,
		`SELECT id, first_name, last_name, avatar_url, city, country, sober_since, created_at
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.FirstName, &u.LastName, &u.AvatarURL, &u.City, &u.Country, &u.SoberSince, &u.CreatedAt)
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
