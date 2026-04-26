package meetups

import (
	"strings"
	"time"
)

var validEventTypes = map[string]bool{
	"in_person": true,
	"online":    true,
	"hybrid":    true,
}

var validEventStatuses = map[string]bool{
	"draft":     true,
	"published": true,
	"cancelled": true,
	"completed": true,
}

var validEventCreationStatuses = map[string]bool{
	"draft":     true,
	"published": true,
}

var validEventVisibility = map[string]bool{
	"public":   true,
	"unlisted": true,
}

var validEventSorts = map[string]bool{
	"recommended": true,
	"soonest":     true,
	"distance":    true,
	"popular":     true,
	"newest":      true,
}

var validDatePresets = map[string]bool{
	"today":        true,
	"tomorrow":     true,
	"this_week":    true,
	"this_weekend": true,
	"custom":       true,
}

var validTimeOfDay = map[string]bool{
	"morning":   true,
	"afternoon": true,
	"evening":   true,
	"night":     true,
}

var validMyMeetupScopes = map[string]bool{
	"upcoming":  true,
	"going":     true,
	"drafts":    true,
	"cancelled": true,
	"past":      true,
}

type meetupInput struct {
	Title           string   `json:"title"`
	Description     *string  `json:"description"`
	CategorySlug    string   `json:"category_slug"`
	CoHostIDs       []string `json:"co_host_ids"`
	EventType       string   `json:"event_type"`
	Status          string   `json:"status"`
	Visibility      string   `json:"visibility"`
	City            string   `json:"city"`
	Country         *string  `json:"country"`
	VenueName       *string  `json:"venue_name"`
	AddressLine1    *string  `json:"address_line_1"`
	AddressLine2    *string  `json:"address_line_2"`
	HowToFindUs     *string  `json:"how_to_find_us"`
	OnlineURL       *string  `json:"online_url"`
	CoverImageURL   *string  `json:"cover_image_url"`
	StartsAt        string   `json:"starts_at"`
	EndsAt          *string  `json:"ends_at"`
	Timezone        string   `json:"timezone"`
	Latitude        *float64 `json:"lat"`
	Longitude       *float64 `json:"lng"`
	Capacity        *int     `json:"capacity"`
	WaitlistEnabled bool     `json:"waitlist_enabled"`
}

func normalizeMeetupInput(input meetupInput) meetupInput {
	input.Title = strings.TrimSpace(input.Title)
	input.CategorySlug = strings.TrimSpace(strings.ToLower(input.CategorySlug))
	input.EventType = strings.TrimSpace(strings.ToLower(input.EventType))
	if input.EventType == "" {
		input.EventType = "in_person"
	}
	input.Status = strings.TrimSpace(strings.ToLower(input.Status))
	if input.Status == "" {
		input.Status = "published"
	}
	input.Visibility = strings.TrimSpace(strings.ToLower(input.Visibility))
	if input.Visibility == "" {
		input.Visibility = "public"
	}
	input.City = strings.TrimSpace(input.City)
	input.Timezone = strings.TrimSpace(input.Timezone)
	if input.Timezone == "" {
		input.Timezone = "UTC"
	}
	trimStringPtr(&input.Description)
	trimStringPtr(&input.Country)
	trimStringPtr(&input.VenueName)
	trimStringPtr(&input.AddressLine1)
	trimStringPtr(&input.AddressLine2)
	trimStringPtr(&input.HowToFindUs)
	trimStringPtr(&input.OnlineURL)
	trimStringPtr(&input.CoverImageURL)
	if input.EndsAt != nil {
		trimStringPtrValue(input.EndsAt)
	}
	input.CoHostIDs = normalizeStringSlice(input.CoHostIDs)
	return input
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func trimStringPtr(ptr **string) {
	if *ptr == nil {
		return
	}
	trimmed := strings.TrimSpace(**ptr)
	*ptr = &trimmed
}

func trimStringPtrValue(ptr *string) {
	if ptr == nil {
		return
	}
	trimmed := strings.TrimSpace(*ptr)
	*ptr = trimmed
}

func validateMeetupInput(input meetupInput) map[string]string {
	errs := map[string]string{}
	if input.Title == "" {
		errs["title"] = "required"
	}
	if input.CategorySlug == "" {
		errs["category_slug"] = "required"
	}
	if input.City == "" {
		errs["city"] = "required"
	}
	if input.StartsAt == "" {
		errs["starts_at"] = "required"
	}
	if !validEventTypes[input.EventType] {
		errs["event_type"] = "invalid"
	}
	if !validEventCreationStatuses[input.Status] {
		errs["status"] = "invalid"
	}
	if !validEventVisibility[input.Visibility] {
		errs["visibility"] = "invalid"
	}
	if input.EventType == "online" && (input.OnlineURL == nil || strings.TrimSpace(*input.OnlineURL) == "") {
		errs["online_url"] = "required"
	}
	if input.EventType == "in_person" && input.VenueName != nil && strings.TrimSpace(*input.VenueName) == "" {
		errs["venue_name"] = "required"
	}
	if input.EventType != "online" && input.Latitude != nil && input.Longitude == nil {
		errs["lng"] = "required"
	}
	if input.EventType != "online" && input.Longitude != nil && input.Latitude == nil {
		errs["lat"] = "required"
	}
	return errs
}

func parseMeetupStartsAt(raw string) (time.Time, error) {
	return time.Parse(time.RFC3339, raw)
}

func parseMeetupEndsAt(raw *string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339, strings.TrimSpace(*raw))
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func validateMeetupCapacity(capacity *int) string {
	if capacity != nil && *capacity < 1 {
		return "must be greater than 0"
	}
	return ""
}

func validateMeetupEndsAt(startsAt time.Time, endsAt *time.Time) string {
	if endsAt != nil && !endsAt.After(startsAt) {
		return "must be after starts_at"
	}
	return ""
}
