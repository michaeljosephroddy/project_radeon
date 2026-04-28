package observability

import (
	"context"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type traceContextKey string

const (
	traceStartKey traceContextKey = "observability_trace_start"
	traceNameKey  traceContextKey = "observability_trace_name"
)

var whitespacePattern = regexp.MustCompile(`\s+`)

type PGXTracer struct {
	slowQueryThreshold time.Duration
}

func NewPGXTracer() *PGXTracer {
	return &PGXTracer{slowQueryThreshold: parseSlowQueryThreshold()}
}

func (t *PGXTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(
		context.WithValue(ctx, traceStartKey, time.Now()),
		traceNameKey,
		"db.query."+classifySQL(data.SQL),
	)
}

func (t *PGXTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	t.observeTrace(ctx, data.Err)
}

func (t *PGXTracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	return context.WithValue(
		context.WithValue(ctx, traceStartKey, time.Now()),
		traceNameKey,
		"db.batch",
	)
}

func (t *PGXTracer) TraceBatchQuery(_ context.Context, _ *pgx.Conn, data pgx.TraceBatchQueryData) {
	ObserveDuration("db.batch_query."+classifySQL(data.SQL), 0, data.Err)
	IncrementCounter("db.batch_query.count", 1)
}

func (t *PGXTracer) TraceBatchEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchEndData) {
	t.observeTrace(ctx, data.Err)
}

func (t *PGXTracer) TraceCopyFromStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	name := "db.copy_from." + sanitizeIdentifier(data.TableName.Sanitize())
	return context.WithValue(
		context.WithValue(ctx, traceStartKey, time.Now()),
		traceNameKey,
		name,
	)
}

func (t *PGXTracer) TraceCopyFromEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceCopyFromEndData) {
	t.observeTrace(ctx, data.Err)
}

func (t *PGXTracer) observeTrace(ctx context.Context, err error) {
	start, _ := ctx.Value(traceStartKey).(time.Time)
	name, _ := ctx.Value(traceNameKey).(string)
	if start.IsZero() || strings.TrimSpace(name) == "" {
		return
	}

	duration := time.Since(start)
	ObserveDuration(name, duration, err)
	IncrementCounter(name+".count", 1)

	if t.slowQueryThreshold > 0 && duration >= t.slowQueryThreshold {
		log.Printf("slow database operation: %s took %s (err=%v)", name, duration, err)
	}
}

func classifySQL(sql string) string {
	normalized := strings.TrimSpace(strings.ToLower(whitespacePattern.ReplaceAllString(sql, " ")))
	if normalized == "" {
		return "unknown"
	}

	tokens := strings.Fields(normalized)
	if len(tokens) == 0 {
		return "unknown"
	}

	verb := tokens[0]
	target := "unknown"
	switch verb {
	case "select", "delete":
		target = tokenAfter(tokens, "from")
	case "insert":
		target = tokenAfter(tokens, "into")
	case "update":
		if len(tokens) > 1 {
			target = sanitizeIdentifier(tokens[1])
		}
	case "with":
		target = "cte"
	case "copy":
		if len(tokens) > 1 {
			target = sanitizeIdentifier(tokens[1])
		}
	}
	return verb + "." + target
}

func tokenAfter(tokens []string, marker string) string {
	for index := 0; index < len(tokens)-1; index++ {
		if tokens[index] == marker {
			return sanitizeIdentifier(tokens[index+1])
		}
	}
	return "unknown"
}

func sanitizeIdentifier(value string) string {
	cleaned := strings.TrimSpace(value)
	cleaned = strings.Trim(cleaned, `"`)
	cleaned = strings.Trim(cleaned, ",)")
	cleaned = strings.TrimPrefix(cleaned, "(")
	if cleaned == "" {
		return "unknown"
	}
	return strings.ReplaceAll(cleaned, ".", "_")
}

func parseSlowQueryThreshold() time.Duration {
	raw := strings.TrimSpace(os.Getenv("OBSERVABILITY_SLOW_QUERY_MS"))
	if raw == "" {
		return 250 * time.Millisecond
	}
	parsed, err := time.ParseDuration(raw + "ms")
	if err != nil {
		return 250 * time.Millisecond
	}
	return parsed
}
