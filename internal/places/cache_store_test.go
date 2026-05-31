package places

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	appcache "github.com/project_radeon/api/pkg/cache"
)

type stubPlacesQuerier struct {
	autocompleteCalls int
}

func (s *stubPlacesQuerier) AutocompletePlaces(context.Context, AutocompleteParams) ([]PlaceSuggestion, error) {
	s.autocompleteCalls++
	return []PlaceSuggestion{{
		ID:          uuid.New(),
		Label:       "Barcelona, Catalonia, Spain",
		Name:        "Barcelona",
		Country:     "Spain",
		CountryCode: "ES",
		Latitude:    41.38879,
		Longitude:   2.15899,
		Population:  1621537,
		Source:      "geonames",
	}}, nil
}

type fakePlacesCacheStore struct {
	mu         sync.Mutex
	prefix     string
	values     map[string][]byte
	versions   map[string]int64
	versionErr error
}

func newFakePlacesCacheStore() *fakePlacesCacheStore {
	return &fakePlacesCacheStore{
		prefix:   "test",
		values:   map[string][]byte{},
		versions: map[string]int64{},
	}
}

func (s *fakePlacesCacheStore) Enabled() bool {
	return true
}

func (s *fakePlacesCacheStore) Key(parts ...string) string {
	items := []string{s.prefix}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		items = append(items, part)
	}
	return strings.Join(items, ":")
}

func (s *fakePlacesCacheStore) GetJSON(_ context.Context, key string, dest any) (bool, error) {
	s.mu.Lock()
	payload, ok := s.values[key]
	s.mu.Unlock()
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(payload, dest)
}

func (s *fakePlacesCacheStore) SetJSON(_ context.Context, key string, value any, _ time.Duration) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.values[key] = payload
	s.mu.Unlock()
	return nil
}

func (s *fakePlacesCacheStore) PublishJSON(context.Context, string, any) error {
	return nil
}

func (s *fakePlacesCacheStore) Subscribe(context.Context, string) (appcache.Subscription, error) {
	return nil, errors.New("not implemented")
}

func (s *fakePlacesCacheStore) GetVersion(_ context.Context, key string) (int64, error) {
	if s.versionErr != nil {
		return 0, s.versionErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.versions[key], nil
}

func (s *fakePlacesCacheStore) BumpVersions(_ context.Context, keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		s.versions[key]++
	}
	return nil
}

func (s *fakePlacesCacheStore) WithJitter(ttl time.Duration) time.Duration {
	return ttl
}

func (s *fakePlacesCacheStore) ReadThrough(ctx context.Context, key string, ttl time.Duration, dest any, loader func(context.Context, any) error) error {
	found, err := s.GetJSON(ctx, key, dest)
	if err != nil || found {
		return err
	}
	if err := loader(ctx, dest); err != nil {
		return err
	}
	return s.SetJSON(ctx, key, dest, ttl)
}

func TestCachedStoreAutocompletePlacesCachesIdenticalParams(t *testing.T) {
	ctx := context.Background()
	cacheStore := newFakePlacesCacheStore()
	inner := &stubPlacesQuerier{}
	store := NewCachedStore(inner, cacheStore)
	params := AutocompleteParams{Query: " Barcel ", CountryCode: "ES", Limit: 8}

	first, err := store.AutocompletePlaces(ctx, params)
	if err != nil {
		t.Fatalf("first autocomplete: %v", err)
	}
	second, err := store.AutocompletePlaces(ctx, params)
	if err != nil {
		t.Fatalf("second autocomplete: %v", err)
	}
	if inner.autocompleteCalls != 1 {
		t.Fatalf("autocomplete calls = %d, want 1", inner.autocompleteCalls)
	}
	if len(first) != 1 || len(second) != 1 || first[0].ID != second[0].ID {
		t.Fatalf("cached suggestions differ: first=%+v second=%+v", first, second)
	}
}

func TestCachedStoreAutocompletePlacesVersionBumpInvalidates(t *testing.T) {
	ctx := context.Background()
	cacheStore := newFakePlacesCacheStore()
	inner := &stubPlacesQuerier{}
	store := NewCachedStore(inner, cacheStore)
	params := AutocompleteParams{Query: "barcel", Limit: 8}

	if _, err := store.AutocompletePlaces(ctx, params); err != nil {
		t.Fatalf("first autocomplete: %v", err)
	}
	if _, err := store.AutocompletePlaces(ctx, params); err != nil {
		t.Fatalf("second autocomplete: %v", err)
	}
	if inner.autocompleteCalls != 1 {
		t.Fatalf("calls before bump = %d, want 1", inner.autocompleteCalls)
	}
	if err := BumpCacheVersion(ctx, cacheStore); err != nil {
		t.Fatalf("bump cache version: %v", err)
	}
	if _, err := store.AutocompletePlaces(ctx, params); err != nil {
		t.Fatalf("after bump autocomplete: %v", err)
	}
	if inner.autocompleteCalls != 2 {
		t.Fatalf("calls after bump = %d, want 2", inner.autocompleteCalls)
	}
}
