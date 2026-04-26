package friends

import (
	"context"
	"time"

	"github.com/google/uuid"
	appcache "github.com/project_radeon/api/pkg/cache"
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

func (s *cachedStore) GetFriendshipState(ctx context.Context, userAID, userBID uuid.UUID) (bool, string, uuid.UUID, error) {
	return s.inner.GetFriendshipState(ctx, userAID, userBID)
}

func (s *cachedStore) InsertFriendship(ctx context.Context, userAID, userBID, requesterID uuid.UUID) error {
	if err := s.inner.InsertFriendship(ctx, userAID, userBID, requesterID); err != nil {
		return err
	}
	s.bumpUserCacheVersions(ctx, userAID, userBID)
	return nil
}

func (s *cachedStore) AcceptFriendRequest(ctx context.Context, userAID, userBID, userID, otherUserID uuid.UUID) error {
	if err := s.inner.AcceptFriendRequest(ctx, userAID, userBID, userID, otherUserID); err != nil {
		return err
	}
	s.bumpUserCacheVersions(ctx, userAID, userBID)
	return nil
}

func (s *cachedStore) DeletePendingFriendship(ctx context.Context, userAID, userBID, requesterID uuid.UUID) error {
	if err := s.inner.DeletePendingFriendship(ctx, userAID, userBID, requesterID); err != nil {
		return err
	}
	s.bumpUserCacheVersions(ctx, userAID, userBID)
	return nil
}

func (s *cachedStore) RemoveFriend(ctx context.Context, userAID, userBID, userID, otherUserID uuid.UUID) error {
	if err := s.inner.RemoveFriend(ctx, userAID, userBID, userID, otherUserID); err != nil {
		return err
	}
	s.bumpUserCacheVersions(ctx, userAID, userBID)
	return nil
}

func (s *cachedStore) ListFriendUsers(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]friendUser, error) {
	return s.inner.ListFriendUsers(ctx, userID, before, limit)
}

func (s *cachedStore) ListPendingRequests(ctx context.Context, userID uuid.UUID, outgoing bool, before *time.Time, limit int) ([]friendUser, error) {
	return s.inner.ListPendingRequests(ctx, userID, outgoing, before, limit)
}

func (s *cachedStore) bumpUserCacheVersions(ctx context.Context, userIDs ...uuid.UUID) {
	keys := make([]string, 0, len(userIDs)*2)
	for _, userID := range userIDs {
		keys = append(keys,
			s.cache.Key("ver", "user", userID.String()),
			s.cache.Key("ver", "discover", "viewer", userID.String()),
		)
	}
	_ = s.cache.BumpVersions(ctx, keys...)
}
