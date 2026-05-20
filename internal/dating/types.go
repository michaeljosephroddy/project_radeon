package dating

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/project_radeon/api/internal/chats"
	"github.com/project_radeon/api/internal/user"
)

const (
	ActionLike = "like"
	ActionPass = "pass"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrForbidden         = errors.New("forbidden")
	ErrConflict          = errors.New("conflict")
	ErrDatingDisabled    = errors.New("dating disabled")
	ErrTargetUnavailable = errors.New("target unavailable")
)

type DiscoverParams struct {
	CurrentUserID     uuid.UUID
	Gender            string
	AgeMin            *int
	AgeMax            *int
	DistanceKm        *int
	Sobriety          string
	Interests         []string
	Lat               *float64
	Lng               *float64
	Cursor            string
	CursorOffset      int
	CursorRequestID   string
	RankedWindowLimit int
	SourceWindowLimit int
	Limit             int
}

type PreviewResponse struct {
	ExactCount int `json:"exact_count"`
}

type DatingMatch struct {
	ID          uuid.UUID  `json:"id"`
	User        user.User  `json:"user"`
	ChatID      *uuid.UUID `json:"chat_id,omitempty"`
	Status      string     `json:"status"`
	MatchedAt   time.Time  `json:"matched_at"`
	UnmatchedAt *time.Time `json:"unmatched_at,omitempty"`
}

type DatingLike struct {
	User    user.User
	LikedAt time.Time
}

type ActionResult struct {
	Action  string       `json:"action"`
	Matched bool         `json:"matched"`
	Match   *DatingMatch `json:"match,omitempty"`
	Chat    *chats.Chat  `json:"chat,omitempty"`
}
