package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	appcache "github.com/project_radeon/api/pkg/cache"
	"golang.org/x/time/rate"

	"github.com/project_radeon/api/pkg/response"
)

type rateLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type limiterStore struct {
	mu      sync.Mutex
	entries map[string]*rateLimiter
	r       rate.Limit
	burst   int
}

func newLimiterStore(r rate.Limit, burst int) *limiterStore {
	s := &limiterStore{
		entries: make(map[string]*rateLimiter),
		r:       r,
		burst:   burst,
	}
	// Evict stale entries every minute to bound memory growth.
	go func() {
		ticker := time.NewTicker(time.Minute)
		for range ticker.C {
			s.mu.Lock()
			for k, v := range s.entries {
				if time.Since(v.lastSeen) > 3*time.Minute {
					delete(s.entries, k)
				}
			}
			s.mu.Unlock()
		}
	}()
	return s
}

func (s *limiterStore) get(key string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok {
		entry = &rateLimiter{limiter: rate.NewLimiter(s.r, s.burst)}
		s.entries[key] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter
}

// ipStore limits unauthenticated and public traffic by IP address.
// 60 requests/minute with a burst of 20 to absorb brief spikes.
var ipStore = newLimiterStore(rate.Every(time.Second), 20)

// userStore limits authenticated traffic per user ID.
// 120 requests/minute with a burst of 30.
var userStore = newLimiterStore(2*rate.Every(time.Second), 30)

// RateLimitIP enforces a per-IP request rate and is applied globally so
// unauthenticated endpoints (register, login) are also protected.
func RateLimitIP(next http.Handler) http.Handler {
	return RateLimitIPWithStore(nil)(next)
}

func RateLimitIPWithStore(store appcache.Store) func(http.Handler) http.Handler {
	var shared sharedFixedWindowLimiter
	if limiter, ok := store.(sharedFixedWindowLimiter); ok {
		shared = limiter
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if shared != nil {
				// Prefer the shared limiter in deployments with Redis so autoscaled
				// instances enforce one consistent window.
				allowed, err := shared.AllowFixedWindow(r.Context(), rateLimitKey("ip", ip), 60, time.Minute)
				if err == nil {
					if !allowed {
						response.Error(w, http.StatusTooManyRequests, "too many requests")
						return
					}
					next.ServeHTTP(w, r)
					return
				}
			}

			if !ipStore.get(ip).Allow() {
				response.Error(w, http.StatusTooManyRequests, "too many requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitUser enforces a per-authenticated-user request rate. It must be
// placed inside the Authenticate middleware group so UserIDKey is available.
func RateLimitUser(next http.Handler) http.Handler {
	return RateLimitUserWithStore(nil)(next)
}

func RateLimitUserWithStore(store appcache.Store) func(http.Handler) http.Handler {
	var shared sharedFixedWindowLimiter
	if limiter, ok := store.(sharedFixedWindowLimiter); ok {
		shared = limiter
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := r.Context().Value(UserIDKey)
			if userID == nil {
				next.ServeHTTP(w, r)
				return
			}

			userKey := userID.(interface{ String() string }).String()
			if shared != nil {
				// Fall back to the in-process limiter only if shared storage is
				// unavailable so local development still works without Redis.
				allowed, err := shared.AllowFixedWindow(r.Context(), rateLimitKey("user", userKey), 120, time.Minute)
				if err == nil {
					if !allowed {
						response.Error(w, http.StatusTooManyRequests, "too many requests")
						return
					}
					next.ServeHTTP(w, r)
					return
				}
			}

			if !userStore.get(userKey).Allow() {
				response.Error(w, http.StatusTooManyRequests, "too many requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type sharedFixedWindowLimiter interface {
	AllowFixedWindow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

func rateLimitKey(scope, subject string) string {
	return "ratelimit:" + scope + ":" + subject
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		// Only trust the first syntactically valid forwarded hop.
		first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
		if parsed := net.ParseIP(first); parsed != nil {
			return parsed.String()
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		if parsed := net.ParseIP(realIP); parsed != nil {
			return parsed.String()
		}
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
