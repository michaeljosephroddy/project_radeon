package pagination

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Params captures normalized page/limit inputs for page-based list endpoints.
type Params struct {
	Page   int
	Limit  int
	Offset int
}

// Response wraps a page of items plus enough metadata for incremental loading.
type Response[T any] struct {
	Items   []T  `json:"items"`
	Page    int  `json:"page"`
	Limit   int  `json:"limit"`
	HasMore bool `json:"has_more"`
}

// Parse gives list endpoints one shared pagination policy so page/limit bounds
// stay consistent across feed, chats, support, meetups, and friendships.
func Parse(r *http.Request, defaultLimit, maxLimit int) Params {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	return Params{
		Page:   page,
		Limit:  limit,
		Offset: (page - 1) * limit,
	}
}

// Slice turns a limit+1 SQL result into a page payload without a second count
// query, which is enough for infinite-scroll style clients.
func Slice[T any](items []T, params Params) Response[T] {
	hasMore := len(items) > params.Limit
	if hasMore {
		items = items[:params.Limit]
	}

	return Response[T]{
		Items:   items,
		Page:    params.Page,
		Limit:   params.Limit,
		HasMore: hasMore,
	}
}

// CursorParams holds parsed cursor inputs for time-ordered list endpoints.
// Before is used by DESC-ordered endpoints (feed, support, friends).
// After is used by ASC-ordered endpoints (comments).
type CursorParams struct {
	Limit  int
	Before *time.Time
	After  *time.Time
}

// CursorResponse wraps a cursor-paginated page. NextCursor is the value to
// pass as ?before or ?after on the next request to fetch the following page.
type CursorResponse[T any] struct {
	Items      []T     `json:"items"`
	Limit      int     `json:"limit"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor,omitempty"`
}

// ParseCursor reads ?limit, ?before, and ?after from the request and returns
// normalized CursorParams. Timestamps must be RFC3339Nano.
func ParseCursor(r *http.Request, defaultLimit, maxLimit int) CursorParams {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	params := CursorParams{Limit: limit}

	if raw := strings.TrimSpace(r.URL.Query().Get("before")); raw != "" {
		if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			params.Before = &t
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("after")); raw != "" {
		if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			params.After = &t
		}
	}

	return params
}

// CursorSlice trims a limit+1 result to limit items and derives the next
// cursor from the last item's timestamp. Works for both ASC and DESC ordering
// since the next cursor is always the boundary item regardless of direction.
func CursorSlice[T any](items []T, limit int, timestamp func(T) time.Time) CursorResponse[T] {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	var nextCursor *string
	if hasMore && len(items) > 0 {
		t := timestamp(items[len(items)-1]).UTC().Format(time.RFC3339Nano)
		nextCursor = &t
	}

	return CursorResponse[T]{
		Items:      items,
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}
}
