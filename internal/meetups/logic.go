package meetups

import (
	"strings"
	"time"
)

func normalizeMeetupInput(input meetupInput) meetupInput {
	input.Title = strings.TrimSpace(input.Title)
	input.City = strings.TrimSpace(input.City)
	if input.Description != nil {
		description := strings.TrimSpace(*input.Description)
		input.Description = &description
	}
	return input
}

func validateMeetupInput(input meetupInput) map[string]string {
	errs := map[string]string{}
	if input.Title == "" {
		errs["title"] = "required"
	}
	if input.City == "" {
		errs["city"] = "required"
	}
	if input.StartsAt == "" {
		errs["starts_at"] = "required"
	}
	return errs
}

func parseMeetupStartsAt(raw string) (time.Time, error) {
	return time.Parse(time.RFC3339, raw)
}

func validateMeetupCapacity(capacity *int) string {
	if capacity != nil && *capacity < 1 {
		return "must be greater than 0"
	}
	return ""
}
