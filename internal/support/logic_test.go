package support

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeCreateSupportRequestInput(t *testing.T) {
	message := "  need a chat  "
	input := normalizeCreateSupportRequestInput(createSupportRequestInput{
		Type:     " need_to_talk ",
		Audience: " community ",
		Message:  &message,
	})

	if input.Type != "need_to_talk" || input.Audience != "community" {
		t.Fatalf("unexpected normalized input: %+v", input)
	}
	if input.Message == nil || *input.Message != "need a chat" {
		t.Fatalf("unexpected message: %v", input.Message)
	}
}

func TestValidateCreateSupportRequestInput(t *testing.T) {
	errs := validateCreateSupportRequestInput(createSupportRequestInput{})
	if errs["type"] == "" || errs["audience"] == "" || errs["expires_at"] == "" {
		t.Fatalf("unexpected errs: %+v", errs)
	}

	errs = validateCreateSupportRequestInput(createSupportRequestInput{
		Type:      "bad",
		Audience:  "bad",
		ExpiresAt: "2026-04-19T18:00:00Z",
	})
	if errs["type"] != "invalid" || errs["audience"] != "invalid" {
		t.Fatalf("unexpected errs: %+v", errs)
	}
}

func TestParseSupportRequestExpiry(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)

	if _, err := parseSupportRequestExpiry("bad", now); err == nil {
		t.Fatal("expected invalid time to fail")
	}

	if _, err := parseSupportRequestExpiry("2026-04-19T11:00:00Z", now); !errors.Is(err, errExpiryNotFuture) {
		t.Fatalf("expected errExpiryNotFuture, got %v", err)
	}

	expiresAt, err := parseSupportRequestExpiry("2026-04-19T13:00:00Z", now)
	if err != nil {
		t.Fatalf("parseSupportRequestExpiry(valid) error = %v", err)
	}
	if !expiresAt.After(now) {
		t.Fatalf("expected future time, got %v", expiresAt)
	}
}

func TestNormalizeAndValidateCreateSupportResponseInput(t *testing.T) {
	message := "  here for you  "
	input := normalizeCreateSupportResponseInput(createSupportResponseInput{
		ResponseType: " can_chat ",
		Message:      &message,
	})

	if input.ResponseType != "can_chat" {
		t.Fatalf("ResponseType = %q, want %q", input.ResponseType, "can_chat")
	}
	if input.Message == nil || *input.Message != "here for you" {
		t.Fatalf("unexpected message: %v", input.Message)
	}

	if errs := validateCreateSupportResponseInput(createSupportResponseInput{}); errs["response_type"] != "required" {
		t.Fatalf("unexpected errs: %+v", errs)
	}
	if errs := validateCreateSupportResponseInput(createSupportResponseInput{ResponseType: "invalid"}); errs["response_type"] != "invalid" {
		t.Fatalf("unexpected errs: %+v", errs)
	}
	if errs := validateCreateSupportResponseInput(createSupportResponseInput{ResponseType: "can_chat"}); len(errs) != 0 {
		t.Fatalf("unexpected errs: %+v", errs)
	}
}

func TestIsSupportedRequestStatusUpdate(t *testing.T) {
	if !isSupportedRequestStatusUpdate(" closed ") {
		t.Fatal("expected trimmed closed status to be supported")
	}
	if isSupportedRequestStatusUpdate("open") {
		t.Fatal("expected open to be unsupported")
	}
}
