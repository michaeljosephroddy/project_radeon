package meetups

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	appcache "github.com/project_radeon/api/pkg/cache"
)

const (
	meetupsListTTL  = 60 * time.Second
	meetupDetailTTL = 5 * time.Minute
	myMeetupsTTL    = 60 * time.Second
	attendeesTTL    = 60 * time.Second
	categoriesTTL   = 30 * time.Minute
)

type cachedStore struct {
	inner Querier
	cache appcache.Store
}

func NewCachedStore(inner Querier, store appcache.Store) Querier {
	if store == nil {
		store = appcache.NewDisabled()
	}
	return &cachedStore{inner: inner, cache: store}
}

func (s *cachedStore) ListCategories(ctx context.Context) ([]MeetupCategory, error) {
	version, err := s.cache.GetVersion(ctx, s.categoriesVersionKey())
	if err != nil {
		return s.inner.ListCategories(ctx)
	}
	key := s.cache.Key("meetups", "categories", "v", strconv.FormatInt(version, 10))
	var categories []MeetupCategory
	if err := s.cache.ReadThrough(ctx, key, categoriesTTL, &categories, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListCategories(ctx)
		if err != nil {
			return err
		}
		*dest.(*[]MeetupCategory) = loaded
		return nil
	}); err != nil {
		return nil, err
	}
	return categories, nil
}

func (s *cachedStore) DiscoverMeetups(ctx context.Context, userID uuid.UUID, params DiscoverMeetupsParams) (*CursorPage[Meetup], error) {
	version, err := s.cache.GetVersion(ctx, s.meetupsVersionKey())
	if err != nil {
		return s.inner.DiscoverMeetups(ctx, userID, params)
	}
	key := s.cache.Key(
		"meetups", "discover",
		"v", strconv.FormatInt(version, 10),
		"pipeline", discoverPipelineCacheVersion(params.Sort),
		"viewer", userID.String(),
		"q", encodeMeetupPart(params.Query),
		"category", encodeMeetupPart(params.CategorySlug),
		"city", encodeMeetupPart(params.City),
		"distance", encodeOptionalInt(params.DistanceKM),
		"type", encodeMeetupPart(params.EventType),
		"date_preset", encodeMeetupPart(params.DatePreset),
		"date_from", encodeOptionalTime(params.DateFrom),
		"date_to", encodeOptionalTime(params.DateTo),
		"day", encodeIntSlice(params.DayOfWeek),
		"time", encodeStringSlice(params.TimeOfDay),
		"open", strconv.FormatBool(params.OpenSpotsOnly),
		"sort", encodeMeetupPart(params.Sort),
		"cursor", encodeMeetupPart(params.Cursor),
		"limit", strconv.Itoa(params.Limit),
	)
	var page CursorPage[Meetup]
	if err := s.cache.ReadThrough(ctx, key, meetupsListTTL, &page, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.DiscoverMeetups(ctx, userID, params)
		if err != nil {
			return err
		}
		*dest.(*CursorPage[Meetup]) = *loaded
		return nil
	}); err != nil {
		return nil, err
	}
	return &page, nil
}

func discoverPipelineCacheVersion(sortKey string) string {
	if sortKey == "recommended" {
		return recommendedPipelineVersion
	}
	return "legacy"
}

func (s *cachedStore) ListMyMeetups(ctx context.Context, userID uuid.UUID, params MyMeetupsParams) (*CursorPage[Meetup], error) {
	version, err := s.cache.GetVersion(ctx, s.myMeetupsVersionKey(userID))
	if err != nil {
		return s.inner.ListMyMeetups(ctx, userID, params)
	}
	globalVersion, err := s.cache.GetVersion(ctx, s.meetupsVersionKey())
	if err != nil {
		return s.inner.ListMyMeetups(ctx, userID, params)
	}
	key := s.cache.Key(
		"meetups", "mine",
		"user", userID.String(),
		"v", strconv.FormatInt(version, 10),
		"global_v", strconv.FormatInt(globalVersion, 10),
		"scope", encodeMeetupPart(params.Scope),
		"cursor", encodeMeetupPart(params.Cursor),
		"limit", strconv.Itoa(params.Limit),
	)
	var page CursorPage[Meetup]
	if err := s.cache.ReadThrough(ctx, key, myMeetupsTTL, &page, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListMyMeetups(ctx, userID, params)
		if err != nil {
			return err
		}
		*dest.(*CursorPage[Meetup]) = *loaded
		return nil
	}); err != nil {
		return nil, err
	}
	return &page, nil
}

func (s *cachedStore) GetMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error) {
	version, err := s.cache.GetVersion(ctx, s.meetupVersionKey(meetupID))
	if err != nil {
		return s.inner.GetMeetup(ctx, meetupID, userID)
	}
	key := s.cache.Key(
		"meetups", "detail",
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

func (s *cachedStore) CreateMeetup(ctx context.Context, userID uuid.UUID, input CreateMeetupInput) (*Meetup, error) {
	meetup, err := s.inner.CreateMeetup(ctx, userID, input)
	if err != nil {
		return nil, err
	}
	s.bumpMeetupVersions(ctx, meetup.ID, userID)
	return meetup, nil
}

func (s *cachedStore) UpdateMeetup(ctx context.Context, meetupID, userID uuid.UUID, input UpdateMeetupInput) (*Meetup, error) {
	meetup, err := s.inner.UpdateMeetup(ctx, meetupID, userID, input)
	if err != nil {
		return nil, err
	}
	s.bumpMeetupVersions(ctx, meetup.ID, userID)
	return meetup, nil
}

func (s *cachedStore) PublishMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error) {
	meetup, err := s.inner.PublishMeetup(ctx, meetupID, userID)
	if err != nil {
		return nil, err
	}
	s.bumpMeetupVersions(ctx, meetup.ID, userID)
	return meetup, nil
}

func (s *cachedStore) CancelMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error) {
	meetup, err := s.inner.CancelMeetup(ctx, meetupID, userID)
	if err != nil {
		return nil, err
	}
	s.bumpMeetupVersions(ctx, meetup.ID, userID)
	return meetup, nil
}

func (s *cachedStore) DeleteMeetup(ctx context.Context, meetupID, userID uuid.UUID) error {
	if err := s.inner.DeleteMeetup(ctx, meetupID, userID); err != nil {
		return err
	}
	s.bumpMeetupVersions(ctx, meetupID, userID)
	return nil
}

func (s *cachedStore) GetAttendees(ctx context.Context, meetupID uuid.UUID, limit, offset int) ([]Attendee, error) {
	version, err := s.cache.GetVersion(ctx, s.meetupVersionKey(meetupID))
	if err != nil {
		return s.inner.GetAttendees(ctx, meetupID, limit, offset)
	}
	key := s.cache.Key(
		"meetups", "attendees",
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

func (s *cachedStore) GetWaitlist(ctx context.Context, meetupID, userID uuid.UUID, limit, offset int) ([]Attendee, error) {
	version, err := s.cache.GetVersion(ctx, s.meetupVersionKey(meetupID))
	if err != nil {
		return s.inner.GetWaitlist(ctx, meetupID, userID, limit, offset)
	}
	key := s.cache.Key(
		"meetups", "waitlist",
		"id", meetupID.String(),
		"viewer", userID.String(),
		"v", strconv.FormatInt(version, 10),
		"limit", strconv.Itoa(limit),
		"offset", strconv.Itoa(offset),
	)
	var attendees []Attendee
	if err := s.cache.ReadThrough(ctx, key, attendeesTTL, &attendees, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.GetWaitlist(ctx, meetupID, userID, limit, offset)
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

func (s *cachedStore) ToggleRSVP(ctx context.Context, meetupID, userID uuid.UUID) (*RSVPResult, error) {
	result, err := s.inner.ToggleRSVP(ctx, meetupID, userID)
	if err != nil {
		return nil, err
	}
	s.bumpMeetupVersions(ctx, meetupID, userID)
	return result, nil
}

func (s *cachedStore) bumpMeetupVersions(ctx context.Context, meetupID, userID uuid.UUID) {
	_ = s.cache.BumpVersions(ctx,
		s.meetupsVersionKey(),
		s.meetupVersionKey(meetupID),
		s.myMeetupsVersionKey(userID),
	)
}

func (s *cachedStore) meetupsVersionKey() string {
	return s.cache.Key("ver", "meetups")
}

func (s *cachedStore) categoriesVersionKey() string {
	return s.cache.Key("ver", "meetups", "categories")
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

func encodeOptionalTime(value *time.Time) string {
	if value == nil {
		return "none"
	}
	return value.UTC().Format(time.RFC3339)
}

func encodeOptionalInt(value *int) string {
	if value == nil {
		return "none"
	}
	return strconv.Itoa(*value)
}

func encodeIntSlice(values []int) string {
	if len(values) == 0 {
		return "none"
	}
	encoded := make([]string, 0, len(values))
	for _, value := range values {
		encoded = append(encoded, strconv.Itoa(value))
	}
	return url.QueryEscape(strings.Join(encoded, ","))
}

func encodeStringSlice(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return url.QueryEscape(strings.Join(values, ","))
}
