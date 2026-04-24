package support

import (
	"testing"
)

func TestNormalizeCreateSupportRequestInput(t *testing.T) {
	message := "  need a chat  "
	input := normalizeCreateSupportRequestInput(createSupportRequestInput{
		Type:    " need_to_talk ",
		Urgency: " soon ",
		Message: &message,
	})

	if input.Type != "need_to_talk" || input.Urgency != "soon" {
		t.Fatalf("unexpected normalized input: %+v", input)
	}
	if input.Message == nil || *input.Message != "need a chat" {
		t.Fatalf("unexpected message: %v", input.Message)
	}
}

func TestNormalizeCreateSupportRequestInputDefaultsUrgency(t *testing.T) {
	input := normalizeCreateSupportRequestInput(createSupportRequestInput{Type: "need_to_talk"})
	if input.Urgency != "when_you_can" {
		t.Fatalf("expected default urgency 'when_you_can', got %q", input.Urgency)
	}
}

func TestValidateCreateSupportRequestInput(t *testing.T) {
	errs := validateCreateSupportRequestInput(createSupportRequestInput{})
	if errs["type"] == "" {
		t.Fatalf("expected type error, errs: %+v", errs)
	}

	errs = validateCreateSupportRequestInput(createSupportRequestInput{
		Type:    "bad",
		Urgency: "when_you_can",
	})
	if errs["type"] != "invalid" {
		t.Fatalf("unexpected errs: %+v", errs)
	}

	errs = validateCreateSupportRequestInput(createSupportRequestInput{
		Type:    "need_to_talk",
		Urgency: "bad_urgency",
	})
	if errs["urgency"] != "invalid" {
		t.Fatalf("unexpected urgency error, errs: %+v", errs)
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
