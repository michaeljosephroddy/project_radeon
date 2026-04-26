package meetups

import (
	"time"

	"github.com/google/uuid"
)

type MeetupCategory struct {
	Slug      string `json:"slug"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

type MeetupHost struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url,omitempty"`
	Role      string    `json:"role"`
}

type AttendeePreview struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url,omitempty"`
}

type Meetup struct {
	ID                uuid.UUID         `json:"id"`
	OrganizerID       uuid.UUID         `json:"organizer_id"`
	OrganizerUsername string            `json:"organizer_username"`
	OrganizerAvatar   *string           `json:"organizer_avatar_url,omitempty"`
	Title             string            `json:"title"`
	Description       *string           `json:"description,omitempty"`
	CategorySlug      string            `json:"category_slug"`
	CategoryLabel     string            `json:"category_label"`
	EventType         string            `json:"event_type"`
	Status            string            `json:"status"`
	Visibility        string            `json:"visibility"`
	City              string            `json:"city"`
	Country           *string           `json:"country,omitempty"`
	VenueName         *string           `json:"venue_name,omitempty"`
	AddressLine1      *string           `json:"address_line_1,omitempty"`
	AddressLine2      *string           `json:"address_line_2,omitempty"`
	HowToFindUs       *string           `json:"how_to_find_us,omitempty"`
	OnlineURL         *string           `json:"online_url,omitempty"`
	CoverImageURL     *string           `json:"cover_image_url,omitempty"`
	StartsAt          time.Time         `json:"starts_at"`
	EndsAt            *time.Time        `json:"ends_at,omitempty"`
	Timezone          string            `json:"timezone"`
	Latitude          *float64          `json:"lat,omitempty"`
	Longitude         *float64          `json:"lng,omitempty"`
	DistanceKM        *float64          `json:"distance_km,omitempty"`
	Capacity          *int              `json:"capacity,omitempty"`
	AttendeeCt        int               `json:"attendee_count"`
	WaitlistEnabled   bool              `json:"waitlist_enabled"`
	WaitlistCount     int               `json:"waitlist_count"`
	SavedCount        int               `json:"saved_count"`
	IsAttending       bool              `json:"is_attending"`
	IsWaitlisted      bool              `json:"is_waitlisted"`
	CanManage         bool              `json:"can_manage"`
	Attendees         []AttendeePreview `json:"attendee_preview,omitempty"`
	Hosts             []MeetupHost      `json:"hosts,omitempty"`
	PublishedAt       *time.Time        `json:"published_at,omitempty"`
	UpdatedAt         time.Time         `json:"updated_at"`
	CreatedAt         time.Time         `json:"created_at"`
}

type Attendee struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url,omitempty"`
	City      *string   `json:"city,omitempty"`
	RSVPAt    time.Time `json:"rsvp_at"`
}

type RSVPResult struct {
	State         string `json:"state"`
	Attending     bool   `json:"attending"`
	Waitlisted    bool   `json:"waitlisted"`
	AttendeeCount int    `json:"attendee_count"`
	WaitlistCount int    `json:"waitlist_count"`
}

type CursorPage[T any] struct {
	Items      []T     `json:"items"`
	Limit      int     `json:"limit"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor,omitempty"`
}

type DiscoverMeetupsParams struct {
	Query         string
	CategorySlug  string
	City          string
	DistanceKM    *int
	EventType     string
	DatePreset    string
	DateFrom      *time.Time
	DateTo        *time.Time
	DayOfWeek     []int
	TimeOfDay     []string
	OpenSpotsOnly bool
	Sort          string
	Cursor        string
	Limit         int
}

type MyMeetupsParams struct {
	Scope  string
	Cursor string
	Limit  int
}

type CreateMeetupInput struct {
	Title           string
	Description     *string
	CategorySlug    string
	CoHostIDs       []uuid.UUID
	EventType       string
	Status          string
	Visibility      string
	City            string
	Country         *string
	VenueName       *string
	AddressLine1    *string
	AddressLine2    *string
	HowToFindUs     *string
	OnlineURL       *string
	CoverImageURL   *string
	StartsAt        time.Time
	EndsAt          *time.Time
	Timezone        string
	Latitude        *float64
	Longitude       *float64
	Capacity        *int
	WaitlistEnabled bool
}

type UpdateMeetupInput = CreateMeetupInput
