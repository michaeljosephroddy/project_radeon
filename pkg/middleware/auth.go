package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project_radeon/api/internal/auth"
	"github.com/project_radeon/api/pkg/response"
)

type contextKey string

const UserIDKey contextKey = "userID"

type UserChecker interface {
	UserExists(ctx context.Context, userID uuid.UUID) (bool, error)
}

type pgUserChecker struct {
	pool *pgxpool.Pool
}

func NewPGUserChecker(pool *pgxpool.Pool) UserChecker {
	return &pgUserChecker{pool: pool}
}

func (c *pgUserChecker) UserExists(ctx context.Context, userID uuid.UUID) (bool, error) {
	var exists bool
	if err := c.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)
	`, userID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// Authenticate validates the bearer token and injects the authenticated user ID into the request context.
func Authenticate(next http.Handler) http.Handler {
	return authenticateRequest(next, false)
}

// AuthenticateWebSocket accepts either a bearer header or access_token query
// parameter so browser websocket clients can authenticate during the upgrade.
func AuthenticateWebSocket(next http.Handler) http.Handler {
	return authenticateRequest(next, true)
}

func authenticateRequest(next http.Handler, allowQueryToken bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenString, ok := extractAccessToken(r, allowQueryToken)
		if !ok {
			response.Error(w, http.StatusUnauthorized, "missing or invalid authorization header")
			return
		}

		claims, err := auth.ParseToken(tokenString)
		if err != nil {
			response.Error(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func extractAccessToken(r *http.Request, allowQueryToken bool) (string, bool) {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if token != "" {
			return token, true
		}
	}

	if allowQueryToken {
		token := strings.TrimSpace(r.URL.Query().Get("access_token"))
		if token != "" {
			return token, true
		}
	}

	return "", false
}

// EnsureCurrentUserExists rejects requests authenticated with a token whose
// user no longer exists, such as after a local reseed that truncates users.
func EnsureCurrentUserExists(checker UserChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userIDValue := r.Context().Value(UserIDKey)
			userID, ok := userIDValue.(uuid.UUID)
			if !ok {
				response.Error(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}
			exists, err := checker.UserExists(r.Context(), userID)
			if err != nil {
				response.Error(w, http.StatusInternalServerError, "could not validate session")
				return
			}
			if !exists {
				response.Error(w, http.StatusUnauthorized, "session expired, please sign in again")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CurrentUserID extracts the authenticated user's ID from request context.
func CurrentUserID(r *http.Request) uuid.UUID {
	return r.Context().Value(UserIDKey).(uuid.UUID)
}
