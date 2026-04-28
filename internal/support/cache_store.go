package support

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	appcache "github.com/project_radeon/api/pkg/cache"
)

const (
	supportListTTL    = 30 * time.Second
	supportRequestTTL = 30 * time.Second
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

func (s *cachedStore) CountOpenSupportRequests(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.inner.CountOpenSupportRequests(ctx, userID)
}

func (s *cachedStore) CreateImmediateSupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string) (*SupportRequest, error) {
	request, err := s.inner.CreateImmediateSupportRequest(ctx, userID, reqType, message, urgency, privacyLevel)
	if err != nil {
		return nil, err
	}

	_ = s.cache.BumpVersions(ctx,
		s.myRequestsVersionKey(userID),
		s.visibleVersionKey(),
		s.requestVersionKey(request.ID),
	)
	return request, nil
}

func (s *cachedStore) CreateCommunitySupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string) (*SupportRequest, error) {
	request, err := s.inner.CreateCommunitySupportRequest(ctx, userID, reqType, message, urgency, privacyLevel)
	if err != nil {
		return nil, err
	}

	_ = s.cache.BumpVersions(ctx,
		s.myRequestsVersionKey(userID),
		s.visibleVersionKey(),
		s.requestVersionKey(request.ID),
	)
	return request, nil
}

func (s *cachedStore) AcceptSupportResponse(ctx context.Context, requesterID, requestID, responseID uuid.UUID) (*SupportRequest, error) {
	request, err := s.inner.AcceptSupportResponse(ctx, requesterID, requestID, responseID)
	if err != nil {
		return nil, err
	}

	keys := []string{
		s.visibleVersionKey(),
		s.requestVersionKey(requestID),
		s.myRequestsVersionKey(requesterID),
		s.viewerVisibleVersionKey(requesterID),
	}
	if request.AcceptedResponderID != nil {
		keys = append(keys,
			s.viewerVisibleVersionKey(*request.AcceptedResponderID),
			s.myRequestsVersionKey(*request.AcceptedResponderID),
		)
	}
	_ = s.cache.BumpVersions(ctx, keys...)
	return request, nil
}

func (s *cachedStore) GetSupportRequest(ctx context.Context, viewerID, requestID uuid.UUID) (*SupportRequest, error) {
	version, err := s.cache.GetVersion(ctx, s.requestVersionKey(requestID))
	if err != nil {
		return s.inner.GetSupportRequest(ctx, viewerID, requestID)
	}

	key := s.cache.Key(
		"support",
		"request",
		"id", requestID.String(),
		"viewer", viewerID.String(),
		"v", strconv.FormatInt(version, 10),
	)

	var request SupportRequest
	if err := s.cache.ReadThrough(ctx, key, supportRequestTTL, &request, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.GetSupportRequest(ctx, viewerID, requestID)
		if err != nil {
			return err
		}
		*dest.(*SupportRequest) = *loaded
		return nil
	}); err != nil {
		return nil, err
	}

	return &request, nil
}

func (s *cachedStore) CloseSupportRequest(ctx context.Context, requestID, userID uuid.UUID) ([]uuid.UUID, error) {
	request, requestErr := s.inner.GetSupportRequest(ctx, userID, requestID)
	ownerID, ownerErr := s.inner.GetSupportRequestOwner(ctx, requestID)
	chatIDs, err := s.inner.CloseSupportRequest(ctx, requestID, userID)
	if err != nil {
		return nil, err
	}

	keys := []string{
		s.visibleVersionKey(),
		s.requestVersionKey(requestID),
		s.viewerVisibleVersionKey(userID),
	}
	if ownerErr == nil {
		keys = append(keys, s.myRequestsVersionKey(ownerID))
	}
	if requestErr == nil && request.ResponderID != nil {
		keys = append(keys, s.viewerVisibleVersionKey(*request.ResponderID))
	}
	_ = s.cache.BumpVersions(ctx, keys...)
	return chatIDs, nil
}

func (s *cachedStore) ListMySupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error) {
	version, err := s.cache.GetVersion(ctx, s.myRequestsVersionKey(userID))
	if err != nil {
		return s.inner.ListMySupportRequests(ctx, userID, before, limit)
	}

	key := s.cache.Key(
		"support",
		"mine",
		"user", userID.String(),
		"v", strconv.FormatInt(version, 10),
		"before", supportTimePart(before),
		"limit", strconv.Itoa(limit),
	)

	var requests []SupportRequest
	if err := s.cache.ReadThrough(ctx, key, supportListTTL, &requests, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListMySupportRequests(ctx, userID, before, limit)
		if err != nil {
			return err
		}
		*dest.(*[]SupportRequest) = loaded
		return nil
	}); err != nil {
		return nil, err
	}

	return requests, nil
}

func (s *cachedStore) ListVisibleSupportRequests(ctx context.Context, userID uuid.UUID, channel SupportChannel, cursor *SupportQueueCursor, limit int) ([]SupportRequest, error) {
	globalVersion, err := s.cache.GetVersion(ctx, s.visibleVersionKey())
	if err != nil {
		return s.inner.ListVisibleSupportRequests(ctx, userID, channel, cursor, limit)
	}
	viewerVersion, err := s.cache.GetVersion(ctx, s.viewerVisibleVersionKey(userID))
	if err != nil {
		return s.inner.ListVisibleSupportRequests(ctx, userID, channel, cursor, limit)
	}

	key := s.cache.Key(
		"support",
		"visible",
		"global_v", strconv.FormatInt(globalVersion, 10),
		"viewer_v", strconv.FormatInt(viewerVersion, 10),
		"user", userID.String(),
		"channel", string(channel),
		"cursor", supportQueueCursorPart(cursor),
		"limit", strconv.Itoa(limit),
	)

	var requests []SupportRequest
	if err := s.cache.ReadThrough(ctx, key, supportListTTL, &requests, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListVisibleSupportRequests(ctx, userID, channel, cursor, limit)
		if err != nil {
			return err
		}
		*dest.(*[]SupportRequest) = loaded
		return nil
	}); err != nil {
		return nil, err
	}

	return requests, nil
}

func (s *cachedStore) GetSupportRequestState(ctx context.Context, requestID uuid.UUID) (uuid.UUID, string, error) {
	return s.inner.GetSupportRequestState(ctx, requestID)
}

func (s *cachedStore) CreateSupportResponse(ctx context.Context, requestID, userID uuid.UUID, responseType string, message *string, scheduledFor *time.Time) (*CreateSupportResponseResult, error) {
	result, err := s.inner.CreateSupportResponse(ctx, requestID, userID, responseType, message, scheduledFor)
	if err != nil {
		return nil, err
	}

	keys := []string{
		s.visibleVersionKey(),
		s.viewerVisibleVersionKey(userID),
		s.requestVersionKey(requestID),
	}
	if ownerID, ownerErr := s.inner.GetSupportRequestOwner(ctx, requestID); ownerErr == nil {
		keys = append(keys, s.myRequestsVersionKey(ownerID))
	}
	_ = s.cache.BumpVersions(ctx, keys...)
	return result, nil
}

func (s *cachedStore) GetSupportRequestOwner(ctx context.Context, requestID uuid.UUID) (uuid.UUID, error) {
	return s.inner.GetSupportRequestOwner(ctx, requestID)
}

func (s *cachedStore) ListSupportResponses(ctx context.Context, requestID uuid.UUID, limit, offset int) ([]SupportResponse, error) {
	return s.inner.ListSupportResponses(ctx, requestID, limit, offset)
}

func (s *cachedStore) myRequestsVersionKey(userID uuid.UUID) string {
	return s.cache.Key("ver", "support", "mine", userID.String())
}

func (s *cachedStore) visibleVersionKey() string {
	return s.cache.Key("ver", "support", "visible")
}

func (s *cachedStore) viewerVisibleVersionKey(userID uuid.UUID) string {
	return s.cache.Key("ver", "support", "viewer_visible", userID.String())
}

func (s *cachedStore) requestVersionKey(requestID uuid.UUID) string {
	return s.cache.Key("ver", "support", "request", requestID.String())
}

func supportTimePart(value *time.Time) string {
	if value == nil {
		return "none"
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func supportQueueCursorPart(cursor *SupportQueueCursor) string {
	if cursor == nil {
		return "none"
	}

	encoded, err := encodeSupportQueueCursor(*cursor)
	if err != nil || encoded == nil {
		return "invalid"
	}
	return *encoded
}
