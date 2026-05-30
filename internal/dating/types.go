package dating

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	ActionLike = "like"
	ActionPass = "pass"
)

var (
	ErrNotFound           = errors.New("not found")
	ErrForbidden          = errors.New("forbidden")
	ErrConflict           = errors.New("conflict")
	ErrPlusRequired       = errors.New("plus required")
	ErrDailyLikeLimit     = errors.New("daily dating like limit reached")
	ErrSpotlightRequired  = errors.New("dating spotlight inventory required")
	ErrDatingDisabled     = errors.New("dating disabled")
	ErrTargetUnavailable  = errors.New("target unavailable")
	ErrProfileIncomplete  = errors.New("dating profile incomplete")
	ErrInvalidDatingEvent = errors.New("invalid dating event")
)

type DiscoverParams struct {
	CurrentUserID     uuid.UUID
	Gender            string
	AgeMin            *int
	AgeMax            *int
	DistanceKm        *int
	Sobriety          string
	Interests         []string
	RelationshipGoal  string
	HeightMinCm       *int
	HeightMaxCm       *int
	FamilyPlans       string
	DrinkingStatus    string
	SmokingStatus     string
	DrugUseStatus     string
	SoberLifestyle    string
	RecoveryApproach  string
	NightlifeComfort  string
	SubstanceBoundary string
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

const (
	SpotlightKindStandard = "spotlight"
	SpotlightKindSuper    = "super_spotlight"
)

type ActiveSpotlight struct {
	ID          uuid.UUID `json:"id"`
	InventoryID uuid.UUID `json:"inventory_id"`
	Kind        string    `json:"kind"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
}

type SpotlightInventorySummary struct {
	Spotlights      int `json:"spotlights"`
	SuperSpotlights int `json:"super_spotlights"`
}

type SpotlightStatus struct {
	Inventory SpotlightInventorySummary `json:"inventory"`
	Active    *ActiveSpotlight          `json:"active"`
}

type DatingPhoto struct {
	ID        uuid.UUID `json:"id"`
	ImageURL  string    `json:"image_url"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
}

type DatingPromptAnswer struct {
	ID        uuid.UUID `json:"id"`
	PromptKey string    `json:"prompt_key"`
	Answer    string    `json:"answer"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type DatingProfile struct {
	ID                  uuid.UUID            `json:"id"`
	UserID              uuid.UUID            `json:"user_id,omitempty"`
	Username            string               `json:"username"`
	Age                 *int                 `json:"age,omitempty"`
	City                *string              `json:"city,omitempty"`
	Country             *string              `json:"country,omitempty"`
	Bio                 *string              `json:"bio,omitempty"`
	RelationshipGoal    string               `json:"relationship_goal"`
	InterestedInGenders []string             `json:"interested_in_genders"`
	HeightCm            *int                 `json:"height_cm,omitempty"`
	JobTitle            *string              `json:"job_title,omitempty"`
	Company             *string              `json:"company,omitempty"`
	Work                *string              `json:"work,omitempty"`
	School              *string              `json:"school,omitempty"`
	Course              *string              `json:"course,omitempty"`
	Education           *string              `json:"education,omitempty"`
	KidsStatus          string               `json:"kids_status"`
	ChildrenStatus      string               `json:"children_status"`
	RelationshipType    string               `json:"relationship_type"`
	Gender              string               `json:"gender"`
	Sexuality           string               `json:"sexuality"`
	Pronouns            string               `json:"pronouns"`
	Ethnicity           string               `json:"ethnicity"`
	Pets                string               `json:"pets"`
	ReligiousBelief     string               `json:"religious_belief"`
	LanguagesSpoken     []string             `json:"languages_spoken"`
	PoliticalView       string               `json:"political_view"`
	DrinkingStatus      string               `json:"drinking_status"`
	SmokingStatus       string               `json:"smoking_status"`
	DrugUseStatus       string               `json:"drug_use_status"`
	Zodiac              string               `json:"zodiac"`
	FamilyPlans         string               `json:"family_plans"`
	CommunicationStyle  string               `json:"communication_style"`
	LoveStyle           string               `json:"love_style"`
	Workout             string               `json:"workout"`
	SocialMedia         string               `json:"social_media"`
	SoberLifestyle      string               `json:"sober_lifestyle"`
	RecoveryApproach    string               `json:"recovery_approach"`
	NightlifeComfort    string               `json:"nightlife_comfort"`
	SubstanceBoundaries string               `json:"substance_boundaries"`
	Interests           []string             `json:"interests"`
	AgeMin              int                  `json:"age_min"`
	AgeMax              int                  `json:"age_max"`
	DistanceKm          int                  `json:"distance_km"`
	Paused              bool                 `json:"paused"`
	CompletedAt         *time.Time           `json:"completed_at,omitempty"`
	Photos              []DatingPhoto        `json:"photos"`
	PromptAnswers       []DatingPromptAnswer `json:"prompt_answers"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
}

type PublicDatingProfile struct {
	ID                  uuid.UUID            `json:"id"`
	UserID              uuid.UUID            `json:"user_id,omitempty"`
	Username            string               `json:"username"`
	Age                 *int                 `json:"age,omitempty"`
	City                *string              `json:"city,omitempty"`
	Country             *string              `json:"country,omitempty"`
	Bio                 *string              `json:"bio,omitempty"`
	RelationshipGoal    string               `json:"relationship_goal"`
	HeightCm            *int                 `json:"height_cm,omitempty"`
	JobTitle            *string              `json:"job_title,omitempty"`
	Company             *string              `json:"company,omitempty"`
	Work                *string              `json:"work,omitempty"`
	School              *string              `json:"school,omitempty"`
	Course              *string              `json:"course,omitempty"`
	Education           *string              `json:"education,omitempty"`
	KidsStatus          string               `json:"kids_status"`
	ChildrenStatus      string               `json:"children_status"`
	RelationshipType    string               `json:"relationship_type"`
	Gender              string               `json:"gender"`
	Sexuality           string               `json:"sexuality"`
	Pronouns            string               `json:"pronouns"`
	Ethnicity           string               `json:"ethnicity"`
	Pets                string               `json:"pets"`
	ReligiousBelief     string               `json:"religious_belief"`
	LanguagesSpoken     []string             `json:"languages_spoken"`
	PoliticalView       string               `json:"political_view"`
	DrinkingStatus      string               `json:"drinking_status"`
	SmokingStatus       string               `json:"smoking_status"`
	DrugUseStatus       string               `json:"drug_use_status"`
	Zodiac              string               `json:"zodiac"`
	FamilyPlans         string               `json:"family_plans"`
	CommunicationStyle  string               `json:"communication_style"`
	LoveStyle           string               `json:"love_style"`
	Workout             string               `json:"workout"`
	SocialMedia         string               `json:"social_media"`
	SoberLifestyle      string               `json:"sober_lifestyle"`
	RecoveryApproach    string               `json:"recovery_approach"`
	NightlifeComfort    string               `json:"nightlife_comfort"`
	SubstanceBoundaries string               `json:"substance_boundaries"`
	Interests           []string             `json:"interests"`
	Photos              []DatingPhoto        `json:"photos"`
	PromptAnswers       []DatingPromptAnswer `json:"prompt_answers"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
}

type DatingPromptAnswerInput struct {
	PromptKey string
	Answer    string
}

type UpdateProfileInput struct {
	Bio                  *string
	RelationshipGoal     *string
	InterestedInGenders  []string
	ReplaceGenders       bool
	HeightCm             *int
	ReplaceHeight        bool
	JobTitle             *string
	Company              *string
	Work                 *string
	School               *string
	Course               *string
	Education            *string
	KidsStatus           *string
	ChildrenStatus       *string
	RelationshipType     *string
	Gender               *string
	Sexuality            *string
	Pronouns             *string
	Ethnicity            *string
	Pets                 *string
	ReligiousBelief      *string
	LanguagesSpoken      []string
	ReplaceLanguages     bool
	PoliticalView        *string
	DrinkingStatus       *string
	SmokingStatus        *string
	DrugUseStatus        *string
	Zodiac               *string
	FamilyPlans          *string
	CommunicationStyle   *string
	LoveStyle            *string
	Workout              *string
	SocialMedia          *string
	SoberLifestyle       *string
	RecoveryApproach     *string
	NightlifeComfort     *string
	SubstanceBoundaries  *string
	Interests            []string
	ReplaceInterests     bool
	PromptAnswers        []DatingPromptAnswerInput
	ReplacePromptAnswers bool
	AgeMin               *int
	AgeMax               *int
	DistanceKm           *int
	Paused               *bool
	Complete             bool
}

type DatingMatch struct {
	ID          uuid.UUID     `json:"id"`
	Profile     DatingProfile `json:"profile"`
	ChatID      *uuid.UUID    `json:"chat_id,omitempty"`
	Status      string        `json:"status"`
	MatchedAt   time.Time     `json:"matched_at"`
	UnmatchedAt *time.Time    `json:"unmatched_at,omitempty"`
}

type DatingMatchResponse struct {
	ID          uuid.UUID           `json:"id"`
	Profile     PublicDatingProfile `json:"profile"`
	ChatID      *uuid.UUID          `json:"chat_id,omitempty"`
	Status      string              `json:"status"`
	MatchedAt   time.Time           `json:"matched_at"`
	UnmatchedAt *time.Time          `json:"unmatched_at,omitempty"`
}

type DatingMatchesPage struct {
	Items       []DatingMatchResponse `json:"items"`
	Limit       int                   `json:"limit"`
	HasMore     bool                  `json:"has_more"`
	NextCursor  *string               `json:"next_cursor,omitempty"`
	UnseenCount int                   `json:"unseen_count"`
}

type DatingMatchesSeenResponse struct {
	SeenAt      time.Time `json:"seen_at"`
	UnseenCount int       `json:"unseen_count"`
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

type ActionResultResponse struct {
	Action  string               `json:"action"`
	Matched bool                 `json:"matched"`
	Match   *DatingMatchResponse `json:"match,omitempty"`
}

type DatingEventType string

const (
	DatingEventSetupStarted       DatingEventType = "setup_started"
	DatingEventSetupCompleted     DatingEventType = "setup_completed"
	DatingEventProfileOpened      DatingEventType = "profile_opened"
	DatingEventLike               DatingEventType = "like"
	DatingEventPass               DatingEventType = "pass"
	DatingEventMatchCreated       DatingEventType = "match_created"
	DatingEventChatOpened         DatingEventType = "chat_opened"
	DatingEventFirstMessageSent   DatingEventType = "first_message_sent"
	DatingEventReport             DatingEventType = "report"
	DatingEventBlock              DatingEventType = "block"
	DatingEventUnmatch            DatingEventType = "unmatch"
	DatingEventLikesYouGateViewed DatingEventType = "likes_you_gate_viewed"
)

func (e DatingEventType) Valid() bool {
	switch e {
	case DatingEventSetupStarted,
		DatingEventSetupCompleted,
		DatingEventProfileOpened,
		DatingEventLike,
		DatingEventPass,
		DatingEventMatchCreated,
		DatingEventChatOpened,
		DatingEventFirstMessageSent,
		DatingEventReport,
		DatingEventBlock,
		DatingEventUnmatch,
		DatingEventLikesYouGateViewed:
		return true
	default:
		return false
	}
}

type DatingEventInput struct {
	ProfileID *uuid.UUID      `json:"profile_id,omitempty"`
	MatchID   *uuid.UUID      `json:"match_id,omitempty"`
	EventType DatingEventType `json:"event_type"`
	Position  *int            `json:"position,omitempty"`
	EventAt   time.Time       `json:"event_at"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}
