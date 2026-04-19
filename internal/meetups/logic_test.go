package meetups

import "testing"

func TestNormalizeMeetupInput(t *testing.T) {
	description := "  hello world  "
	input := normalizeMeetupInput(meetupInput{
		Title:       "  Weekly Meetup  ",
		City:        "  Dublin  ",
		Description: &description,
	})

	if input.Title != "Weekly Meetup" || input.City != "Dublin" {
		t.Fatalf("unexpected normalized input: %+v", input)
	}
	if input.Description == nil || *input.Description != "hello world" {
		t.Fatalf("unexpected description: %v", input.Description)
	}
}

func TestValidateMeetupInput(t *testing.T) {
	errs := validateMeetupInput(meetupInput{})
	if errs["title"] == "" || errs["city"] == "" || errs["starts_at"] == "" {
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
