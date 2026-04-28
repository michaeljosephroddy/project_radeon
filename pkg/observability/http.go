package observability

import (
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

type DBPoolSnapshot struct {
	AcquireCount            int64   `json:"acquire_count"`
	AcquireDurationMS       float64 `json:"acquire_duration_ms"`
	AcquiredConns           int32   `json:"acquired_conns"`
	CanceledAcquireCount    int64   `json:"canceled_acquire_count"`
	ConstructingConns       int32   `json:"constructing_conns"`
	EmptyAcquireCount       int64   `json:"empty_acquire_count"`
	IdleConns               int32   `json:"idle_conns"`
	MaxConns                int32   `json:"max_conns"`
	TotalConns              int32   `json:"total_conns"`
	NewConnsCount           int64   `json:"new_conns_count"`
	MaxLifetimeDestroyCount int64   `json:"max_lifetime_destroy_count"`
	MaxIdleDestroyCount     int64   `json:"max_idle_destroy_count"`
}

type RuntimeSnapshot struct {
	Goroutines  int    `json:"goroutines"`
	HeapAlloc   uint64 `json:"heap_alloc"`
	HeapSys     uint64 `json:"heap_sys"`
	HeapObjects uint64 `json:"heap_objects"`
}

type HTTPSnapshot struct {
	Snapshot
	DB           *DBPoolSnapshot `json:"db,omitempty"`
	Runtime      RuntimeSnapshot `json:"runtime"`
	CacheEnabled bool            `json:"cache_enabled"`
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWebSocketUpgrade(r) {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		AddGauge("http.in_flight", 1)
		defer AddGauge("http.in_flight", -1)

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		route := routePattern(r)
		metricName := "http." + r.Method + " " + route
		var err error
		if recorder.status >= http.StatusInternalServerError {
			err = http.ErrAbortHandler
		}
		ObserveDuration(metricName, time.Since(start), err)
		IncrementCounter("http.status."+http.StatusText(recorder.status), 1)
	})
}

func Handler(pool *pgxpool.Pool, cacheEnabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		snapshot := HTTPSnapshot{
			Snapshot:     SnapshotNow(),
			Runtime:      runtimeSnapshot(),
			CacheEnabled: cacheEnabled,
		}
		if pool != nil {
			stats := pool.Stat()
			snapshot.DB = &DBPoolSnapshot{
				AcquireCount:            stats.AcquireCount(),
				AcquireDurationMS:       stats.AcquireDuration().Seconds() * 1000,
				AcquiredConns:           stats.AcquiredConns(),
				CanceledAcquireCount:    stats.CanceledAcquireCount(),
				ConstructingConns:       stats.ConstructingConns(),
				EmptyAcquireCount:       stats.EmptyAcquireCount(),
				IdleConns:               stats.IdleConns(),
				MaxConns:                stats.MaxConns(),
				TotalConns:              stats.TotalConns(),
				NewConnsCount:           stats.NewConnsCount(),
				MaxLifetimeDestroyCount: stats.MaxLifetimeDestroyCount(),
				MaxIdleDestroyCount:     stats.MaxIdleDestroyCount(),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snapshot)
	}
}

func routePattern(r *http.Request) string {
	if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
		if pattern := strings.TrimSpace(routeContext.RoutePattern()); pattern != "" {
			return pattern
		}
	}
	if path := strings.TrimSpace(r.URL.Path); path != "" {
		return path
	}
	return "unknown"
}

func isWebSocketUpgrade(r *http.Request) bool {
	connection := strings.ToLower(r.Header.Get("Connection"))
	upgrade := strings.ToLower(r.Header.Get("Upgrade"))
	return strings.Contains(connection, "upgrade") && upgrade == "websocket"
}

func runtimeSnapshot() RuntimeSnapshot {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return RuntimeSnapshot{
		Goroutines:  runtime.NumGoroutine(),
		HeapAlloc:   mem.HeapAlloc,
		HeapSys:     mem.HeapSys,
		HeapObjects: mem.HeapObjects,
	}
}
