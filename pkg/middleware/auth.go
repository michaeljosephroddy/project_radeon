package middleware

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/project_radeon/api/internal/auth"
	"github.com/project_radeon/api/pkg/response"
)

type contextKey string

const UserIDKey contextKey = "userID"

type cachedToken struct {
	claims    *auth.Claims
	expiresAt time.Time
}

var (
	tokenCacheMu sync.RWMutex
	tokenCache   = make(map[string]cachedToken)
)

func init() {
	// Evict expired entries every two minutes to bound memory growth.
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		for range ticker.C {
			now := time.Now()
			tokenCacheMu.Lock()
			for k, v := range tokenCache {
				if v.expiresAt.Before(now) {
					delete(tokenCache, k)
				}
			}
			tokenCacheMu.Unlock()
		}
	}()
}

// Authenticate validates the bearer token and injects the authenticated user ID into the request context.
func Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			response.Error(w, http.StatusUnauthorized, "missing or invalid authorization header")
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		tokenCacheMu.RLock()
		cached, hit := tokenCache[tokenString]
		tokenCacheMu.RUnlock()

		var claims *auth.Claims
		if hit && cached.expiresAt.After(time.Now()) {
			claims = cached.claims
		} else {
			var err error
			claims, err = auth.ParseToken(tokenString)
			if err != nil {
				response.Error(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}
			// Cache for 60 s, or until the token's own expiry if sooner.
			cacheUntil := time.Now().Add(60 * time.Second)
			if claims.ExpiresAt != nil && claims.ExpiresAt.Time.Before(cacheUntil) {
				cacheUntil = claims.ExpiresAt.Time
			}
			tokenCacheMu.Lock()
			tokenCache[tokenString] = cachedToken{claims: claims, expiresAt: cacheUntil}
			tokenCacheMu.Unlock()
		}

		ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CurrentUserID extracts the authenticated user's ID from request context.
func CurrentUserID(r *http.Request) uuid.UUID {
	return r.Context().Value(UserIDKey).(uuid.UUID)
}
