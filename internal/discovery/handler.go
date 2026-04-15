package discovery

import (
	"context"
	"fmt"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/response"
)

// VectorRebuilder is satisfied by *Handler and consumed by internal/interests.
type VectorRebuilder interface {
	RebuildVector(ctx context.Context, userID uuid.UUID) error
}

// CacheInvalidator is satisfied by *Handler and consumed by internal/user.
type CacheInvalidator interface {
	InvalidateSuggestions(userID uuid.UUID)
}

// DiscoveryUpdater combines both interfaces for packages that need both
// (e.g. internal/interests, which rebuilds the vector and invalidates the cache).
type DiscoveryUpdater interface {
	VectorRebuilder
	CacheInvalidator
}

type Handler struct {
	db    *pgxpool.Pool
	cache *suggestionCache
}

// ScoredUser is a candidate returned by the suggestions endpoint.
type ScoredUser struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	AvatarURL *string   `json:"avatar_url"`
	City      *string   `json:"city"`
	Score     float64   `json:"score"`
}

func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{db: db, cache: newSuggestionCache()}
}

// RebuildVector satisfies VectorRebuilder.
func (h *Handler) RebuildVector(ctx context.Context, userID uuid.UUID) error {
	return rebuildVector(ctx, h.db, userID)
}

// InvalidateSuggestions satisfies CacheInvalidator.
func (h *Handler) InvalidateSuggestions(userID uuid.UUID) {
	h.cache.invalidate(userID)
}

// GET /users/suggestions
func (h *Handler) GetSuggestions(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	if cached, ok := h.cache.get(userID); ok {
		response.Success(w, http.StatusOK, cached)
		return
	}

	suggestions, err := h.buildSuggestions(r.Context(), userID, 20)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch suggestions")
		return
	}

	h.cache.set(userID, suggestions)
	response.Success(w, http.StatusOK, suggestions)
}

// POST /users/{id}/dismiss
func (h *Handler) DismissUser(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	targetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}

	if userID == targetID {
		response.Error(w, http.StatusBadRequest, "cannot dismiss yourself")
		return
	}

	if _, err := h.db.Exec(r.Context(),
		`INSERT INTO dismissed_users (user_id, dismissed_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, targetID,
	); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not dismiss user")
		return
	}

	h.cache.invalidate(userID)
	response.Success(w, http.StatusOK, map[string]bool{"dismissed": true})
}

type userProfile struct {
	id        uuid.UUID
	firstName string
	lastName  string
	avatarURL *string
	city      *string
	lat       *float64
	lng       *float64
	vec       []float64
}

func (h *Handler) buildSuggestions(ctx context.Context, userID uuid.UUID, topN int) ([]ScoredUser, error) {
	var target userProfile
	var radiusKm int
	if err := h.db.QueryRow(ctx,
		`SELECT id, first_name, last_name, avatar_url, city, lat, lng, interest_vec, discovery_radius_km
		 FROM users WHERE id = $1`, userID,
	).Scan(
		&target.id, &target.firstName, &target.lastName, &target.avatarURL,
		&target.city, &target.lat, &target.lng, &target.vec, &radiusKm,
	); err != nil {
		return nil, fmt.Errorf("fetch target user: %w", err)
	}

	rows, err := h.db.Query(ctx,
		`SELECT u.id, u.first_name, u.last_name, u.avatar_url, u.city, u.lat, u.lng, u.interest_vec
		 FROM users u
		 WHERE u.id != $1
		   AND u.interest_vec IS NOT NULL
		   AND NOT EXISTS (
		       SELECT 1 FROM connections c
		       WHERE (c.requester_id = $1 AND c.addressee_id = u.id)
		          OR (c.addressee_id = $1 AND c.requester_id = u.id)
		   )
		   AND NOT EXISTS (
		       SELECT 1 FROM dismissed_users d
		       WHERE d.user_id = $1 AND d.dismissed_id = u.id
		   )`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("fetch candidates: %w", err)
	}
	defer rows.Close()

	type scored struct {
		user  ScoredUser
		score float64
	}

	var results []scored
	for rows.Next() {
		var c userProfile
		if err := rows.Scan(
			&c.id, &c.firstName, &c.lastName, &c.avatarURL,
			&c.city, &c.lat, &c.lng, &c.vec,
		); err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		score := computeScore(target.vec, target.lat, target.lng, c.vec, c.lat, c.lng, float64(radiusKm))
		results = append(results, scored{
			user:  ScoredUser{ID: c.id, FirstName: c.firstName, LastName: c.lastName, AvatarURL: c.avatarURL, City: c.city},
			score: score,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate candidates: %w", err)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})
	if len(results) > topN {
		results = results[:topN]
	}

	suggestions := make([]ScoredUser, len(results))
	for i, r := range results {
		suggestions[i] = r.user
		suggestions[i].Score = r.score
	}
	return suggestions, nil
}
