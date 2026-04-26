package meetups

import "testing"

func TestNormalizeMeetupInput(t *testing.T) {
	description := "  hello world  "
	country := "  Ireland "
	input := normalizeMeetupInput(meetupInput{
		Title:        "  Weekly Meetup  ",
		CategorySlug: "  COFFEE ",
		EventType:    " In_Person ",
		Status:       " Published ",
		Visibility:   " Public ",
		City:         "  Dublin  ",
		Country:      &country,
		Description:  &description,
	})

	if input.Title != "Weekly Meetup" || input.City != "Dublin" {
		t.Fatalf("unexpected normalized input: %+v", input)
	}
	if input.CategorySlug != "coffee" || input.EventType != "in_person" || input.Status != "published" || input.Visibility != "public" {
		t.Fatalf("unexpected normalized metadata: %+v", input)
	}
	if input.Description == nil || *input.Description != "hello world" {
		t.Fatalf("unexpected description: %v", input.Description)
	}
	if input.Country == nil || *input.Country != "Ireland" {
		t.Fatalf("unexpected country: %v", input.Country)
	}
}

func TestValidateMeetupInput(t *testing.T) {
	errs := validateMeetupInput(meetupInput{})
	if errs["title"] == "" || errs["city"] == "" || errs["starts_at"] == "" || errs["category_slug"] == "" {
		t.Fatalf("unexpected errs: %+v", errs)
	}
}

func TestParseMeetupStartsAt(t *testing.T) {
	if _, err := parseMeetupStartsAt("2026-04-19T18:00:00Z"); err != nil {
		t.Fatalf("parseMeetupStartsAt(valid) error = %v", err)
	}
	if _, err := parseMeetupStartsAt("not-a-time"); err == nil {
		t.Fatal("expected invalid starts_at to fail")
	}
}

func TestValidateMeetupCapacity(t *testing.T) {
	zero := 0
	if msg := validateMeetupCapacity(&zero); msg == "" {
		t.Fatal("expected capacity validation error")
	}
	one := 1
	if msg := validateMeetupCapacity(&one); msg != "" {
		t.Fatalf("unexpected error: %q", msg)
	}
}

func TestValidateMeetupEndsAt(t *testing.T) {
	start, _ := parseMeetupStartsAt("2026-04-19T18:00:00Z")
	end, _ := parseMeetupStartsAt("2026-04-19T19:00:00Z")
	before, _ := parseMeetupStartsAt("2026-04-19T17:00:00Z")
	if msg := validateMeetupEndsAt(start, &end); msg != "" {
		t.Fatalf("unexpected end validation error: %q", msg)
	}
	if msg := validateMeetupEndsAt(start, &before); msg == "" {
		t.Fatal("expected ends_at validation error")
	}
}
