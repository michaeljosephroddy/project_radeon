package places

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	appcache "github.com/project_radeon/api/pkg/cache"
)

const (
	placeAutocompleteTTL   = 24 * time.Hour
	placeCacheSchema       = "1"
	placeAutocompleteScope = "autocomplete"
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
	return store.BumpVersions(ctx, placesVersionKey(store))
}

func (s *cachedStore) AutocompletePlaces(ctx context.Context, params AutocompleteParams) ([]PlaceSuggestion, error) {
	version, err := s.cache.GetVersion(ctx, placesVersionKey(s.cache))
	if err != nil {
		return s.inner.AutocompletePlaces(ctx, params)
	}

	key := placeAutocompleteCacheKey(s.cache, version, params)
	var suggestions []PlaceSuggestion
	if err := s.cache.ReadThrough(ctx, key, placeAutocompleteTTL, &suggestions, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.AutocompletePlaces(ctx, params)
		if err != nil {
			return err
		}
		*dest.(*[]PlaceSuggestion) = loaded
		return nil
	}); err != nil {
		return nil, err
	}
	return suggestions, nil
}

func placesVersionKey(cache appcache.Store) string {
	return cache.Key("places", "version")
}

func placeAutocompleteCacheKey(cache appcache.Store, version int64, params AutocompleteParams) string {
	return cache.Key(
		"places", placeAutocompleteScope,
		"schema", placeCacheSchema,
		"v", strconv.FormatInt(version, 10),
		"q", encodePlaceCachePart(params.Query),
		"country", encodePlaceCachePart(params.CountryCode),
		"limit", strconv.Itoa(params.Limit),
	)
}

func encodePlaceCachePart(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "-"
	}
	return url.QueryEscape(normalized)
}
