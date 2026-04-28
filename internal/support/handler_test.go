package support

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/middleware"
)

type mockQuerier struct {
	countOpenSupportRequests      func(ctx context.Context, userID uuid.UUID) (int, error)
	createImmediateSupportRequest func(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string) (*SupportRequest, error)
	createCommunitySupportRequest func(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string) (*SupportRequest, error)
	acceptSupportResponse         func(ctx context.Context, requesterID, requestID, responseID uuid.UUID) (*SupportRequest, error)
	getSupportRequest             func(ctx context.Context, viewerID, requestID uuid.UUID) (*SupportRequest, error)
	closeSupportRequest           func(ctx context.Context, requestID, userID uuid.UUID) ([]uuid.UUID, error)
	listMySupportRequests         func(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error)
	listVisibleSupportRequests    func(ctx context.Context, userID uuid.UUID, channel SupportChannel, cursor *SupportQueueCursor, limit int) ([]SupportRequest, error)
	getSupportRequestState        func(ctx context.Context, requestID uuid.UUID) (uuid.UUID, string, error)
	createSupportResponse         func(ctx context.Context, requestID, userID uuid.UUID, responseType string, message *string, scheduledFor *time.Time) (*CreateSupportResponseResult, error)
	getSupportRequestOwner        func(ctx context.Context, requestID uuid.UUID) (uuid.UUID, error)
	listSupportResponses          func(ctx context.Context, requestID uuid.UUID, limit, offset int) ([]SupportResponse, error)
}

func (m *mockQuerier) CountOpenSupportRequests(ctx context.Context, userID uuid.UUID) (int, error) {
	if m.countOpenSupportRequests != nil {
		return m.countOpenSupportRequests(ctx, userID)
	}
	return 0, nil
}
func (m *mockQuerier) CreateImmediateSupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string) (*SupportRequest, error) {
	if m.createImmediateSupportRequest != nil {
		return m.createImmediateSupportRequest(ctx, userID, reqType, message, urgency, privacyLevel)
	}
	return &SupportRequest{ID: uuid.New(), RequesterID: userID, Channel: SupportChannelImmediate}, nil
}
func (m *mockQuerier) CreateCommunitySupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string) (*SupportRequest, error) {
	if m.createCommunitySupportRequest != nil {
		return m.createCommunitySupportRequest(ctx, userID, reqType, message, urgency, privacyLevel)
	}
	return &SupportRequest{ID: uuid.New(), RequesterID: userID, Channel: SupportChannelCommunity}, nil
}
func (m *mockQuerier) AcceptSupportResponse(ctx context.Context, requesterID, requestID, responseID uuid.UUID) (*SupportRequest, error) {
	if m.acceptSupportResponse != nil {
		return m.acceptSupportResponse(ctx, requesterID, requestID, responseID)
	}
	return &SupportRequest{ID: requestID, RequesterID: requesterID, AcceptedResponseID: &responseID}, nil
}
func (m *mockQuerier) GetSupportRequest(ctx context.Context, viewerID, requestID uuid.UUID) (*SupportRequest, error) {
	if m.getSupportRequest != nil {
		return m.getSupportRequest(ctx, viewerID, requestID)
	}
	return &SupportRequest{ID: requestID}, nil
}
func (m *mockQuerier) CloseSupportRequest(ctx context.Context, requestID, userID uuid.UUID) ([]uuid.UUID, error) {
	if m.closeSupportRequest != nil {
		return m.closeSupportRequest(ctx, requestID, userID)
	}
	return nil, nil
}
func (m *mockQuerier) ListMySupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error) {
	if m.listMySupportRequests != nil {
		return m.listMySupportRequests(ctx, userID, before, limit)
	}
	return nil, nil
}
func (m *mockQuerier) ListVisibleSupportRequests(ctx context.Context, userID uuid.UUID, channel SupportChannel, cursor *SupportQueueCursor, limit int) ([]SupportRequest, error) {
	if m.listVisibleSupportRequests != nil {
		return m.listVisibleSupportRequests(ctx, userID, channel, cursor, limit)
	}
	return nil, nil
}
func (m *mockQuerier) GetSupportRequestState(ctx context.Context, requestID uuid.UUID) (uuid.UUID, string, error) {
	if m.getSupportRequestState != nil {
		return m.getSupportRequestState(ctx, requestID)
	}
	return uuid.Nil, "", ErrNotFound
}
func (m *mockQuerier) CreateSupportResponse(ctx context.Context, requestID, userID uuid.UUID, responseType string, message *string, scheduledFor *time.Time) (*CreateSupportResponseResult, error) {
	if m.createSupportResponse != nil {
		return m.createSupportResponse(ctx, requestID, userID, responseType, message, scheduledFor)
	}
	return &CreateSupportResponseResult{Response: &SupportResponse{ID: uuid.New()}}, nil
}
func (m *mockQuerier) GetSupportRequestOwner(ctx context.Context, requestID uuid.UUID) (uuid.UUID, error) {
	if m.getSupportRequestOwner != nil {
		return m.getSupportRequestOwner(ctx, requestID)
	}
	return uuid.Nil, ErrNotFound
}
func (m *mockQuerier) ListSupportResponses(ctx context.Context, requestID uuid.UUID, limit, offset int) ([]SupportResponse, error) {
	if m.listSupportResponses != nil {
		return m.listSupportResponses(ctx, requestID, limit, offset)
	}
	return nil, nil
}

var (
	fixedUser    = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	fixedRequest = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	fixedOther   = uuid.MustParse("00000000-0000-0000-0000-000000000003")
)

func withUserID(r *http.Request, id uuid.UUID) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.UserIDKey, id))
}

func TestListSupportRequestsRejectsInvalidChannel(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	req := authedRequest(http.MethodGet, "")
	req.URL.RawQuery = "channel=bad"

	h.ListSupportRequests(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListSupportRequestsPassesChannelAndCursor(t *testing.T) {
	var seenChannel SupportChannel
	var seenCursor *SupportQueueCursor

	h := NewHandler(&mockQuerier{
		listVisibleSupportRequests: func(_ context.Context, _ uuid.UUID, channel SupportChannel, cursor *SupportQueueCursor, limit int) ([]SupportRequest, error) {
			seenChannel = channel
			seenCursor = cursor
			return []SupportRequest{}, nil
		},
	})

	encodedCursor, err := encodeSupportQueueCursor(SupportQueueCursor{
		AttentionBucket: 1,
		UrgencyRank:     2,
		CreatedAt:       time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC),
		ID:              fixedRequest,
	})
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}

	rec := httptest.NewRecorder()
	req := authedRequest(http.MethodGet, "")
	req.URL.RawQuery = "channel=immediate&cursor=" + *encodedCursor

	h.ListSupportRequests(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if seenChannel != SupportChannelImmediate {
		t.Fatalf("channel = %q, want %q", seenChannel, SupportChannelImmediate)
	}
	if seenCursor == nil || seenCursor.ID != fixedRequest || seenCursor.AttentionBucket != 1 || seenCursor.UrgencyRank != 2 {
		t.Fatalf("cursor = %#v, want populated decoded cursor", seenCursor)
	}
}

func TestParseSupportQueueCursorRejectsInvalidBase64(t *testing.T) {
	if _, err := parseSupportQueueCursor("not-valid-%%%"); err == nil {
		t.Fatal("expected cursor parse error")
	}
}

func TestEncodeSupportQueueCursorProducesRoundTripPayload(t *testing.T) {
	cursor := SupportQueueCursor{
		AttentionBucket: 2,
		UrgencyRank:     1,
		CreatedAt:       time.Date(2026, 4, 28, 11, 0, 0, 0, time.UTC),
		ID:              fixedOther,
	}

	encoded, err := encodeSupportQueueCursor(cursor)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}

	payload, err := base64.RawURLEncoding.DecodeString(*encoded)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}

	var decoded SupportQueueCursor
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode json: %v", err)
	}

	if decoded != cursor {
		t.Fatalf("decoded cursor = %#v, want %#v", decoded, cursor)
	}
}

func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func authedRequest(method, body string) *http.Request {
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	return withUserID(req, fixedUser)
}

func authedRequestWithID(method, body, id string) *http.Request {
	req := authedRequest(method, body)
	return withURLParam(req, "id", id)
}

func authedRequestWithParam(method, body, key, value string) *http.Request {
	req := authedRequest(method, body)
	return withURLParam(req, key, value)
}

// ── GetSupportRequest ─────────────────────────────────────────────────────────

func TestGetSupportRequestRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.GetSupportRequest(rec, authedRequestWithID(http.MethodGet, "", "bad"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetSupportRequestNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequest: func(_ context.Context, _, _ uuid.UUID) (*SupportRequest, error) {
			return nil, ErrNotFound
		},
	})
	rec := httptest.NewRecorder()
	h.GetSupportRequest(rec, authedRequestWithID(http.MethodGet, "", fixedRequest.String()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGetSupportRequestSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequest: func(_ context.Context, _, id uuid.UUID) (*SupportRequest, error) {
			return &SupportRequest{ID: id}, nil
		},
	})
	rec := httptest.NewRecorder()
	h.GetSupportRequest(rec, authedRequestWithID(http.MethodGet, "", fixedRequest.String()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// ── UpdateSupportRequest ──────────────────────────────────────────────────────

func TestUpdateSupportRequestRejectsUnsupportedStatus(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.UpdateSupportRequest(rec, authedRequestWithID(http.MethodPatch, `{"status":"open"}`, fixedRequest.String()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdateSupportRequestNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		closeSupportRequest: func(_ context.Context, _, _ uuid.UUID) ([]uuid.UUID, error) { return nil, ErrNotFound },
	})
	rec := httptest.NewRecorder()
	h.UpdateSupportRequest(rec, authedRequestWithID(http.MethodPatch, `{"status":"closed"}`, fixedRequest.String()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestUpdateSupportRequestSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequest: func(_ context.Context, _, id uuid.UUID) (*SupportRequest, error) {
			return &SupportRequest{ID: id, Status: "closed"}, nil
		},
	})
	rec := httptest.NewRecorder()
	h.UpdateSupportRequest(rec, authedRequestWithID(http.MethodPatch, `{"status":"closed"}`, fixedRequest.String()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// ── CreateSupportResponse ─────────────────────────────────────────────────────

func TestCreateSupportResponseValidationFlow(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateSupportResponse(rec, authedRequestWithID(http.MethodPost, `{"response_type":"bad"}`, fixedRequest.String()))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCreateSupportResponseRequestNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestState: func(_ context.Context, _ uuid.UUID) (uuid.UUID, string, error) {
			return uuid.Nil, "", ErrNotFound
		},
	})
	rec := httptest.NewRecorder()
	h.CreateSupportResponse(rec, authedRequestWithID(http.MethodPost, `{"response_type":"can_chat"}`, fixedRequest.String()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCreateSupportResponseCannotRespondToOwnRequest(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestState: func(_ context.Context, _ uuid.UUID) (uuid.UUID, string, error) {
			return fixedUser, "open", nil
		},
	})
	rec := httptest.NewRecorder()
	h.CreateSupportResponse(rec, authedRequestWithID(http.MethodPost, `{"response_type":"can_chat"}`, fixedRequest.String()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateSupportResponseRequestNoLongerOpen(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestState: func(_ context.Context, _ uuid.UUID) (uuid.UUID, string, error) {
			return fixedOther, "closed", nil
		},
	})
	rec := httptest.NewRecorder()
	h.CreateSupportResponse(rec, authedRequestWithID(http.MethodPost, `{"response_type":"can_chat"}`, fixedRequest.String()))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestCreateSupportResponseSuccessWhenRequesterIsOtherUser(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestState: func(_ context.Context, _ uuid.UUID) (uuid.UUID, string, error) {
			return fixedOther, "open", nil
		},
	})
	rec := httptest.NewRecorder()
	h.CreateSupportResponse(rec, authedRequestWithID(http.MethodPost, `{"response_type":"can_chat"}`, fixedRequest.String()))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestCreateSupportResponseSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestState: func(_ context.Context, _ uuid.UUID) (uuid.UUID, string, error) {
			return fixedOther, "open", nil
		},
	})
	rec := httptest.NewRecorder()
	h.CreateSupportResponse(rec, authedRequestWithID(http.MethodPost, `{"response_type":"can_chat"}`, fixedRequest.String()))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

// ── ListSupportResponses ──────────────────────────────────────────────────────

func TestListSupportResponsesRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.ListSupportResponses(rec, authedRequestWithID(http.MethodGet, "", "bad"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListSupportResponsesRequestNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestOwner: func(_ context.Context, _ uuid.UUID) (uuid.UUID, error) {
			return uuid.Nil, ErrNotFound
		},
	})
	rec := httptest.NewRecorder()
	h.ListSupportResponses(rec, authedRequestWithID(http.MethodGet, "", fixedRequest.String()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestListSupportResponsesForbiddenForNonOwner(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestOwner: func(_ context.Context, _ uuid.UUID) (uuid.UUID, error) {
			return fixedOther, nil
		},
	})
	rec := httptest.NewRecorder()
	h.ListSupportResponses(rec, authedRequestWithID(http.MethodGet, "", fixedRequest.String()))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestListSupportResponsesSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestOwner: func(_ context.Context, _ uuid.UUID) (uuid.UUID, error) {
			return fixedUser, nil
		},
	})
	rec := httptest.NewRecorder()
	h.ListSupportResponses(rec, authedRequestWithID(http.MethodGet, "", fixedRequest.String()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
