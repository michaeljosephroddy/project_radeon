package dating

import (
	"errors"
	"time"

	"github.com/google/uuid"
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
	ErrProfileIncomplete = errors.New("dating profile incomplete")
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

type DatingPhoto struct {
	ID        uuid.UUID `json:"id"`
	ImageURL  string    `json:"image_url"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
}

type DatingProfile struct {
	ID                  uuid.UUID     `json:"id"`
	UserID              uuid.UUID     `json:"user_id,omitempty"`
	Username            string        `json:"username"`
	Age                 *int          `json:"age,omitempty"`
	City                *string       `json:"city,omitempty"`
	Country             *string       `json:"country,omitempty"`
	Bio                 *string       `json:"bio,omitempty"`
	RelationshipGoal    string        `json:"relationship_goal"`
	InterestedInGenders []string      `json:"interested_in_genders"`
	HeightCm            *int          `json:"height_cm,omitempty"`
	Work                *string       `json:"work,omitempty"`
	Education           *string       `json:"education,omitempty"`
	KidsStatus          string        `json:"kids_status"`
	Interests           []string      `json:"interests"`
	AgeMin              int           `json:"age_min"`
	AgeMax              int           `json:"age_max"`
	DistanceKm          int           `json:"distance_km"`
	Paused              bool          `json:"paused"`
	CompletedAt         *time.Time    `json:"completed_at,omitempty"`
	Photos              []DatingPhoto `json:"photos"`
	CreatedAt           time.Time     `json:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
}

type UpdateProfileInput struct {
	Bio                 *string
	RelationshipGoal    *string
	InterestedInGenders []string
	ReplaceGenders      bool
	HeightCm            *int
	ReplaceHeight       bool
	Work                *string
	Education           *string
	KidsStatus          *string
	Interests           []string
	ReplaceInterests    bool
	AgeMin              *int
	AgeMax              *int
	DistanceKm          *int
	Paused              *bool
	Complete            bool
}

type DatingMatch struct {
	ID          uuid.UUID     `json:"id"`
	Profile     DatingProfile `json:"profile"`
	ChatID      *uuid.UUID    `json:"chat_id,omitempty"`
	Status      string        `json:"status"`
	MatchedAt   time.Time     `json:"matched_at"`
	UnmatchedAt *time.Time    `json:"unmatched_at,omitempty"`
}

type DatingLike struct {
	Profile DatingProfile
	LikedAt time.Time
}

type ActionResult struct {
	Action  string       `json:"action"`
	Matched bool         `json:"matched"`
	Match   *DatingMatch `json:"match,omitempty"`
}
