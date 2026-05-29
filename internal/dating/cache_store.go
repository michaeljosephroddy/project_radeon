package dating

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

const datingDiscoverRankedTTL = 5 * time.Minute

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

func (s *cachedStore) GetMyProfile(ctx context.Context, userID uuid.UUID) (*DatingProfile, error) {
	return s.inner.GetMyProfile(ctx, userID)
}

func (s *cachedStore) GetProfile(ctx context.Context, viewerID, profileID uuid.UUID) (*DatingProfile, error) {
	return s.inner.GetProfile(ctx, viewerID, profileID)
}

func (s *cachedStore) UpdateMyProfile(ctx context.Context, userID uuid.UUID, input UpdateProfileInput) (*DatingProfile, error) {
	profile, err := s.inner.UpdateMyProfile(ctx, userID, input)
	if err != nil {
		return nil, err
	}
	_ = s.cache.BumpVersions(ctx, s.viewerVersionKey(userID), s.globalVersionKey())
	return profile, nil
}

func (s *cachedStore) ListInterests(ctx context.Context) ([]string, error) {
	return s.inner.ListInterests(ctx)
}

func (s *cachedStore) AddPhoto(ctx context.Context, userID uuid.UUID, imageURL string, width, height int) (*DatingProfile, error) {
	profile, err := s.inner.AddPhoto(ctx, userID, imageURL, width, height)
	if err != nil {
		return nil, err
	}
	_ = s.cache.BumpVersions(ctx, s.viewerVersionKey(userID), s.globalVersionKey())
	return profile, nil
}

func (s *cachedStore) DeletePhoto(ctx context.Context, userID, photoID uuid.UUID) (*DatingProfile, error) {
	profile, err := s.inner.DeletePhoto(ctx, userID, photoID)
	if err != nil {
		return nil, err
	}
	_ = s.cache.BumpVersions(ctx, s.viewerVersionKey(userID), s.globalVersionKey())
	return profile, nil
}

func (s *cachedStore) ReorderPhotos(ctx context.Context, userID uuid.UUID, photoIDs []uuid.UUID) (*DatingProfile, error) {
	profile, err := s.inner.ReorderPhotos(ctx, userID, photoIDs)
	if err != nil {
		return nil, err
	}
	_ = s.cache.BumpVersions(ctx, s.viewerVersionKey(userID), s.globalVersionKey())
	return profile, nil
}

func (s *cachedStore) Discover(ctx context.Context, params DiscoverParams) ([]DatingProfile, error) {
	store, ok := s.inner.(*pgStore)
	if !ok || !s.cache.Enabled() || strings.TrimSpace(params.CursorRequestID) == "" {
		return s.inner.Discover(ctx, params)
	}

	viewerVersion, err := s.cache.GetVersion(ctx, s.viewerVersionKey(params.CurrentUserID))
	if err != nil {
		return s.inner.Discover(ctx, params)
	}
	globalVersion, err := s.cache.GetVersion(ctx, s.globalVersionKey())
	if err != nil {
		return s.inner.Discover(ctx, params)
	}

	key := s.cache.Key(
		"dating", "discover", "ranked",
		"request", params.CursorRequestID,
		"viewer_v", strconv.FormatInt(viewerVersion, 10),
		"global_v", strconv.FormatInt(globalVersion, 10),
		"viewer", params.CurrentUserID.String(),
		"gender", cacheStringPart(params.Gender),
		"sobriety", cacheStringPart(params.Sobriety),
		"age_min", cacheIntPart(params.AgeMin),
		"age_max", cacheIntPart(params.AgeMax),
		"distance_km", cacheIntPart(params.DistanceKm),
		"interests", cacheStringSlicePart(params.Interests),
		"lat", cacheFloatPart(params.Lat),
		"lng", cacheFloatPart(params.Lng),
		"ranked_limit", strconv.Itoa(datingRankedWindowMax),
		"source_limit", strconv.Itoa(datingSourceWindowMax),
	)

	var ranked []datingCandidate
	if err := s.cache.ReadThrough(ctx, key, datingDiscoverRankedTTL, &ranked, func(ctx context.Context, dest any) error {
		rankParams := params
		rankParams.CursorOffset = 0
		rankParams.Cursor = ""
		rankParams.RankedWindowLimit = datingRankedWindowMax
		rankParams.SourceWindowLimit = datingSourceWindowMax

		loaded, err := store.loadDatingRankedWindow(ctx, rankParams, time.Now().UTC())
		if err != nil {
			return err
		}
		*dest.(*[]datingCandidate) = loaded
		return nil
	}); err != nil {
		return nil, err
	}

	return store.discoverProfilesFromRankedCandidates(ctx, params, ranked)
}

func (s *cachedStore) CountDiscover(ctx context.Context, params DiscoverParams) (int, error) {
	return s.inner.CountDiscover(ctx, params)
}

func (s *cachedStore) ListLikes(ctx context.Context, userID uuid.UUID, before *string, limit int) ([]DatingLike, error) {
	return s.inner.ListLikes(ctx, userID, before, limit)
}

func (s *cachedStore) CountLikes(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.inner.CountLikes(ctx, userID)
}

func (s *cachedStore) RecordAction(ctx context.Context, actorID, targetID uuid.UUID, action string) (*ActionResult, error) {
	result, err := s.inner.RecordAction(ctx, actorID, targetID, action)
	if err != nil {
		return nil, err
	}
	_ = s.cache.BumpVersions(ctx,
		s.viewerVersionKey(actorID),
		s.viewerVersionKey(targetID),
		s.globalVersionKey(),
	)
	return result, nil
}

func (s *cachedStore) ListMatches(ctx context.Context, userID uuid.UUID, before *string, limit int) ([]DatingMatch, error) {
	return s.inner.ListMatches(ctx, userID, before, limit)
}

func (s *cachedStore) CountUnseenMatches(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.inner.CountUnseenMatches(ctx, userID)
}

func (s *cachedStore) MarkMatchesSeen(ctx context.Context, userID uuid.UUID) (time.Time, error) {
	return s.inner.MarkMatchesSeen(ctx, userID)
}

func (s *cachedStore) GetMatch(ctx context.Context, userID, matchID uuid.UUID) (*DatingMatch, error) {
	return s.inner.GetMatch(ctx, userID, matchID)
}

func (s *cachedStore) Unmatch(ctx context.Context, userID, matchID uuid.UUID) (*DatingMatch, error) {
	match, err := s.inner.Unmatch(ctx, userID, matchID)
	if err != nil {
		return nil, err
	}
	_ = s.cache.BumpVersions(ctx, s.viewerVersionKey(userID), s.globalVersionKey())
	return match, nil
}

func (s *cachedStore) LogEvents(ctx context.Context, userID uuid.UUID, events []DatingEventInput) error {
	return s.inner.LogEvents(ctx, userID, events)
}

func (s *cachedStore) viewerVersionKey(userID uuid.UUID) string {
	return s.cache.Key("ver", "dating", "viewer", userID.String())
}

func (s *cachedStore) globalVersionKey() string {
	return s.cache.Key("ver", "dating", "global")
}

func cacheStringPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	return url.QueryEscape(value)
}

func cacheIntPart(value *int) string {
	if value == nil {
		return "_"
	}
	return strconv.Itoa(*value)
}

func cacheFloatPart(value *float64) string {
	if value == nil {
		return "_"
	}
	return strconv.FormatFloat(*value, 'f', 5, 64)
}

func cacheStringSlicePart(values []string) string {
	if len(values) == 0 {
		return "_"
	}
	copied := append([]string(nil), values...)
	sort.Strings(copied)
	for index := range copied {
		copied[index] = cacheStringPart(copied[index])
	}
	return strings.Join(copied, ",")
}
