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

func (s *stubQuerier) ListMeetups(context.Context, uuid.UUID, string, string, int, int) ([]Meetup, error) {
	return nil, nil
}

func (s *stubQuerier) ListMyMeetups(context.Context, uuid.UUID, int, int) ([]Meetup, error) {
	return nil, nil
}

func (s *stubQuerier) AttachAttendeePreviews(context.Context, []Meetup, int) error {
	return nil
}

func (s *stubQuerier) GetMeetup(_ context.Context, meetupID, userID uuid.UUID) (*Meetup, error) {
	s.getMeetupCalls++
	return &Meetup{
		ID:          meetupID,
		OrganizerID: organizerID,
		Title:       "meetup",
		StartsAt:    time.Unix(int64(s.getMeetupCalls), 0).UTC(),
		IsAttending: userID == organizerID,
	}, nil
}

func (s *stubQuerier) CreateMeetup(context.Context, uuid.UUID, string, *string, string, time.Time, *int) (*Meetup, error) {
	return nil, nil
}

func (s *stubQuerier) GetAttendees(context.Context, uuid.UUID, int, int) ([]Attendee, error) {
	return nil, nil
}

func (s *stubQuerier) GetMeetupCapacity(context.Context, uuid.UUID) (*int, int, error) {
	return nil, 0, nil
}

func (s *stubQuerier) IsRSVPd(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

func (s *stubQuerier) AddRSVP(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (s *stubQuerier) RemoveRSVP(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (s *stubQuerier) GetMeetupOrganizerID(context.Context, uuid.UUID) (uuid.UUID, error) {
	return organizerID, nil
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

	if err := cached.AddRSVP(context.Background(), meetupID, viewerID); err != nil {
		t.Fatalf("AddRSVP: %v", err)
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
