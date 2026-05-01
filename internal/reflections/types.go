package reflections

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrValidation = errors.New("validation failed")
)

type DailyReflection struct {
	ID             uuid.UUID  `json:"id"`
	UserID         uuid.UUID  `json:"user_id"`
	ReflectionDate string     `json:"reflection_date"`
	PromptKey      *string    `json:"prompt_key,omitempty"`
	PromptText     *string    `json:"prompt_text,omitempty"`
	GratefulFor    *string    `json:"grateful_for,omitempty"`
	OnMind         *string    `json:"on_mind,omitempty"`
	BlockingToday  *string    `json:"blocking_today,omitempty"`
	Body           string     `json:"body"`
	SharedPostID   *uuid.UUID `json:"shared_post_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type UpsertDailyReflectionInput struct {
	PromptKey     *string
	PromptText    *string
	GratefulFor   *string
	OnMind        *string
	BlockingToday *string
	Body          string
}

type UpdateDailyReflectionInput struct {
	PromptKey     **string
	PromptText    **string
	GratefulFor   **string
	OnMind        **string
	BlockingToday **string
	Body          *string
}

type ListResponse struct {
	Items      []DailyReflection `json:"items"`
	Limit      int               `json:"limit"`
	HasMore    bool              `json:"has_more"`
	NextCursor *string           `json:"next_cursor,omitempty"`
}
