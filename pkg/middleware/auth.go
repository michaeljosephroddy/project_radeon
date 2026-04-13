package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/project_radeon/api/internal/auth"
	"github.com/project_radeon/api/pkg/response"
	"github.com/google/uuid"
)

type contextKey string

const UserIDKey contextKey = "userID"

// Authenticate validates the JWT and injects the user ID into the request context.
func Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			response.Error(w, http.StatusUnauthorized, "missing or invalid authorization header")
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := auth.ParseToken(tokenString)
		if err != nil {
			response.Error(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CurrentUserID extracts the authenticated user's ID from context.
func CurrentUserID(r *http.Request) uuid.UUID {
	return r.Context().Value(UserIDKey).(uuid.UUID)
}
