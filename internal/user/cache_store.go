package user

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
	profileTTL         = 5 * time.Minute
	discoverRankedTTL  = 10 * time.Minute
	discoverSearchTTL  = 3 * time.Minute
	discoverCountTTL   = 45 * time.Second
	interestsTTL       = 24 * time.Hour
	discoverGlobalPart = "discover_global"
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

func (s *cachedStore) GetUser(ctx context.Context, viewerID, userID uuid.UUID) (*User, error) {
	version, err := s.cache.GetVersion(ctx, s.userVersionKey(userID))
	if err != nil {
		return s.inner.GetUser(ctx, viewerID, userID)
	}

	key := s.cache.Key(
		"user",
		"profile",
		"v", strconv.FormatInt(version, 10),
		"viewer", viewerID.String(),
		"target", userID.String(),
	)

	var user User
	if err := s.cache.ReadThrough(ctx, key, profileTTL, &user, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.GetUser(ctx, viewerID, userID)
		if err != nil {
			return err
		}
		*dest.(*User) = *loaded
		return nil
	}); err != nil {
		return nil, err
	}

	return &user, nil
}

func (s *cachedStore) UsernameExistsForOthers(ctx context.Context, username string, userID uuid.UUID) (bool, error) {
	return s.inner.UsernameExistsForOthers(ctx, username, userID)
}

func (s *cachedStore) UpdateUser(ctx context.Context, userID uuid.UUID, username, city, country, gender, bio *string, soberSince *time.Time, replaceSoberSince bool, birthDate *time.Time, replaceBirthDate bool, interests []string, replaceInterests bool, lat, lng *float64) error {
	if err := s.inner.UpdateUser(ctx, userID, username, city, country, gender, bio, soberSince, replaceSoberSince, birthDate, replaceBirthDate, interests, replaceInterests, lat, lng); err != nil {
		return err
	}

	return s.cache.BumpVersions(ctx,
		s.userVersionKey(userID),
		s.discoverViewerVersionKey(userID),
		s.discoverGlobalVersionKey(),
	)
}

func (s *cachedStore) UpdateCurrentLocation(ctx context.Context, userID uuid.UUID, lat, lng float64, city string) error {
	if err := s.inner.UpdateCurrentLocation(ctx, userID, lat, lng, city); err != nil {
		return err
	}

	return s.cache.BumpVersions(ctx,
		s.userVersionKey(userID),
		s.discoverViewerVersionKey(userID),
		s.discoverGlobalVersionKey(),
	)
}

func (s *cachedStore) UpdateAvatarURL(ctx context.Context, userID uuid.UUID, avatarURL string) error {
	if err := s.inner.UpdateAvatarURL(ctx, userID, avatarURL); err != nil {
		return err
	}

	return s.cache.BumpVersions(ctx,
		s.userVersionKey(userID),
		s.discoverGlobalVersionKey(),
	)
}

func (s *cachedStore) UpdateBannerURL(ctx context.Context, userID uuid.UUID, bannerURL string) error {
	if err := s.inner.UpdateBannerURL(ctx, userID, bannerURL); err != nil {
		return err
	}

	return s.cache.BumpVersions(ctx, s.userVersionKey(userID))
}

func (s *cachedStore) DiscoverUsers(ctx context.Context, params DiscoverUsersParams) ([]User, error) {
	viewerVersion, err := s.cache.GetVersion(ctx, s.discoverViewerVersionKey(params.CurrentUserID))
	if err != nil {
		return s.inner.DiscoverUsers(ctx, params)
	}
	globalVersion, err := s.cache.GetVersion(ctx, s.discoverGlobalVersionKey())
	if err != nil {
		return s.inner.DiscoverUsers(ctx, params)
	}

	ttl := discoverRankedTTL
	if params.Query != "" {
		ttl = discoverSearchTTL
	}

	key := s.cache.Key(
		"user",
		"discover",
		"pipeline", s.discoverPipelinePart(),
		"viewer_v", strconv.FormatInt(viewerVersion, 10),
		"global_v", strconv.FormatInt(globalVersion, 10),
		"viewer", params.CurrentUserID.String(),
		"city", encodePart(params.City),
		"query", encodePart(params.Query),
		"gender", encodePart(params.Gender),
		"sobriety", encodePart(params.Sobriety),
		"age_min", intPart(params.AgeMin),
		"age_max", intPart(params.AgeMax),
		"distance_km", intPart(params.DistanceKm),
		"interests", stringSlicePart(params.Interests),
		"lat", floatPart(params.Lat),
		"lng", floatPart(params.Lng),
		"display_limit", strconv.Itoa(params.DisplayLimit),
		"limit", strconv.Itoa(params.Limit),
		"offset", strconv.Itoa(params.Offset),
	)

	var users []User
	if err := s.cache.ReadThrough(ctx, key, ttl, &users, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.DiscoverUsers(ctx, params)
		if err != nil {
			return err
		}
		*dest.(*[]User) = loaded
		return nil
	}); err != nil {
		return nil, err
	}

	return users, nil
}

func (s *cachedStore) CountDiscoverUsers(ctx context.Context, params DiscoverUsersParams) (int, error) {
	viewerVersion, err := s.cache.GetVersion(ctx, s.discoverViewerVersionKey(params.CurrentUserID))
	if err != nil {
		return s.inner.CountDiscoverUsers(ctx, params)
	}
	globalVersion, err := s.cache.GetVersion(ctx, s.discoverGlobalVersionKey())
	if err != nil {
		return s.inner.CountDiscoverUsers(ctx, params)
	}

	key := s.cache.Key(
		"user",
		"discover_count",
		"pipeline", s.discoverPipelinePart(),
		"viewer_v", strconv.FormatInt(viewerVersion, 10),
		"global_v", strconv.FormatInt(globalVersion, 10),
		"viewer", params.CurrentUserID.String(),
		"city", encodePart(params.City),
		"query", encodePart(params.Query),
		"gender", encodePart(params.Gender),
		"sobriety", encodePart(params.Sobriety),
		"age_min", intPart(params.AgeMin),
		"age_max", intPart(params.AgeMax),
		"distance_km", intPart(params.DistanceKm),
		"interests", stringSlicePart(params.Interests),
		"lat", floatPart(params.Lat),
		"lng", floatPart(params.Lng),
	)

	var count int
	if err := s.cache.ReadThrough(ctx, key, discoverCountTTL, &count, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.CountDiscoverUsers(ctx, params)
		if err != nil {
			return err
		}
		*dest.(*int) = loaded
		return nil
	}); err != nil {
		return 0, err
	}

	return count, nil
}

func (s *cachedStore) ListInterests(ctx context.Context) ([]string, error) {
	key := s.cache.Key("user", "interests")

	var interests []string
	if err := s.cache.ReadThrough(ctx, key, interestsTTL, &interests, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListInterests(ctx)
		if err != nil {
			return err
		}
		*dest.(*[]string) = loaded
		return nil
	}); err != nil {
		return nil, err
	}

	return interests, nil
}

func (s *cachedStore) userVersionKey(userID uuid.UUID) string {
	return s.cache.Key("ver", "user", userID.String())
}

func (s *cachedStore) discoverViewerVersionKey(userID uuid.UUID) string {
	return s.cache.Key("ver", "discover", "viewer", userID.String())
}

func (s *cachedStore) discoverGlobalVersionKey() string {
	return s.cache.Key("ver", "discover", discoverGlobalPart)
}

func (s *cachedStore) discoverPipelinePart() string {
	store, ok := s.inner.(*pgStore)
	if !ok {
		return "legacy"
	}
	if store.discoverPipelineV2 {
		return "v2"
	}
	return "legacy"
}

func encodePart(value string) string {
	if value == "" {
		return "none"
	}
	return url.QueryEscape(value)
}

func floatPart(value *float64) string {
	if value == nil {
		return "none"
	}
	return strconv.FormatFloat(*value, 'f', 6, 64)
}

func intPart(value *int) string {
	if value == nil {
		return "none"
	}
	return strconv.Itoa(*value)
}

func stringSlicePart(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	escaped := make([]string, 0, len(values))
	for _, value := range values {
		escaped = append(escaped, url.QueryEscape(value))
	}
	return strings.Join(escaped, ",")
}
