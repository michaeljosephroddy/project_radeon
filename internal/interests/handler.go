package interests

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/response"
)

// DiscoveryUpdater is implemented by *discovery.Handler. Defined here so
// internal/interests does not import internal/discovery.
type DiscoveryUpdater interface {
	RebuildVector(ctx context.Context, userID uuid.UUID) error
	InvalidateSuggestions(userID uuid.UUID)
}

type Handler struct {
	db        *pgxpool.Pool
	discovery DiscoveryUpdater
}

func NewHandler(db *pgxpool.Pool, discovery DiscoveryUpdater) *Handler {
	return &Handler{db: db, discovery: discovery}
}

// GET /interests — returns the full fixed list
func (h *Handler) ListInterests(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(), `SELECT id, name FROM interests ORDER BY name ASC`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch interests")
		return
	}
	defer rows.Close()

	type Interest struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	}

	var interests []Interest
	for rows.Next() {
		var i Interest
		rows.Scan(&i.ID, &i.Name)
		interests = append(interests, i)
	}

	response.Success(w, http.StatusOK, interests)
}

// PUT /users/me/interests — replaces the user's interests entirely
func (h *Handler) SetUserInterests(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		InterestIDs []uuid.UUID `json:"interest_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Replace in a transaction
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "transaction error")
		return
	}
	defer tx.Rollback(r.Context())

	if _, err := tx.Exec(r.Context(), `DELETE FROM user_interests WHERE user_id = $1`, userID); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update interests")
		return
	}

	for _, id := range input.InterestIDs {
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO user_interests (user_id, interest_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			userID, id,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not update interests")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not save interests")
		return
	}

	// Rebuild this user's interest vector and invalidate their cached suggestions
	// asynchronously so the response is not blocked. context.Background() is
	// intentional — the request context will be cancelled before the goroutine finishes.
	go func() {
		if err := h.discovery.RebuildVector(context.Background(), userID); err != nil {
			log.Printf("vector rebuild failed for user %s: %v", userID, err)
		}
		h.discovery.InvalidateSuggestions(userID)
	}()

	response.Success(w, http.StatusOK, map[string]bool{"updated": true})
}
