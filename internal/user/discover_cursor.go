package user

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/pagination"
)

const discoverCursorVersion = "v1"

type discoverCursor struct {
	Version    string `json:"v"`
	Mode       string `json:"mode"`
	LastID     string `json:"last_id,omitempty"`
	LastOffset int    `json:"last_offset,omitempty"`
	Rank       *int   `json:"rank,omitempty"`
	CreatedAt  string `json:"created_at,omitempty"`
}

func sliceDiscoverUsers(users []User, params DiscoverUsersParams) pagination.CursorResponse[User] {
	limit := params.DisplayLimit
	if limit < 1 {
		limit = 20
	}
	hasMore := len(users) > limit
	items := users
	if hasMore {
		items = users[:limit]
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		if params.Query != "" {
			nextCursor = encodeDiscoverSearchCursor(last, params.Query)
		} else {
			nextCursor = encodeDiscoverRankedCursor(last.ID, params.Offset+len(items))
		}
	}
	return pagination.CursorResponse[User]{
		Items:      items,
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}
}

func encodeDiscoverRankedCursor(lastID uuid.UUID, lastOffset int) *string {
	if lastID == uuid.Nil {
		return nil
	}
	return encodeDiscoverCursor(discoverCursor{
		Version:    discoverCursorVersion,
		Mode:       "ranked",
		LastID:     lastID.String(),
		LastOffset: lastOffset,
	})
}

func encodeDiscoverSearchCursor(user User, query string) *string {
	if user.ID == uuid.Nil {
		return nil
	}
	rank := discoverSearchRank(user.Username, query)
	return encodeDiscoverCursor(discoverCursor{
		Version:   discoverCursorVersion,
		Mode:      "search",
		LastID:    user.ID.String(),
		Rank:      &rank,
		CreatedAt: user.CreatedAt.UTC().Format(time.RFC3339Nano),
	})
}

func encodeDiscoverCursor(cursor discoverCursor) *string {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return nil
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return &encoded
}

func decodeDiscoverCursor(raw string) discoverCursor {
	if raw == "" {
		return discoverCursor{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return discoverCursor{}
	}
	var cursor discoverCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return discoverCursor{}
	}
	if cursor.Version != discoverCursorVersion {
		return discoverCursor{}
	}
	return cursor
}

func discoverSearchRank(candidateUsername, query string) int {
	candidate := strings.ToLower(strings.TrimSpace(candidateUsername))
	needle := strings.ToLower(strings.TrimSpace(query))
	if candidate == needle {
		return 0
	}
	if strings.HasPrefix(candidate, needle) {
		return 1
	}
	return 2
}
