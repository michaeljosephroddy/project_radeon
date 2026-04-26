package auth

import (
	"strings"
	"time"

	"github.com/project_radeon/api/pkg/username"
)

type registerInput struct {
	Username   string  `json:"username"`
	Email      string  `json:"email"`
	Password   string  `json:"password"`
	City       string  `json:"city"`
	Country    string  `json:"country"`
	Gender     *string `json:"gender"`
	BirthDate  *string `json:"birth_date"`
	SoberSince *string `json:"sober_since"`
}

type loginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func normalizeRegisterInput(input registerInput) registerInput {
	input.Username = username.Normalize(input.Username)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	return input
}

func validateRegisterInput(input registerInput) map[string]string {
	errs := map[string]string{}
	if msg := username.ValidationError(input.Username); msg != "" {
		errs["username"] = msg
	}
	if input.Email == "" {
		errs["email"] = "required"
	}
	if len(input.Password) < 8 {
		errs["password"] = "must be at least 8 characters"
	}
	if input.Gender != nil {
		trimmedGender := strings.TrimSpace(*input.Gender)
		if trimmedGender != "" {
			if _, ok := normalizeRegisterGender(trimmedGender); !ok {
				errs["gender"] = "gender must be woman, man, or non_binary"
			}
		}
	}
	if input.BirthDate != nil {
		trimmedBirthDate := strings.TrimSpace(*input.BirthDate)
		if trimmedBirthDate != "" {
			if _, err := parseCalendarDate(trimmedBirthDate); err != nil {
				errs["birth_date"] = "birth_date must be YYYY-MM-DD"
			}
		}
	}
	return errs
}

func parseSoberSince(raw *string) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}

	parsed, err := time.Parse("2006-01-02", *raw)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseBirthDate(raw *string) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}

	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil, nil
	}

	return parseCalendarDate(trimmed)
}

func parseCalendarDate(raw string) (*time.Time, error) {
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func normalizeRegisterGender(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "woman", "women":
		return "woman", true
	case "man", "men":
		return "man", true
	case "non_binary", "non-binary", "nonbinary":
		return "non_binary", true
	default:
		return "", false
	}
}

func normalizeLoginInput(input loginInput) loginInput {
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	return input
}
