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

func normalizeLoginInput(input loginInput) loginInput {
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	return input
}
