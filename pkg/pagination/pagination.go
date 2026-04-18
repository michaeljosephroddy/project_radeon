package pagination

import (
	"net/http"
	"strconv"
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
