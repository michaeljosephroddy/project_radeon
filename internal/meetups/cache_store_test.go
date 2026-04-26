package meetups

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/project_radeon/api/internal/cachetest"
)

type stubQuerier struct {
	getMeetupCalls int
}

var organizerID = uuid.New()

func (s *stubQuerier) ListCategories(context.Context) ([]MeetupCategory, error) {
	return []MeetupCategory{{Slug: "coffee", Label: "Coffee", SortOrder: 1}}, nil
}

func (s *stubQuerier) DiscoverMeetups(context.Context, uuid.UUID, DiscoverMeetupsParams) (*CursorPage[Meetup], error) {
	return &CursorPage[Meetup]{Items: nil, Limit: 20}, nil
}

func (s *stubQuerier) ListMyMeetups(context.Context, uuid.UUID, MyMeetupsParams) (*CursorPage[Meetup], error) {
	return &CursorPage[Meetup]{Items: nil, Limit: 20}, nil
}

func (s *stubQuerier) GetMeetup(_ context.Context, meetupID, userID uuid.UUID) (*Meetup, error) {
	s.getMeetupCalls++
	return &Meetup{
		ID:                meetupID,
		OrganizerID:       organizerID,
		OrganizerUsername: "host",
		Title:             "meetup",
		CategorySlug:      "coffee",
		CategoryLabel:     "Coffee",
		City:              "Dublin",
		StartsAt:          time.Unix(int64(s.getMeetupCalls), 0).UTC(),
		IsAttending:       userID == organizerID,
		UpdatedAt:         time.Unix(int64(s.getMeetupCalls), 0).UTC(),
		CreatedAt:         time.Unix(0, 0).UTC(),
	}, nil
}

func (s *stubQuerier) CreateMeetup(context.Context, uuid.UUID, CreateMeetupInput) (*Meetup, error) {
	return nil, nil
}

func (s *stubQuerier) UpdateMeetup(context.Context, uuid.UUID, uuid.UUID, UpdateMeetupInput) (*Meetup, error) {
	return nil, nil
}

func (s *stubQuerier) PublishMeetup(context.Context, uuid.UUID, uuid.UUID) (*Meetup, error) {
	return nil, nil
}

func (s *stubQuerier) CancelMeetup(context.Context, uuid.UUID, uuid.UUID) (*Meetup, error) {
	return nil, nil
}

func (s *stubQuerier) DeleteMeetup(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (s *stubQuerier) GetAttendees(context.Context, uuid.UUID, int, int) ([]Attendee, error) {
	return nil, nil
}

func (s *stubQuerier) GetWaitlist(context.Context, uuid.UUID, uuid.UUID, int, int) ([]Attendee, error) {
	return nil, nil
}

func (s *stubQuerier) ToggleRSVP(context.Context, uuid.UUID, uuid.UUID) (*RSVPResult, error) {
	return &RSVPResult{State: "going", Attending: true, AttendeeCount: 1}, nil
}

func TestCachedStoreInvalidatesMeetupDetailAfterRSVP(t *testing.T) {
	t.Parallel()

	inner := &stubQuerier{}
	store := cachetest.NewStore()
	cached := NewCachedStore(inner, store)

	meetupID := uuid.New()
	viewerID := uuid.New()

	first, err := cached.GetMeetup(context.Background(), meetupID, viewerID)
	if err != nil {
		t.Fatalf("first GetMeetup: %v", err)
	}
	second, err := cached.GetMeetup(context.Background(), meetupID, viewerID)
	if err != nil {
		t.Fatalf("second GetMeetup: %v", err)
	}
	if inner.getMeetupCalls != 1 {
		t.Fatalf("expected one underlying GetMeetup call after cache hit, got %d", inner.getMeetupCalls)
	}
	if !first.StartsAt.Equal(second.StartsAt) {
		t.Fatalf("expected cached meetup detail to be identical before invalidation")
	}

	if _, err := cached.ToggleRSVP(context.Background(), meetupID, viewerID); err != nil {
		t.Fatalf("ToggleRSVP: %v", err)
	}

	third, err := cached.GetMeetup(context.Background(), meetupID, viewerID)
	if err != nil {
		t.Fatalf("third GetMeetup: %v", err)
	}
	if inner.getMeetupCalls != 2 {
		t.Fatalf("expected invalidation to force a fresh GetMeetup call, got %d", inner.getMeetupCalls)
	}
	if !third.StartsAt.After(second.StartsAt) {
		t.Fatalf("expected invalidated meetup detail to be newer than cached detail")
	}
}
