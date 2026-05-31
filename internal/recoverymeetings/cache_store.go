package recoverymeetings

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	appcache "github.com/project_radeon/api/pkg/cache"
)

const (
	recoveryMeetingListTTL     = 15 * time.Minute
	recoveryMeetingDetailTTL   = time.Hour
	recoveryMeetingCacheSchema = "2"
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

func BumpCacheVersion(ctx context.Context, store appcache.Store) error {
	if store == nil {
		store = appcache.NewDisabled()
	}
	return store.BumpVersions(ctx, recoveryMeetingsVersionKey(store))
}

func (s *cachedStore) ListRecoveryMeetings(ctx context.Context, params ListParams) (*CursorPage[RecoveryMeeting], error) {
	version, err := s.cache.GetVersion(ctx, recoveryMeetingsVersionKey(s.cache))
	if err != nil {
		return s.inner.ListRecoveryMeetings(ctx, params)
	}

	key := recoveryMeetingListCacheKey(s.cache, version, params)
	var page CursorPage[RecoveryMeeting]
	if err := s.cache.ReadThrough(ctx, key, recoveryMeetingListTTL, &page, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListRecoveryMeetings(ctx, params)
		if err != nil {
			return err
		}
		*dest.(*CursorPage[RecoveryMeeting]) = *loaded
		return nil
	}); err != nil {
		return nil, err
	}
	return &page, nil
}

func (s *cachedStore) GetRecoveryMeeting(ctx context.Context, id uuid.UUID) (*RecoveryMeeting, error) {
	version, err := s.cache.GetVersion(ctx, recoveryMeetingsVersionKey(s.cache))
	if err != nil {
		return s.inner.GetRecoveryMeeting(ctx, id)
	}

	key := s.cache.Key(
		"recovery_meetings", "detail",
		"schema", recoveryMeetingCacheSchema,
		"v", strconv.FormatInt(version, 10),
		"id", id.String(),
	)
	var meeting RecoveryMeeting
	if err := s.cache.ReadThrough(ctx, key, recoveryMeetingDetailTTL, &meeting, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.GetRecoveryMeeting(ctx, id)
		if err != nil {
			return err
		}
		*dest.(*RecoveryMeeting) = *loaded
		return nil
	}); err != nil {
		return nil, err
	}
	return &meeting, nil
}

func recoveryMeetingsVersionKey(cache appcache.Store) string {
	return cache.Key("recovery_meetings", "version")
}

func encodeRecoveryMeetingCachePart(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "-"
	}
	return url.QueryEscape(normalized)
}

func encodeRecoveryMeetingFellowships(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	normalized := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		fellowship := strings.ToLower(strings.TrimSpace(value))
		if fellowship == "" {
			continue
		}
		if _, ok := seen[fellowship]; ok {
			continue
		}
		seen[fellowship] = struct{}{}
		normalized = append(normalized, fellowship)
	}
	if len(normalized) == 0 {
		return "-"
	}
	sort.Strings(normalized)
	return url.QueryEscape(strings.Join(normalized, ","))
}

func recoveryMeetingListCacheKey(cache appcache.Store, version int64, params ListParams) string {
	return cache.Key(
		"recovery_meetings", "list",
		"schema", recoveryMeetingCacheSchema,
		"v", strconv.FormatInt(version, 10),
		"q", encodeRecoveryMeetingCachePart(params.Query),
		"fellowship", encodeRecoveryMeetingFellowships(params.Fellowships),
		"country", encodeRecoveryMeetingCachePart(params.Country),
		"region", encodeRecoveryMeetingCachePart(params.Region),
		"city", encodeRecoveryMeetingCachePart(params.City),
		"location", encodeRecoveryMeetingCachePart(params.Location),
		"place", encodeRecoveryMeetingUUID(params.PlaceID),
		"type", encodeRecoveryMeetingCachePart(params.MeetingType),
		"day", encodeRecoveryMeetingOptionalInt(params.DayOfWeek),
		"cursor", encodeRecoveryMeetingCachePart(params.Cursor),
		"limit", strconv.Itoa(params.Limit),
	)
}

func encodeRecoveryMeetingUUID(value *uuid.UUID) string {
	if value == nil {
		return "-"
	}
	return value.String()
}

func encodeRecoveryMeetingOptionalInt(value *int) string {
	if value == nil {
		return "-"
	}
	return strconv.Itoa(*value)
}
