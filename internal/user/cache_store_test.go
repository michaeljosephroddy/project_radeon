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

func (s *stubQuerier) UpdateUser(context.Context, uuid.UUID, *string, *string, *string, *string, *time.Time, bool, []string, bool, *float64, *float64) error {
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

func (s *stubQuerier) DiscoverUsers(_ context.Context, currentUserID uuid.UUID, city, query string, lat, lng *float64, limit, offset int) ([]User, error) {
	s.discoverUsersCalls++
	return []User{{
		ID:               currentUserID,
		Username:         query + city,
		CreatedAt:        time.Unix(int64(s.discoverUsersCalls), 0).UTC(),
		FriendshipStatus: "none",
	}}, nil
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

	if err := cached.UpdateUser(context.Background(), userID, nil, nil, nil, nil, nil, false, nil, false, nil, nil); err != nil {
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
	_, err := cached.DiscoverUsers(context.Background(), viewerID, "%dublin%", "", nil, nil, 20, 0)
	if err != nil {
		t.Fatalf("first DiscoverUsers: %v", err)
	}
	_, err = cached.DiscoverUsers(context.Background(), viewerID, "%dublin%", "", nil, nil, 20, 0)
	if err != nil {
		t.Fatalf("second DiscoverUsers: %v", err)
	}
	if inner.discoverUsersCalls != 1 {
		t.Fatalf("expected one underlying DiscoverUsers call after cache hit, got %d", inner.discoverUsersCalls)
	}
}
