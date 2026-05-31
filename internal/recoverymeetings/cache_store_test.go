package recoverymeetings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	appcache "github.com/project_radeon/api/pkg/cache"
)

type stubRecoveryQuerier struct {
	listCalls   int
	detailCalls int

	listFn   func(context.Context, ListParams) (*CursorPage[RecoveryMeeting], error)
	detailFn func(context.Context, uuid.UUID) (*RecoveryMeeting, error)
}

func (s *stubRecoveryQuerier) ListRecoveryMeetings(ctx context.Context, params ListParams) (*CursorPage[RecoveryMeeting], error) {
	s.listCalls++
	if s.listFn != nil {
		return s.listFn(ctx, params)
	}
	return &CursorPage[RecoveryMeeting]{
		Items: []RecoveryMeeting{{
			ID:          uuid.New(),
			Fellowship:  "aa",
			SourceID:    "test",
			Name:        "Cached meeting",
			MeetingType: "in_person",
			UpdatedAt:   time.Unix(1, 0).UTC(),
		}},
		Limit: params.Limit,
	}, nil
}

func (s *stubRecoveryQuerier) GetRecoveryMeeting(ctx context.Context, id uuid.UUID) (*RecoveryMeeting, error) {
	s.detailCalls++
	if s.detailFn != nil {
		return s.detailFn(ctx, id)
	}
	return &RecoveryMeeting{
		ID:          id,
		Fellowship:  "aa",
		SourceID:    "test",
		Name:        "Cached meeting",
		MeetingType: "in_person",
		UpdatedAt:   time.Unix(1, 0).UTC(),
	}, nil
}

type fakeCacheStore struct {
	mu          sync.Mutex
	enabled     bool
	prefix      string
	values      map[string][]byte
	versions    map[string]int64
	versionErr  error
	readThrough bool
}

func newFakeCacheStore() *fakeCacheStore {
	return &fakeCacheStore{
		enabled:  true,
		prefix:   "test",
		values:   map[string][]byte{},
		versions: map[string]int64{},
	}
}

func (s *fakeCacheStore) Enabled() bool {
	return s.enabled
}

func (s *fakeCacheStore) Key(parts ...string) string {
	items := []string{s.prefix}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		items = append(items, part)
	}
	return strings.Join(items, ":")
}

func (s *fakeCacheStore) GetJSON(_ context.Context, key string, dest any) (bool, error) {
	s.mu.Lock()
	payload, ok := s.values[key]
	s.mu.Unlock()
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(payload, dest)
}

func (s *fakeCacheStore) SetJSON(_ context.Context, key string, value any, _ time.Duration) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.values[key] = payload
	s.mu.Unlock()
	return nil
}

func (s *fakeCacheStore) PublishJSON(context.Context, string, any) error {
	return nil
}

func (s *fakeCacheStore) Subscribe(context.Context, string) (appcache.Subscription, error) {
	return nil, errors.New("not implemented")
}

func (s *fakeCacheStore) GetVersion(_ context.Context, key string) (int64, error) {
	if s.versionErr != nil {
		return 0, s.versionErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.versions[key], nil
}

func (s *fakeCacheStore) BumpVersions(_ context.Context, keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		s.versions[key]++
	}
	return nil
}

func (s *fakeCacheStore) WithJitter(ttl time.Duration) time.Duration {
	return ttl
}

func (s *fakeCacheStore) ReadThrough(ctx context.Context, key string, ttl time.Duration, dest any, loader func(context.Context, any) error) error {
	s.readThrough = true
	found, err := s.GetJSON(ctx, key, dest)
	if err != nil || found {
		return err
	}
	if err := loader(ctx, dest); err != nil {
		return err
	}
	return s.SetJSON(ctx, key, dest, ttl)
}

func TestCachedStoreListRecoveryMeetingsCachesIdenticalParams(t *testing.T) {
	ctx := context.Background()
	cacheStore := newFakeCacheStore()
	inner := &stubRecoveryQuerier{}
	store := NewCachedStore(inner, cacheStore)
	params := ListParams{
		Query:       "  Zoom ",
		Fellowships: []string{"ca", "aa"},
		Country:     "Ireland",
		Location:    "Dublin",
		Limit:       20,
	}

	first, err := store.ListRecoveryMeetings(ctx, params)
	if err != nil {
		t.Fatalf("first list: %v", err)
	}
	second, err := store.ListRecoveryMeetings(ctx, params)
	if err != nil {
		t.Fatalf("second list: %v", err)
	}

	if inner.listCalls != 1 {
		t.Fatalf("inner list calls = %d, want 1", inner.listCalls)
	}
	if len(first.Items) != 1 || len(second.Items) != 1 || first.Items[0].ID != second.Items[0].ID {
		t.Fatalf("cached pages differ: first=%+v second=%+v", first, second)
	}
}

func TestCachedStoreListRecoveryMeetingsNormalizesFellowshipOrder(t *testing.T) {
	placeID := uuid.New()
	day := 1
	first := ListParams{
		Query:       "Zoom",
		Fellowships: []string{"ca", "aa"},
		Country:     "Ireland",
		Region:      "Dublin",
		Location:    "Dublin",
		PlaceID:     &placeID,
		MeetingType: "online",
		DayOfWeek:   &day,
		Cursor:      "cursor",
		Limit:       20,
	}
	second := first
	second.Fellowships = []string{"AA", "ca", "aa"}

	cacheStore := newFakeCacheStore()
	firstKey := recoveryMeetingListCacheKey(cacheStore, 7, first)
	secondKey := recoveryMeetingListCacheKey(cacheStore, 7, second)
	if firstKey != secondKey {
		t.Fatalf("keys differ:\n%s\n%s", firstKey, secondKey)
	}
}

func TestCachedStoreListRecoveryMeetingsSeparatesCursorAndLimit(t *testing.T) {
	cacheStore := newFakeCacheStore()
	base := ListParams{Fellowships: []string{"aa"}, Limit: 20}
	withCursor := base
	withCursor.Cursor = "next"
	withLimit := base
	withLimit.Limit = 30

	baseKey := recoveryMeetingListCacheKey(cacheStore, 1, base)
	cursorKey := recoveryMeetingListCacheKey(cacheStore, 1, withCursor)
	limitKey := recoveryMeetingListCacheKey(cacheStore, 1, withLimit)
	if baseKey == cursorKey {
		t.Fatalf("cursor key was not distinct: %s", baseKey)
	}
	if baseKey == limitKey {
		t.Fatalf("limit key was not distinct: %s", baseKey)
	}
}

func TestCachedStoreVersionBumpInvalidatesList(t *testing.T) {
	ctx := context.Background()
	cacheStore := newFakeCacheStore()
	inner := &stubRecoveryQuerier{}
	store := NewCachedStore(inner, cacheStore)
	params := ListParams{Fellowships: []string{"aa"}, Limit: 20}

	if _, err := store.ListRecoveryMeetings(ctx, params); err != nil {
		t.Fatalf("first list: %v", err)
	}
	if _, err := store.ListRecoveryMeetings(ctx, params); err != nil {
		t.Fatalf("second list: %v", err)
	}
	if inner.listCalls != 1 {
		t.Fatalf("inner list calls before bump = %d, want 1", inner.listCalls)
	}
	if err := BumpCacheVersion(ctx, cacheStore); err != nil {
		t.Fatalf("bump version: %v", err)
	}
	if _, err := store.ListRecoveryMeetings(ctx, params); err != nil {
		t.Fatalf("after bump list: %v", err)
	}
	if inner.listCalls != 2 {
		t.Fatalf("inner list calls after bump = %d, want 2", inner.listCalls)
	}
}

func TestCachedStoreVersionErrorFallsBackToInner(t *testing.T) {
	ctx := context.Background()
	cacheStore := newFakeCacheStore()
	cacheStore.versionErr = fmt.Errorf("redis unavailable")
	inner := &stubRecoveryQuerier{}
	store := NewCachedStore(inner, cacheStore)

	if _, err := store.ListRecoveryMeetings(ctx, ListParams{Limit: 20}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if inner.listCalls != 1 {
		t.Fatalf("inner list calls = %d, want 1", inner.listCalls)
	}
	if cacheStore.readThrough {
		t.Fatal("read-through cache should not be used after version error")
	}
}

func TestCachedStoreGetRecoveryMeetingCachesDetail(t *testing.T) {
	ctx := context.Background()
	cacheStore := newFakeCacheStore()
	inner := &stubRecoveryQuerier{}
	store := NewCachedStore(inner, cacheStore)
	id := uuid.New()

	first, err := store.GetRecoveryMeeting(ctx, id)
	if err != nil {
		t.Fatalf("first detail: %v", err)
	}
	second, err := store.GetRecoveryMeeting(ctx, id)
	if err != nil {
		t.Fatalf("second detail: %v", err)
	}

	if inner.detailCalls != 1 {
		t.Fatalf("inner detail calls = %d, want 1", inner.detailCalls)
	}
	if first.ID != second.ID {
		t.Fatalf("cached detail IDs differ: %s != %s", first.ID, second.ID)
	}
}
