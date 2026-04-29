package support

import (
	"testing"
)

func TestNormalizeCreateSupportRequestInput(t *testing.T) {
	message := "  need a chat  "
	input := normalizeCreateSupportRequestInput(CreateSupportRequestInput{
		SupportType:  " chat ",
		Urgency:      " soon ",
		PrivacyLevel: " standard ",
		Message:      &message,
	})

	if input.SupportType != "chat" || input.Urgency != "medium" || input.PrivacyLevel != "standard" {
		t.Fatalf("unexpected normalized input: %+v", input)
	}
	if input.Message == nil || *input.Message != "need a chat" {
		t.Fatalf("unexpected message: %v", input.Message)
	}
}

func TestNormalizeCreateSupportRequestInputDefaults(t *testing.T) {
	input := normalizeCreateSupportRequestInput(CreateSupportRequestInput{SupportType: "chat"})
	if input.Urgency != "low" || input.PrivacyLevel != "standard" {
		t.Fatalf("unexpected defaults: %+v", input)
	}
}

func TestValidateCreateSupportRequestInput(t *testing.T) {
	errs := validateCreateSupportRequestInput(CreateSupportRequestInput{})
	if errs["support_type"] == "" {
		t.Fatalf("expected type error, errs: %+v", errs)
	}

	errs = validateCreateSupportRequestInput(CreateSupportRequestInput{
		SupportType:  "bad",
		Urgency:      "low",
		PrivacyLevel: "standard",
	})
	if errs["support_type"] != "invalid" {
		t.Fatalf("unexpected errs: %+v", errs)
	}

	errs = validateCreateSupportRequestInput(CreateSupportRequestInput{
		SupportType:  "chat",
		Urgency:      "bad_urgency",
		PrivacyLevel: "standard",
	})
	if errs["urgency"] != "invalid" {
		t.Fatalf("unexpected urgency error, errs: %+v", errs)
	}

	errs = validateCreateSupportRequestInput(CreateSupportRequestInput{
		SupportType:  "chat",
		Urgency:      "medium",
		PrivacyLevel: "bad_privacy",
	})
	if errs["privacy_level"] != "invalid" {
		t.Fatalf("unexpected privacy error, errs: %+v", errs)
	}
}

func TestNormalizeAndValidateCreateSupportOfferInput(t *testing.T) {
	message := "  here for you  "
	input := normalizeCreateSupportOfferInput(createSupportOfferInput{
		OfferType: " chat ",
		Message:   &message,
	})

	if input.OfferType != "chat" {
		t.Fatalf("OfferType = %q, want %q", input.OfferType, "chat")
	}
	if input.Message == nil || *input.Message != "here for you" {
		t.Fatalf("unexpected message: %v", input.Message)
	}

	if errs := validateCreateSupportOfferInput(createSupportOfferInput{}); errs["offer_type"] != "required" {
		t.Fatalf("unexpected errs: %+v", errs)
	}
	if errs := validateCreateSupportOfferInput(createSupportOfferInput{OfferType: "invalid"}); errs["offer_type"] != "invalid" {
		t.Fatalf("unexpected errs: %+v", errs)
	}
	if errs := validateCreateSupportOfferInput(createSupportOfferInput{OfferType: "chat"}); len(errs) != 0 {
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
