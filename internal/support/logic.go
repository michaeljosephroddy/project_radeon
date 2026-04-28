package support

import (
	"fmt"
	"strings"
	"time"
)

type createSupportResponseInput struct {
	ResponseType string  `json:"response_type"`
	Message      *string `json:"message"`
	ScheduledFor *string `json:"scheduled_for"`
}

type createChannelSupportRequestInput struct {
	Type         string  `json:"type"`
	Message      *string `json:"message"`
	Urgency      string  `json:"urgency"`
	PrivacyLevel string  `json:"privacy_level"`
}

func normalizeCreateChannelSupportRequestInput(input createChannelSupportRequestInput) createChannelSupportRequestInput {
	input.Type = strings.TrimSpace(input.Type)
	input.Urgency = strings.TrimSpace(input.Urgency)
	input.PrivacyLevel = strings.TrimSpace(input.PrivacyLevel)
	if input.Urgency == "" {
		input.Urgency = "when_you_can"
	}
	if input.PrivacyLevel == "" {
		input.PrivacyLevel = "standard"
	}
	if input.Message != nil {
		msg := strings.TrimSpace(*input.Message)
		input.Message = &msg
	}
	return input
}

func validateCreateChannelSupportRequestInput(input createChannelSupportRequestInput) map[string]string {
	errs := map[string]string{}
	if input.Type == "" {
		errs["type"] = "required"
	} else if !validSupportTypes[input.Type] {
		errs["type"] = "invalid"
	}
	if !validSupportUrgencies[input.Urgency] {
		errs["urgency"] = "invalid"
	}
	if !validSupportPrivacyLevels[input.PrivacyLevel] {
		errs["privacy_level"] = "invalid"
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
		return "Hey, I saw your support request — I'm here and happy to chat right now if you'd like to talk."
	case "can_meet":
		return "Hey, I saw your support request. I'm close by and happy to meet up in person if that would help."
	case "check_in_later":
		if scheduledFor != nil {
			return fmt.Sprintf("Hey, I saw your support request. I can't chat right now but I'd love to check in with you on %s.", scheduledFor.Format("Mon, Jan 2 at 3:04 PM"))
		}
		return "Hey, I saw your support request. I can't chat right now but I'd love to check in with you a bit later."
	default:
		return "Hey, I saw your support request and I'd like to help."
	}
}

func isSupportedRequestStatusUpdate(status string) bool {
	return strings.TrimSpace(status) == "closed"
}
