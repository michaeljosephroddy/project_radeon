package support

import (
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
	return input
}

func validateCreateSupportResponseInput(input createSupportResponseInput) map[string]string {
	if input.ResponseType == "" {
		return map[string]string{"response_type": "required"}
	}
	if !validSupportResponseTypes[input.ResponseType] {
		return map[string]string{"response_type": "invalid"}
	}
	return map[string]string{}
}

func isSupportedRequestStatusUpdate(status string) bool {
	return strings.TrimSpace(status) == "closed"
}
