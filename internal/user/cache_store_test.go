package user

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/project_radeon/api/internal/cachetest"
)

type stubQuerier struct {
	getUserCalls       int
	discoverUsersCalls int
	countDiscoverCalls int
	listInterestsCalls int
}

func (s *stubQuerier) GetUser(_ context.Context, viewerID, userID uuid.UUID) (*User, error) {
	s.getUserCalls++
	return &User{
		ID:               userID,
		Username:         "user-" + viewerID.String(),
		CreatedAt:        time.Unix(int64(s.getUserCalls), 0).UTC(),
		FriendshipStatus: "none",
	}, nil
}

func (s *stubQuerier) UsernameExistsForOthers(context.Context, string, uuid.UUID) (bool, error) {
	return false, nil
}

func (s *stubQuerier) UpdateUser(context.Context, uuid.UUID, *string, *string, *string, *string, *string, *time.Time, bool, *time.Time, bool, []string, bool, *float64, *float64) error {
	return nil
}

func (s *stubQuerier) UpdateAvatarURL(context.Context, uuid.UUID, string) error {
	return nil
}

func (s *stubQuerier) UpdateBannerURL(context.Context, uuid.UUID, string) error {
	return nil
}

func (s *stubQuerier) UpdateCurrentLocation(context.Context, uuid.UUID, float64, float64, string) error {
	return nil
}

func (s *stubQuerier) DiscoverUsers(_ context.Context, params DiscoverUsersParams) ([]User, error) {
	s.discoverUsersCalls++
	return []User{{
		ID:               params.CurrentUserID,
		Username:         params.Query + params.City + params.Gender + params.Sobriety,
		CreatedAt:        time.Unix(int64(s.discoverUsersCalls), 0).UTC(),
		FriendshipStatus: "none",
	}}, nil
}

func (s *stubQuerier) CountDiscoverUsers(_ context.Context, params DiscoverUsersParams) (int, error) {
	s.countDiscoverCalls++
	return len(params.Interests) + s.countDiscoverCalls, nil
}

func (s *stubQuerier) ListInterests(context.Context) ([]string, error) {
	s.listInterestsCalls++
	return []string{"fitness", "meetups"}, nil
}

func TestCachedStoreGetUserCachesAndInvalidates(t *testing.T) {
	t.Parallel()

	inner := &stubQuerier{}
	store := cachetest.NewStore()
	cached := NewCachedStore(inner, store)

	viewerID := uuid.New()
	userID := uuid.New()

	first, err := cached.GetUser(context.Background(), viewerID, userID)
	if err != nil {
		t.Fatalf("first GetUser: %v", err)
	}
	second, err := cached.GetUser(context.Background(), viewerID, userID)
	if err != nil {
		t.Fatalf("second GetUser: %v", err)
	}
	if inner.getUserCalls != 1 {
		t.Fatalf("expected one underlying GetUser call after cache hit, got %d", inner.getUserCalls)
	}
	if !first.CreatedAt.Equal(second.CreatedAt) {
		t.Fatalf("expected cached response to preserve CreatedAt, got %v and %v", first.CreatedAt, second.CreatedAt)
	}

	if err := cached.UpdateUser(context.Background(), userID, nil, nil, nil, nil, nil, nil, false, nil, false, nil, false, nil, nil); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	third, err := cached.GetUser(context.Background(), viewerID, userID)
	if err != nil {
		t.Fatalf("third GetUser: %v", err)
	}
	if inner.getUserCalls != 2 {
		t.Fatalf("expected cache invalidation to force a second underlying GetUser call, got %d", inner.getUserCalls)
	}
	if !third.CreatedAt.After(second.CreatedAt) {
		t.Fatalf("expected invalidated read to return fresh data, got %v after %v", third.CreatedAt, second.CreatedAt)
	}
}

func TestCachedStoreDiscoverUsersCaches(t *testing.T) {
	t.Parallel()

	inner := &stubQuerier{}
	store := cachetest.NewStore()
	cached := NewCachedStore(inner, store)

	viewerID := uuid.New()
	baseParams := DiscoverUsersParams{
		CurrentUserID: viewerID,
		City:          "%dublin%",
		Limit:         20,
		Offset:        0,
	}

	_, err := cached.DiscoverUsers(context.Background(), baseParams)
	if err != nil {
		t.Fatalf("first DiscoverUsers: %v", err)
	}
	_, err = cached.DiscoverUsers(context.Background(), baseParams)
	if err != nil {
		t.Fatalf("second DiscoverUsers: %v", err)
	}
	if inner.discoverUsersCalls != 1 {
		t.Fatalf("expected one underlying DiscoverUsers call after cache hit, got %d", inner.discoverUsersCalls)
	}
}

func TestCachedStoreDiscoverUsersKeysByFilters(t *testing.T) {
	t.Parallel()

	inner := &stubQuerier{}
	store := cachetest.NewStore()
	cached := NewCachedStore(inner, store)

	viewerID := uuid.New()
	firstParams := DiscoverUsersParams{
		CurrentUserID: viewerID,
		City:          "Dublin",
		Gender:        "woman",
		Sobriety:      "years_1",
		Interests:     []string{"fitness"},
		Limit:         20,
		Offset:        0,
	}
	secondParams := DiscoverUsersParams{
		CurrentUserID: viewerID,
		City:          "Dublin",
		Gender:        "man",
		Sobriety:      "years_1",
		Interests:     []string{"meetups"},
		Limit:         20,
		Offset:        0,
	}

	if _, err := cached.DiscoverUsers(context.Background(), firstParams); err != nil {
		t.Fatalf("first filtered DiscoverUsers: %v", err)
	}
	if _, err := cached.DiscoverUsers(context.Background(), secondParams); err != nil {
		t.Fatalf("second filtered DiscoverUsers: %v", err)
	}
	if inner.discoverUsersCalls != 2 {
		t.Fatalf("expected distinct filtered queries to bypass each other's cache keys, got %d underlying calls", inner.discoverUsersCalls)
	}
}
