package support

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	appcache "github.com/project_radeon/api/pkg/cache"
)

const (
	supportProfileTTL = 5 * time.Minute
	supportListTTL    = 30 * time.Second
	supportRequestTTL = 30 * time.Second
	supportSummaryTTL = 30 * time.Second
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

func (s *cachedStore) GetSupportProfile(ctx context.Context, userID uuid.UUID) (*SupportProfile, error) {
	version, err := s.cache.GetVersion(ctx, s.profileVersionKey(userID))
	if err != nil {
		return s.inner.GetSupportProfile(ctx, userID)
	}

	key := s.cache.Key("support", "profile", "user", userID.String(), "v", strconv.FormatInt(version, 10))

	var profile SupportProfile
	if err := s.cache.ReadThrough(ctx, key, supportProfileTTL, &profile, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.GetSupportProfile(ctx, userID)
		if err != nil {
			return err
		}
		*dest.(*SupportProfile) = *loaded
		return nil
	}); err != nil {
		return nil, err
	}

	return &profile, nil
}

func (s *cachedStore) UpdateSupportProfile(ctx context.Context, userID uuid.UUID, available bool) (*SupportProfile, error) {
	profile, err := s.inner.UpdateSupportProfile(ctx, userID, available)
	if err != nil {
		return nil, err
	}

	_ = s.cache.BumpVersions(ctx,
		s.profileVersionKey(userID),
		s.summaryVersionKey(),
	)
	return profile, nil
}

func (s *cachedStore) GetSupportHome(ctx context.Context, userID uuid.UUID) (*SupportHomePayload, error) {
	return s.inner.GetSupportHome(ctx, userID)
}

func (s *cachedStore) GetSupportResponderProfile(ctx context.Context, userID uuid.UUID) (*SupportResponderProfile, error) {
	return s.inner.GetSupportResponderProfile(ctx, userID)
}

func (s *cachedStore) UpdateSupportResponderProfile(ctx context.Context, userID uuid.UUID, input UpdateSupportResponderProfileInput) (*SupportResponderProfile, error) {
	profile, err := s.inner.UpdateSupportResponderProfile(ctx, userID, input)
	if err != nil {
		return nil, err
	}

	_ = s.cache.BumpVersions(ctx,
		s.profileVersionKey(userID),
		s.summaryVersionKey(),
		s.viewerVisibleVersionKey(userID),
	)
	return profile, nil
}

func (s *cachedStore) CountOpenSupportRequests(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.inner.CountOpenSupportRequests(ctx, userID)
}

func (s *cachedStore) CreateSupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, priorityVisibility bool, priorityExpiresAt *time.Time) (*SupportRequest, error) {
	request, err := s.inner.CreateSupportRequest(ctx, userID, reqType, message, urgency, priorityVisibility, priorityExpiresAt)
	if err != nil {
		return nil, err
	}

	_ = s.cache.BumpVersions(ctx,
		s.myRequestsVersionKey(userID),
		s.visibleVersionKey(),
		s.summaryVersionKey(),
		s.requestVersionKey(request.ID),
	)
	return request, nil
}

func (s *cachedStore) CreateImmediateSupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string, priorityVisibility bool, priorityExpiresAt *time.Time) (*SupportRequest, error) {
	request, err := s.inner.CreateImmediateSupportRequest(ctx, userID, reqType, message, urgency, privacyLevel, priorityVisibility, priorityExpiresAt)
	if err != nil {
		return nil, err
	}

	_ = s.cache.BumpVersions(ctx,
		s.myRequestsVersionKey(userID),
		s.visibleVersionKey(),
		s.summaryVersionKey(),
		s.requestVersionKey(request.ID),
	)
	return request, nil
}

func (s *cachedStore) CreateCommunitySupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string, priorityVisibility bool, priorityExpiresAt *time.Time) (*SupportRequest, error) {
	request, err := s.inner.CreateCommunitySupportRequest(ctx, userID, reqType, message, urgency, privacyLevel, priorityVisibility, priorityExpiresAt)
	if err != nil {
		return nil, err
	}

	_ = s.cache.BumpVersions(ctx,
		s.myRequestsVersionKey(userID),
		s.visibleVersionKey(),
		s.summaryVersionKey(),
		s.requestVersionKey(request.ID),
	)
	return request, nil
}

func (s *cachedStore) RouteSupportRequest(ctx context.Context, requestID uuid.UUID) error {
	return s.inner.RouteSupportRequest(ctx, requestID)
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

func (s *cachedStore) CloseSupportRequest(ctx context.Context, requestID, userID uuid.UUID) error {
	if err := s.inner.CloseSupportRequest(ctx, requestID, userID); err != nil {
		return err
	}

	_ = s.cache.BumpVersions(ctx,
		s.myRequestsVersionKey(userID),
		s.visibleVersionKey(),
		s.summaryVersionKey(),
		s.requestVersionKey(requestID),
	)
	return nil
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

func (s *cachedStore) ListVisibleSupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error) {
	globalVersion, err := s.cache.GetVersion(ctx, s.visibleVersionKey())
	if err != nil {
		return s.inner.ListVisibleSupportRequests(ctx, userID, before, limit)
	}
	viewerVersion, err := s.cache.GetVersion(ctx, s.viewerVisibleVersionKey(userID))
	if err != nil {
		return s.inner.ListVisibleSupportRequests(ctx, userID, before, limit)
	}

	key := s.cache.Key(
		"support",
		"visible",
		"global_v", strconv.FormatInt(globalVersion, 10),
		"viewer_v", strconv.FormatInt(viewerVersion, 10),
		"user", userID.String(),
		"before", supportTimePart(before),
		"limit", strconv.Itoa(limit),
	)

	var requests []SupportRequest
	if err := s.cache.ReadThrough(ctx, key, supportListTTL, &requests, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListVisibleSupportRequests(ctx, userID, before, limit)
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

func (s *cachedStore) ListRespondedSupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error) {
	globalVersion, err := s.cache.GetVersion(ctx, s.visibleVersionKey())
	if err != nil {
		return s.inner.ListRespondedSupportRequests(ctx, userID, before, limit)
	}
	viewerVersion, err := s.cache.GetVersion(ctx, s.viewerVisibleVersionKey(userID))
	if err != nil {
		return s.inner.ListRespondedSupportRequests(ctx, userID, before, limit)
	}

	key := s.cache.Key(
		"support",
		"responded",
		"global_v", strconv.FormatInt(globalVersion, 10),
		"viewer_v", strconv.FormatInt(viewerVersion, 10),
		"user", userID.String(),
		"before", supportTimePart(before),
		"limit", strconv.Itoa(limit),
	)

	var requests []SupportRequest
	if err := s.cache.ReadThrough(ctx, key, supportListTTL, &requests, func(ctx context.Context, dest any) error {
		loaded, err := s.inner.ListRespondedSupportRequests(ctx, userID, before, limit)
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

func (s *cachedStore) ListResponderQueue(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportOffer, error) {
	return s.inner.ListResponderQueue(ctx, userID, before, limit)
}

func (s *cachedStore) ListSupportSessions(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportSession, error) {
	return s.inner.ListSupportSessions(ctx, userID, before, limit)
}

func (s *cachedStore) CloseSupportSession(ctx context.Context, userID, sessionID uuid.UUID, outcome string) (*SupportSession, error) {
	session, err := s.inner.CloseSupportSession(ctx, userID, sessionID, outcome)
	if err != nil {
		return nil, err
	}

	_ = s.cache.BumpVersions(ctx,
		s.summaryVersionKey(),
		s.viewerVisibleVersionKey(userID),
	)
	return session, nil
}

func (s *cachedStore) SweepExpiredSupportOffers(ctx context.Context) error {
	return s.inner.SweepExpiredSupportOffers(ctx)
}

func (s *cachedStore) AcceptSupportOffer(ctx context.Context, responderID, offerID uuid.UUID) (*SupportSession, error) {
	session, err := s.inner.AcceptSupportOffer(ctx, responderID, offerID)
	if err != nil {
		return nil, err
	}

	_ = s.cache.BumpVersions(ctx,
		s.viewerVisibleVersionKey(responderID),
		s.summaryVersionKey(),
	)
	return session, nil
}

func (s *cachedStore) DeclineSupportOffer(ctx context.Context, responderID, offerID uuid.UUID) error {
	if err := s.inner.DeclineSupportOffer(ctx, responderID, offerID); err != nil {
		return err
	}

	_ = s.cache.BumpVersions(ctx,
		s.viewerVisibleVersionKey(responderID),
		s.summaryVersionKey(),
	)
	return nil
}

func (s *cachedStore) FetchSupportSummary(ctx context.Context, viewerID uuid.UUID) (int, int, error) {
	version, err := s.cache.GetVersion(ctx, s.summaryVersionKey())
	if err != nil {
		return s.inner.FetchSupportSummary(ctx, viewerID)
	}

	key := s.cache.Key(
		"support",
		"summary",
		"viewer", viewerID.String(),
		"v", strconv.FormatInt(version, 10),
	)

	type supportSummary struct {
		OpenCount      int `json:"open_count"`
		AvailableCount int `json:"available_count"`
	}

	var summary supportSummary
	if err := s.cache.ReadThrough(ctx, key, supportSummaryTTL, &summary, func(ctx context.Context, dest any) error {
		openCount, availableCount, err := s.inner.FetchSupportSummary(ctx, viewerID)
		if err != nil {
			return err
		}
		typed := dest.(*supportSummary)
		typed.OpenCount = openCount
		typed.AvailableCount = availableCount
		return nil
	}); err != nil {
		return 0, 0, err
	}

	return summary.OpenCount, summary.AvailableCount, nil
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
	if result.Chat != nil && result.Chat.SupportContext != nil {
		keys = append(keys, s.myRequestsVersionKey(result.Chat.SupportContext.RequesterID))
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

func (s *cachedStore) profileVersionKey(userID uuid.UUID) string {
	return s.cache.Key("ver", "support", "profile", userID.String())
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

func (s *cachedStore) summaryVersionKey() string {
	return s.cache.Key("ver", "support", "summary")
}

func supportTimePart(value *time.Time) string {
	if value == nil {
		return "none"
	}
	return value.UTC().Format(time.RFC3339Nano)
}
