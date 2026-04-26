package meetups

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
	appcache "github.com/project_radeon/api/pkg/cache"
)

const (
	meetupsListTTL   = 60 * time.Second
	meetupDetailTTL  = 5 * time.Minute
	myMeetupsTTL     = 60 * time.Second
	attendeesTTL     = 60 * time.Second
	attendeePreviews = 3
)

type cachedStore struct {
	inner Querier
	cache appcache.Store
}

type meetupOrganizerLookup interface {
	GetMeetupOrganizerID(ctx context.Context, meetupID uuid.UUID) (uuid.UUID, error)
}

func NewCachedStore(inner Querier, store appcache.Store) Querier {
	if store == nil {
		store = appcache.NewDisabled()
	}
	return &cachedStore{inner: inner, cache: store}
}

func (s *cachedStore) ListMeetups(ctx context.Context, userID uuid.UUID, cityFilter, queryFilter string, limit, offset int) ([]Meetup, error) {
	globalVersion, err := s.cache.GetVersion(ctx, s.meetupsVersionKey())
	if err != nil {
		return s.inner.ListMeetups(ctx, userID, cityFilter, queryFilter, limit, offset)
	}

	key := s.cache.Key(
		"meetups",
		"list",
		"v", strconv.FormatInt(globalVersion, 10),
		"viewer", userID.String(),
		"city", encodeMeetupPart(cityFilter),
		"query", encodeMeetupPart(queryFilter),
		"limit", strconv.Itoa(limit),
		"offset", strconv.Itoa(offset),
	)

	var meetups []Meetup
	if err := s.cache.ReadThrough(ctx, key, meetupsListTTL, &meetups, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListMeetups(ctx, userID, cityFilter, queryFilter, limit, offset)
		if err != nil {
			return err
		}
		if err := s.inner.AttachAttendeePreviews(ctx, loaded, attendeePreviews); err != nil {
			return err
		}
		*dest.(*[]Meetup) = loaded
		return nil
	}); err != nil {
		return nil, err
	}

	return meetups, nil
}

func (s *cachedStore) ListMyMeetups(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Meetup, error) {
	version, err := s.cache.GetVersion(ctx, s.myMeetupsVersionKey(userID))
	if err != nil {
		return s.inner.ListMyMeetups(ctx, userID, limit, offset)
	}

	key := s.cache.Key(
		"meetups",
		"mine",
		"user", userID.String(),
		"v", strconv.FormatInt(version, 10),
		"limit", strconv.Itoa(limit),
		"offset", strconv.Itoa(offset),
	)

	var meetups []Meetup
	if err := s.cache.ReadThrough(ctx, key, myMeetupsTTL, &meetups, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListMyMeetups(ctx, userID, limit, offset)
		if err != nil {
			return err
		}
		if err := s.inner.AttachAttendeePreviews(ctx, loaded, attendeePreviews); err != nil {
			return err
		}
		*dest.(*[]Meetup) = loaded
		return nil
	}); err != nil {
		return nil, err
	}

	return meetups, nil
}

func (s *cachedStore) AttachAttendeePreviews(ctx context.Context, meetups []Meetup, previewLimit int) error {
	return s.inner.AttachAttendeePreviews(ctx, meetups, previewLimit)
}

func (s *cachedStore) GetMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error) {
	version, err := s.cache.GetVersion(ctx, s.meetupVersionKey(meetupID))
	if err != nil {
		return s.inner.GetMeetup(ctx, meetupID, userID)
	}

	key := s.cache.Key(
		"meetup",
		"id", meetupID.String(),
		"viewer", userID.String(),
		"v", strconv.FormatInt(version, 10),
	)

	var meetup Meetup
	if err := s.cache.ReadThrough(ctx, key, meetupDetailTTL, &meetup, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.GetMeetup(ctx, meetupID, userID)
		if err != nil {
			return err
		}
		*dest.(*Meetup) = *loaded
		return nil
	}); err != nil {
		return nil, err
	}

	return &meetup, nil
}

func (s *cachedStore) CreateMeetup(ctx context.Context, userID uuid.UUID, title string, description *string, city string, startsAt time.Time, capacity *int) (*Meetup, error) {
	meetup, err := s.inner.CreateMeetup(ctx, userID, title, description, city, startsAt, capacity)
	if err != nil {
		return nil, err
	}

	_ = s.cache.BumpVersions(ctx,
		s.meetupsVersionKey(),
		s.meetupVersionKey(meetup.ID),
		s.myMeetupsVersionKey(userID),
	)
	return meetup, nil
}

func (s *cachedStore) GetAttendees(ctx context.Context, meetupID uuid.UUID, limit, offset int) ([]Attendee, error) {
	version, err := s.cache.GetVersion(ctx, s.meetupVersionKey(meetupID))
	if err != nil {
		return s.inner.GetAttendees(ctx, meetupID, limit, offset)
	}

	key := s.cache.Key(
		"meetup",
		"attendees",
		"id", meetupID.String(),
		"v", strconv.FormatInt(version, 10),
		"limit", strconv.Itoa(limit),
		"offset", strconv.Itoa(offset),
	)

	var attendees []Attendee
	if err := s.cache.ReadThrough(ctx, key, attendeesTTL, &attendees, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.GetAttendees(ctx, meetupID, limit, offset)
		if err != nil {
			return err
		}
		*dest.(*[]Attendee) = loaded
		return nil
	}); err != nil {
		return nil, err
	}

	return attendees, nil
}

func (s *cachedStore) GetMeetupCapacity(ctx context.Context, meetupID uuid.UUID) (*int, int, error) {
	return s.inner.GetMeetupCapacity(ctx, meetupID)
}

func (s *cachedStore) IsRSVPd(ctx context.Context, meetupID, userID uuid.UUID) (bool, error) {
	return s.inner.IsRSVPd(ctx, meetupID, userID)
}

func (s *cachedStore) AddRSVP(ctx context.Context, meetupID, userID uuid.UUID) error {
	if err := s.inner.AddRSVP(ctx, meetupID, userID); err != nil {
		return err
	}

	s.bumpMeetupVersions(ctx, meetupID)
	return nil
}

func (s *cachedStore) RemoveRSVP(ctx context.Context, meetupID, userID uuid.UUID) error {
	if err := s.inner.RemoveRSVP(ctx, meetupID, userID); err != nil {
		return err
	}

	s.bumpMeetupVersions(ctx, meetupID)
	return nil
}

func (s *cachedStore) bumpMeetupVersions(ctx context.Context, meetupID uuid.UUID) {
	keys := []string{
		s.meetupsVersionKey(),
		s.meetupVersionKey(meetupID),
	}
	if organizerID, ok := s.lookupOrganizerID(ctx, meetupID); ok {
		keys = append(keys, s.myMeetupsVersionKey(organizerID))
	}
	_ = s.cache.BumpVersions(ctx, keys...)
}

func (s *cachedStore) lookupOrganizerID(ctx context.Context, meetupID uuid.UUID) (uuid.UUID, bool) {
	lookup, ok := s.inner.(meetupOrganizerLookup)
	if !ok {
		return uuid.Nil, false
	}

	organizerID, err := lookup.GetMeetupOrganizerID(ctx, meetupID)
	if err != nil {
		return uuid.Nil, false
	}
	return organizerID, true
}

func (s *cachedStore) meetupsVersionKey() string {
	return s.cache.Key("ver", "meetups")
}

func (s *cachedStore) meetupVersionKey(meetupID uuid.UUID) string {
	return s.cache.Key("ver", "meetup", meetupID.String())
}

func (s *cachedStore) myMeetupsVersionKey(userID uuid.UUID) string {
	return s.cache.Key("ver", "meetups", "mine", userID.String())
}

func encodeMeetupPart(value string) string {
	if value == "" {
		return "none"
	}
	return url.QueryEscape(value)
}
