package support

import (
	"fmt"
	"strings"
	"time"
)

type createSupportRequestInput struct {
	Type      string  `json:"type"`
	Message   *string `json:"message"`
	Audience  string  `json:"audience"`
	ExpiresAt string  `json:"expires_at"`
}

type createSupportResponseInput struct {
	ResponseType string  `json:"response_type"`
	Message      *string `json:"message"`
	ScheduledFor *string `json:"scheduled_for"`
}

func normalizeCreateSupportRequestInput(input createSupportRequestInput) createSupportRequestInput {
	input.Type = strings.TrimSpace(input.Type)
	input.Audience = strings.TrimSpace(input.Audience)
	if input.Message != nil {
		msg := strings.TrimSpace(*input.Message)
		input.Message = &msg
	}
	return input
}

func validateCreateSupportRequestInput(input createSupportRequestInput) map[string]string {
	errs := map[string]string{}
	if input.Type == "" {
		errs["type"] = "required"
	} else if !validSupportTypes[input.Type] {
		errs["type"] = "invalid"
	}
	if input.Audience == "" {
		errs["audience"] = "required"
	} else if !validSupportAudiences[input.Audience] {
		errs["audience"] = "invalid"
	}
	if input.ExpiresAt == "" {
		errs["expires_at"] = "required"
	}
	return errs
}

func parseSupportRequestExpiry(raw string, now time.Time) (time.Time, error) {
	expiresAt, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, err
	}
	if !expiresAt.After(now) {
		return time.Time{}, errExpiryNotFuture
	}
	return expiresAt, nil
}

func normalizeCreateSupportResponseInput(input createSupportResponseInput) createSupportResponseInput {
	input.ResponseType = strings.TrimSpace(input.ResponseType)
	if input.Message != nil {
		msg := strings.TrimSpace(*input.Message)
		input.Message = &msg
	}
	if input.ScheduledFor != nil {
		scheduled := strings.TrimSpace(*input.ScheduledFor)
		input.ScheduledFor = &scheduled
	}
	return input
}

func validateCreateSupportResponseInput(input createSupportResponseInput) map[string]string {
	errs := map[string]string{}
	if input.ResponseType == "" {
		errs["response_type"] = "required"
	} else if !validSupportResponseTypes[input.ResponseType] {
		errs["response_type"] = "invalid"
	}
	if input.ResponseType == "check_in_later" {
		if input.ScheduledFor == nil || strings.TrimSpace(*input.ScheduledFor) == "" {
			errs["scheduled_for"] = "required"
		}
	}
	return errs
}

func parseSupportResponseScheduledFor(raw *string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*raw))
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func formatSupportResponseMessage(responseType string, message *string, scheduledFor *time.Time) string {
	trimmed := ""
	if message != nil {
		trimmed = strings.TrimSpace(*message)
	}
	if trimmed != "" {
		return trimmed
	}

	switch responseType {
	case "can_chat":
		return "I can chat now about your support request."
	case "nearby":
		return "I'm nearby if company would help."
	case "check_in_later":
		if scheduledFor != nil {
			return fmt.Sprintf("Busy at the moment but I can check in at %s.", scheduledFor.Format(time.RFC3339))
		}
		return "Busy at the moment but I can check in later."
	default:
		return "I responded to your support request."
	}
}

func defaultSupportModes() []string {
	return []string{"can_chat", "check_in_later", "nearby"}
}

func normalizeSupportModes(modes []string) []string {
	if len(modes) == 0 {
		return defaultSupportModes()
	}

	normalized := make([]string, 0, len(modes))
	seen := make(map[string]bool, len(modes))
	for _, mode := range modes {
		trimmed := strings.TrimSpace(mode)
		if trimmed == "" || seen[trimmed] || !validSupportResponseTypes[trimmed] {
			continue
		}
		seen[trimmed] = true
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return defaultSupportModes()
	}
	return normalized
}

func supportModeEnabled(modes []string, responseType string) bool {
	for _, mode := range normalizeSupportModes(modes) {
		if mode == responseType {
			return true
		}
	}
	return false
}

func isSupportedRequestStatusUpdate(status string) bool {
	return strings.TrimSpace(status) == "closed"
}
