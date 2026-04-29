package observability

import (
	"sync"
	"time"
)

type timerStats struct {
	Count      int64
	ErrorCount int64
	TotalNS    int64
	MinNS      int64
	MaxNS      int64
}

type TimerSnapshot struct {
	Count      int64   `json:"count"`
	ErrorCount int64   `json:"error_count"`
	TotalMS    float64 `json:"total_ms"`
	AvgMS      float64 `json:"avg_ms"`
	MinMS      float64 `json:"min_ms"`
	MaxMS      float64 `json:"max_ms"`
}

type Snapshot struct {
	GeneratedAt   time.Time                `json:"generated_at"`
	UptimeSeconds float64                  `json:"uptime_seconds"`
	Counters      map[string]int64         `json:"counters"`
	Gauges        map[string]int64         `json:"gauges"`
	Timers        map[string]TimerSnapshot `json:"timers"`
}

type Registry struct {
	mu       sync.Mutex
	started  time.Time
	counters map[string]int64
	gauges   map[string]int64
	timers   map[string]*timerStats
}

func newRegistry() *Registry {
	return &Registry{
		started:  time.Now().UTC(),
		counters: make(map[string]int64),
		gauges:   make(map[string]int64),
		timers:   make(map[string]*timerStats),
	}
}

var defaultRegistry = newRegistry()

func IncrementCounter(name string, delta int64) {
	defaultRegistry.IncrementCounter(name, delta)
}

func AddGauge(name string, delta int64) {
	defaultRegistry.AddGauge(name, delta)
}

func ObserveDuration(name string, duration time.Duration, err error) {
	defaultRegistry.ObserveDuration(name, duration, err)
}

func SnapshotNow() Snapshot {
	return defaultRegistry.Snapshot()
}

func (r *Registry) IncrementCounter(name string, delta int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[name] += delta
}

func (r *Registry) AddGauge(name string, delta int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges[name] += delta
}

func (r *Registry) ObserveDuration(name string, duration time.Duration, err error) {
	ns := duration.Nanoseconds()

	r.mu.Lock()
	defer r.mu.Unlock()

	stats, ok := r.timers[name]
	if !ok {
		stats = &timerStats{MinNS: ns, MaxNS: ns}
		r.timers[name] = stats
	}
	stats.Count++
	stats.TotalNS += ns
	if err != nil {
		stats.ErrorCount++
	}
	if ns < stats.MinNS {
		stats.MinNS = ns
	}
	if ns > stats.MaxNS {
		stats.MaxNS = ns
	}
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	counters := make(map[string]int64, len(r.counters))
	for key, value := range r.counters {
		counters[key] = value
	}

	gauges := make(map[string]int64, len(r.gauges))
	for key, value := range r.gauges {
		gauges[key] = value
	}

	timers := make(map[string]TimerSnapshot, len(r.timers))
	for key, stats := range r.timers {
		avgMS := 0.0
		if stats.Count > 0 {
			avgMS = float64(stats.TotalNS) / float64(stats.Count) / float64(time.Millisecond)
		}
		timers[key] = TimerSnapshot{
			Count:      stats.Count,
			ErrorCount: stats.ErrorCount,
			TotalMS:    float64(stats.TotalNS) / float64(time.Millisecond),
			AvgMS:      avgMS,
			MinMS:      float64(stats.MinNS) / float64(time.Millisecond),
			MaxMS:      float64(stats.MaxNS) / float64(time.Millisecond),
		}
	}

	return Snapshot{
		GeneratedAt:   time.Now().UTC(),
		UptimeSeconds: time.Since(r.started).Seconds(),
		Counters:      counters,
		Gauges:        gauges,
		Timers:        timers,
	}
}
