package support

import (
	"fmt"
	"strings"
	"time"
)

type createSupportOfferInput struct {
	OfferType    string  `json:"offer_type"`
	Message      *string `json:"message"`
	ScheduledFor *string `json:"scheduled_for"`
}

type CreateSupportRequestInput struct {
	SupportType     string           `json:"support_type"`
	Message         *string          `json:"message"`
	Urgency         string           `json:"urgency"`
	Topics          []string         `json:"topics"`
	PreferredGender *string          `json:"preferred_gender"`
	Location        *SupportLocation `json:"location"`
	PrivacyLevel    string           `json:"privacy_level"`
}

func normalizeCreateSupportRequestInput(input CreateSupportRequestInput) CreateSupportRequestInput {
	input.SupportType = strings.TrimSpace(input.SupportType)
	input.Urgency = normalizeSupportUrgency(strings.TrimSpace(input.Urgency))
	input.PrivacyLevel = strings.TrimSpace(input.PrivacyLevel)
	if input.Urgency == "" {
		input.Urgency = "low"
	}
	if input.PrivacyLevel == "" {
		input.PrivacyLevel = "standard"
	}
	if input.Message != nil {
		msg := strings.TrimSpace(*input.Message)
		input.Message = &msg
	}
	input.Topics = normalizeSupportTopics(input.Topics)
	if input.PreferredGender != nil {
		gender := strings.TrimSpace(*input.PreferredGender)
		if gender == "" {
			input.PreferredGender = nil
		} else {
			input.PreferredGender = &gender
		}
	}
	if input.Location != nil {
		input.Location.Visibility = strings.TrimSpace(input.Location.Visibility)
		if input.Location.Visibility == "" {
			input.Location.Visibility = "hidden"
		}
	}
	return input
}

func validateCreateSupportRequestInput(input CreateSupportRequestInput) map[string]string {
	errs := map[string]string{}
	if input.SupportType == "" {
		errs["support_type"] = "required"
	} else if !validSupportTypes[input.SupportType] {
		errs["support_type"] = "invalid"
	}
	if !validSupportUrgencies[input.Urgency] {
		errs["urgency"] = "invalid"
	}
	if !validSupportPrivacyLevels[input.PrivacyLevel] {
		errs["privacy_level"] = "invalid"
	}
	for _, topic := range input.Topics {
		if !validSupportTopics[topic] {
			errs["topics"] = "invalid"
			break
		}
	}
	if input.PreferredGender != nil && !validPreferredGenders[*input.PreferredGender] {
		errs["preferred_gender"] = "invalid"
	}
	if input.Location != nil && !validLocationVisibilities[input.Location.Visibility] {
		errs["location.visibility"] = "invalid"
	}
	return errs
}

func normalizeCreateSupportOfferInput(input createSupportOfferInput) createSupportOfferInput {
	input.OfferType = strings.TrimSpace(input.OfferType)
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

func validateCreateSupportOfferInput(input createSupportOfferInput) map[string]string {
	errs := map[string]string{}
	if input.OfferType == "" {
		errs["offer_type"] = "required"
	} else if !validSupportOfferTypes[input.OfferType] {
		errs["offer_type"] = "invalid"
	}
	return errs
}

func parseSupportOfferScheduledFor(raw *string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*raw))
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func formatSupportOfferMessage(offerType string, message *string, scheduledFor *time.Time) string {
	trimmed := ""
	if message != nil {
		trimmed = strings.TrimSpace(*message)
	}
	if trimmed != "" {
		return trimmed
	}

	switch offerType {
	case "chat":
		return "Hey, I saw your support request — I'm here and happy to chat right now if you'd like to talk."
	case "call":
		return "Hey, I saw your support request. I'm available for a call if you'd like to talk."
	case "meetup":
		return "Hey, I saw your support request. I'm close by and happy to meet up in person if that would help."
	default:
		if scheduledFor != nil {
			return fmt.Sprintf("Hey, I saw your support request and I'd like to help around %s.", scheduledFor.Format("Mon, Jan 2 at 3:04 PM"))
		}
		return "Hey, I saw your support request and I'd like to help."
	}
}

func isSupportedRequestStatusUpdate(status string) bool {
	return strings.TrimSpace(status) == "closed"
}

func normalizeSupportUrgency(value string) string {
	switch value {
	case "right_now":
		return "high"
	case "soon":
		return "medium"
	case "when_you_can":
		return "low"
	default:
		return value
	}
}

func normalizeSupportTopics(values []string) []string {
	seen := map[string]bool{}
	topics := make([]string, 0, len(values))
	for _, value := range values {
		topic := strings.TrimSpace(value)
		if topic == "" || seen[topic] {
			continue
		}
		seen[topic] = true
		topics = append(topics, topic)
	}
	return topics
}
