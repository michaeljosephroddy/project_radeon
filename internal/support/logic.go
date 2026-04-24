package support

import (
	"fmt"
	"strings"
	"time"
)

type createSupportRequestInput struct {
	Type               string  `json:"type"`
	Message            *string `json:"message"`
	Urgency            string  `json:"urgency"`
	PriorityVisibility bool    `json:"priority_visibility"`
}

type createSupportResponseInput struct {
	ResponseType string  `json:"response_type"`
	Message      *string `json:"message"`
	ScheduledFor *string `json:"scheduled_for"`
}

func normalizeCreateSupportRequestInput(input createSupportRequestInput) createSupportRequestInput {
	input.Type = strings.TrimSpace(input.Type)
	input.Urgency = strings.TrimSpace(input.Urgency)
	if input.Urgency == "" {
		input.Urgency = "when_you_can"
	}
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
	if !validSupportUrgencies[input.Urgency] {
		errs["urgency"] = "invalid"
	}
	return errs
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

func isSupportedRequestStatusUpdate(status string) bool {
	return strings.TrimSpace(status) == "closed"
}
