package recoverymeetings

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const SnapshotSchemaVersion = "2026-04-30"

var (
	ErrNotFound          = errors.New("not found")
	ErrInvalidSnapshot   = errors.New("invalid recovery meeting snapshot")
	ErrLargeDropRejected = errors.New("recovery meeting snapshot is more than 20 percent smaller than current active data")
)

type Snapshot struct {
	SchemaVersion string            `json:"schema_version"`
	GeneratedAt   time.Time         `json:"generated_at"`
	Meetings      []SnapshotMeeting `json:"meetings"`
}

type SnapshotMeeting struct {
	AccessibilityNotes *string              `json:"accessibility_notes"`
	AddressLine1       *string              `json:"address_line1"`
	AddressLine2       *string              `json:"address_line2"`
	City               *string              `json:"city"`
	Country            *string              `json:"country"`
	CountryCode        *string              `json:"country_code"`
	Fellowship         string               `json:"fellowship"`
	Formats            []string             `json:"formats"`
	IsApproximate      bool                 `json:"is_approximate_location"`
	Language           *string              `json:"language"`
	LastVerifiedAt     *time.Time           `json:"last_verified_at"`
	Latitude           *float64             `json:"latitude"`
	Longitude          *float64             `json:"longitude"`
	MeetingType        string               `json:"meeting_type"`
	Name               string               `json:"name"`
	Occurrences        []SnapshotOccurrence `json:"occurrences"`
	OnlineURL          *string              `json:"online_url"`
	PhoneJoinInfo      *string              `json:"phone_join_info"`
	PostalCode         *string              `json:"postal_code"`
	Region             *string              `json:"region"`
	RegionCode         *string              `json:"region_code"`
	SourceID           string               `json:"source_id"`
	SourceRecordID     string               `json:"source_record_id"`
	SourceURL          string               `json:"source_url"`
	VenueName          *string              `json:"venue_name"`
}

type SnapshotOccurrence struct {
	DayOfWeek      int     `json:"day_of_week"`
	EndTimeLocal   *string `json:"end_time_local"`
	StartTimeLocal string  `json:"start_time_local"`
	Timezone       string  `json:"timezone"`
}

type ImportOptions struct {
	SnapshotPath   string
	DryRun         bool
	AllowEmpty     bool
	AllowLargeDrop bool
}

type ImportResult struct {
	ImportRunID        *uuid.UUID `json:"import_run_id,omitempty"`
	MeetingsSeen       int        `json:"meetings_seen"`
	MeetingsUpserted   int        `json:"meetings_upserted"`
	OccurrencesWritten int        `json:"occurrences_written"`
	StaleMarked        int        `json:"stale_marked"`
	InactiveMarked     int        `json:"inactive_marked"`
	SnapshotSHA256     string     `json:"snapshot_sha256"`
	DryRun             bool       `json:"dry_run"`
}

type RecoveryMeeting struct {
	ID                    uuid.UUID           `json:"id"`
	Fellowship            string              `json:"fellowship"`
	SourceID              string              `json:"source_id"`
	SourceRecordID        string              `json:"source_record_id"`
	SourceURL             string              `json:"source_url"`
	Name                  string              `json:"name"`
	MeetingType           string              `json:"meeting_type"`
	VenueName             *string             `json:"venue_name,omitempty"`
	AddressLine1          *string             `json:"address_line1,omitempty"`
	AddressLine2          *string             `json:"address_line2,omitempty"`
	City                  *string             `json:"city,omitempty"`
	Region                *string             `json:"region,omitempty"`
	RegionCode            *string             `json:"region_code,omitempty"`
	PostalCode            *string             `json:"postal_code,omitempty"`
	Country               *string             `json:"country,omitempty"`
	CountryCode           *string             `json:"country_code,omitempty"`
	Latitude              *float64            `json:"latitude,omitempty"`
	Longitude             *float64            `json:"longitude,omitempty"`
	IsApproximateLocation bool                `json:"is_approximate_location"`
	OnlineURL             *string             `json:"online_url,omitempty"`
	PhoneJoinInfo         *string             `json:"phone_join_info,omitempty"`
	Formats               []string            `json:"formats"`
	Language              *string             `json:"language,omitempty"`
	AccessibilityNotes    *string             `json:"accessibility_notes,omitempty"`
	Occurrences           []MeetingOccurrence `json:"occurrences"`
	LastVerifiedAt        *time.Time          `json:"last_verified_at,omitempty"`
	UpdatedAt             time.Time           `json:"updated_at"`
}

type MeetingOccurrence struct {
	ID             uuid.UUID `json:"id"`
	DayOfWeek      int       `json:"day_of_week"`
	StartTimeLocal string    `json:"start_time_local"`
	EndTimeLocal   *string   `json:"end_time_local,omitempty"`
	Timezone       string    `json:"timezone"`
}

type LocationSuggestion struct {
	Label        string  `json:"label"`
	Location     string  `json:"location"`
	Region       *string `json:"region,omitempty"`
	RegionCode   *string `json:"region_code,omitempty"`
	Country      *string `json:"country,omitempty"`
	CountryCode  *string `json:"country_code,omitempty"`
	MeetingCount int     `json:"meeting_count"`
}

type RegionSuggestion struct {
	Label        string `json:"label"`
	Region       string `json:"region"`
	RegionCode   string `json:"region_code,omitempty"`
	Country      string `json:"country"`
	CountryCode  string `json:"country_code,omitempty"`
	MeetingCount int    `json:"meeting_count"`
}

type CountrySuggestion struct {
	Label        string `json:"label"`
	Country      string `json:"country"`
	CountryCode  string `json:"country_code,omitempty"`
	MeetingCount int    `json:"meeting_count"`
}

type FilterOptionLevel string

const (
	FilterOptionLevelCountry  FilterOptionLevel = "country"
	FilterOptionLevelRegion   FilterOptionLevel = "region"
	FilterOptionLevelLocality FilterOptionLevel = "locality"
)

type FilterOption struct {
	Label        string  `json:"label"`
	Level        string  `json:"level"`
	Country      *string `json:"country,omitempty"`
	CountryCode  *string `json:"country_code,omitempty"`
	Region       *string `json:"region,omitempty"`
	RegionCode   *string `json:"region_code,omitempty"`
	Locality     *string `json:"locality,omitempty"`
	MeetingCount int     `json:"meeting_count"`
}

type FilterOptionsParams struct {
	Level       FilterOptionLevel
	Query       string
	Fellowships []string
	Country     string
	Region      string
	Limit       int
}

type CursorPage[T any] struct {
	Items      []T     `json:"items"`
	Limit      int     `json:"limit"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor,omitempty"`
}

type ListParams struct {
	Query       string
	Fellowships []string
	Country     string
	Region      string
	City        string
	Location    string
	MeetingType string
	DayOfWeek   *int
	Cursor      string
	Limit       int
}
