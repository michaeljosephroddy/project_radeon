package support

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/project_radeon/api/internal/cachetest"
)

type stubQuerier struct {
	getSupportRequestCalls int
}

func (s *stubQuerier) CountOpenSupportRequests(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}

func (s *stubQuerier) CreateImmediateSupportRequest(context.Context, uuid.UUID, string, *string, string, string) (*SupportRequest, error) {
	return nil, nil
}

func (s *stubQuerier) CreateCommunitySupportRequest(context.Context, uuid.UUID, string, *string, string, string) (*SupportRequest, error) {
	return nil, nil
}

func (s *stubQuerier) AcceptSupportResponse(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*SupportRequest, error) {
	return &SupportRequest{}, nil
}

func (s *stubQuerier) GetSupportRequest(_ context.Context, viewerID, requestID uuid.UUID) (*SupportRequest, error) {
	s.getSupportRequestCalls++
	return &SupportRequest{
		ID:            requestID,
		RequesterID:   viewerID,
		Username:      "support",
		Type:          "need_to_talk",
		Urgency:       "soon",
		Status:        "open",
		CreatedAt:     time.Unix(int64(s.getSupportRequestCalls), 0).UTC(),
		ResponseCount: s.getSupportRequestCalls - 1,
	}, nil
}

func (s *stubQuerier) CloseSupportRequest(context.Context, uuid.UUID, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (s *stubQuerier) ListMySupportRequests(context.Context, uuid.UUID, *time.Time, int) ([]SupportRequest, error) {
	return nil, nil
}

func (s *stubQuerier) ListVisibleSupportRequests(context.Context, uuid.UUID, SupportChannel, *SupportQueueCursor, int) ([]SupportRequest, error) {
	return nil, nil
}

func (s *stubQuerier) GetSupportRequestState(context.Context, uuid.UUID) (uuid.UUID, string, error) {
	return uuid.Nil, "", nil
}

func (s *stubQuerier) CreateSupportResponse(context.Context, uuid.UUID, uuid.UUID, string, *string, *time.Time) (*CreateSupportResponseResult, error) {
	return &CreateSupportResponseResult{Response: &SupportResponse{}}, nil
}

func (s *stubQuerier) GetSupportRequestOwner(context.Context, uuid.UUID) (uuid.UUID, error) {
	return requestOwnerID, nil
}

func (s *stubQuerier) ListSupportResponses(context.Context, uuid.UUID, int, int) ([]SupportResponse, error) {
	return nil, nil
}

var requestOwnerID = uuid.New()

func TestCachedStoreInvalidatesSupportRequestAfterResponse(t *testing.T) {
	t.Parallel()

	inner := &stubQuerier{}
	store := cachetest.NewStore()
	cached := NewCachedStore(inner, store)

	viewerID := uuid.New()
	requestID := uuid.New()

	first, err := cached.GetSupportRequest(context.Background(), viewerID, requestID)
	if err != nil {
		t.Fatalf("first GetSupportRequest: %v", err)
	}
	second, err := cached.GetSupportRequest(context.Background(), viewerID, requestID)
	if err != nil {
		t.Fatalf("second GetSupportRequest: %v", err)
	}
	if inner.getSupportRequestCalls != 1 {
		t.Fatalf("expected one underlying GetSupportRequest call after cache hit, got %d", inner.getSupportRequestCalls)
	}
	if first.ResponseCount != second.ResponseCount {
		t.Fatalf("expected cached support request response count to match")
	}

	if _, err := cached.CreateSupportResponse(context.Background(), requestID, viewerID, "can_chat", nil, nil); err != nil {
		t.Fatalf("CreateSupportResponse: %v", err)
	}

	third, err := cached.GetSupportRequest(context.Background(), viewerID, requestID)
	if err != nil {
		t.Fatalf("third GetSupportRequest: %v", err)
	}
	if inner.getSupportRequestCalls != 2 {
		t.Fatalf("expected invalidation to force a fresh GetSupportRequest call, got %d", inner.getSupportRequestCalls)
	}
	if third.ResponseCount <= second.ResponseCount {
		t.Fatalf("expected invalidated support request to reflect updated response count")
	}
}
