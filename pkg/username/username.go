package username

import (
	"regexp"
	"strings"
)

const (
	MinLength = 3
	MaxLength = 20
)

var (
	// Keeping the regex in one place ensures registration, profile edits, and
	// search normalization all enforce the same username contract.
	pattern  = regexp.MustCompile(`^[a-z0-9._]{3,20}$`)
	reserved = map[string]struct{}{
		"admin":     {},
		"api":       {},
		"app":       {},
		"chats":     {},
		"community": {},
		"discover":  {},
		"meetups":   {},
		"feed":      {},
		"help":      {},
		"login":     {},
		"messages":  {},
		"profile":   {},
		"register":  {},
		"settings":  {},
		"support":   {},
	}
)

func Normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func ValidationError(value string) string {
	// The first matching rule wins so callers get one actionable validation
	// message instead of a noisy list of overlapping username errors.
	switch {
	case value == "":
		return "required"
	case len(value) < MinLength || len(value) > MaxLength:
		return "must be between 3 and 20 characters"
	case !pattern.MatchString(value):
		return "may only contain lowercase letters, numbers, periods, and underscores"
	case IsReserved(value):
		return "is reserved"
	default:
		return ""
	}
}

func IsReserved(value string) bool {
	_, ok := reserved[value]
	return ok
}
