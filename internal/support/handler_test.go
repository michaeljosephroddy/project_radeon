package support

import (
	"context"
	"errors"
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
	getSupportProfile             func(ctx context.Context, userID uuid.UUID) (*SupportProfile, error)
	updateSupportProfile          func(ctx context.Context, userID uuid.UUID, available bool) (*SupportProfile, error)
	getSupportHome                func(ctx context.Context, userID uuid.UUID) (*SupportHomePayload, error)
	getSupportResponderProfile    func(ctx context.Context, userID uuid.UUID) (*SupportResponderProfile, error)
	updateSupportResponderProfile func(ctx context.Context, userID uuid.UUID, input UpdateSupportResponderProfileInput) (*SupportResponderProfile, error)
	countOpenSupportRequests      func(ctx context.Context, userID uuid.UUID) (int, error)
	createSupportRequest          func(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, priorityVisibility bool, priorityExpiresAt *time.Time) (*SupportRequest, error)
	createImmediateSupportRequest func(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string, priorityVisibility bool, priorityExpiresAt *time.Time) (*SupportRequest, error)
	createCommunitySupportRequest func(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string, priorityVisibility bool, priorityExpiresAt *time.Time) (*SupportRequest, error)
	routeSupportRequest           func(ctx context.Context, requestID uuid.UUID) error
	acceptSupportOffer            func(ctx context.Context, responderID, offerID uuid.UUID) (*SupportSession, error)
	declineSupportOffer           func(ctx context.Context, responderID, offerID uuid.UUID) error
	getSupportRequest             func(ctx context.Context, viewerID, requestID uuid.UUID) (*SupportRequest, error)
	closeSupportRequest           func(ctx context.Context, requestID, userID uuid.UUID) error
	listMySupportRequests         func(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error)
	listVisibleSupportRequests    func(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error)
	listRespondedSupportRequests  func(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error)
	listResponderQueue            func(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportOffer, error)
	listSupportSessions           func(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportSession, error)
	closeSupportSession           func(ctx context.Context, userID, sessionID uuid.UUID, outcome string) (*SupportSession, error)
	sweepExpiredSupportOffers     func(ctx context.Context) error
	fetchSupportSummary           func(ctx context.Context, viewerID uuid.UUID) (int, int, error)
	getSupportRequestState        func(ctx context.Context, requestID uuid.UUID) (uuid.UUID, string, error)
	createSupportResponse         func(ctx context.Context, requestID, userID uuid.UUID, responseType string, message *string, scheduledFor *time.Time) (*CreateSupportResponseResult, error)
	getSupportRequestOwner        func(ctx context.Context, requestID uuid.UUID) (uuid.UUID, error)
	listSupportResponses          func(ctx context.Context, requestID uuid.UUID, limit, offset int) ([]SupportResponse, error)
}

func (m *mockQuerier) GetSupportProfile(ctx context.Context, userID uuid.UUID) (*SupportProfile, error) {
	if m.getSupportProfile != nil {
		return m.getSupportProfile(ctx, userID)
	}
	return &SupportProfile{IsAvailableToSupport: true}, nil
}
func (m *mockQuerier) UpdateSupportProfile(ctx context.Context, userID uuid.UUID, available bool) (*SupportProfile, error) {
	if m.updateSupportProfile != nil {
		return m.updateSupportProfile(ctx, userID, available)
	}
	return &SupportProfile{IsAvailableToSupport: available}, nil
}
func (m *mockQuerier) GetSupportHome(ctx context.Context, userID uuid.UUID) (*SupportHomePayload, error) {
	if m.getSupportHome != nil {
		return m.getSupportHome(ctx, userID)
	}
	return &SupportHomePayload{}, nil
}
func (m *mockQuerier) GetSupportResponderProfile(ctx context.Context, userID uuid.UUID) (*SupportResponderProfile, error) {
	if m.getSupportResponderProfile != nil {
		return m.getSupportResponderProfile(ctx, userID)
	}
	return &SupportResponderProfile{UserID: userID}, nil
}
func (m *mockQuerier) UpdateSupportResponderProfile(ctx context.Context, userID uuid.UUID, input UpdateSupportResponderProfileInput) (*SupportResponderProfile, error) {
	if m.updateSupportResponderProfile != nil {
		return m.updateSupportResponderProfile(ctx, userID, input)
	}
	return &SupportResponderProfile{UserID: userID, MaxConcurrentSessions: input.MaxConcurrentSessions}, nil
}
func (m *mockQuerier) CountOpenSupportRequests(ctx context.Context, userID uuid.UUID) (int, error) {
	if m.countOpenSupportRequests != nil {
		return m.countOpenSupportRequests(ctx, userID)
	}
	return 0, nil
}
func (m *mockQuerier) CreateSupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, priorityVisibility bool, priorityExpiresAt *time.Time) (*SupportRequest, error) {
	if m.createSupportRequest != nil {
		return m.createSupportRequest(ctx, userID, reqType, message, urgency, priorityVisibility, priorityExpiresAt)
	}
	return &SupportRequest{ID: uuid.New(), RequesterID: userID}, nil
}
func (m *mockQuerier) CreateImmediateSupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string, priorityVisibility bool, priorityExpiresAt *time.Time) (*SupportRequest, error) {
	if m.createImmediateSupportRequest != nil {
		return m.createImmediateSupportRequest(ctx, userID, reqType, message, urgency, privacyLevel, priorityVisibility, priorityExpiresAt)
	}
	return &SupportRequest{ID: uuid.New(), RequesterID: userID, Channel: SupportChannelImmediate}, nil
}
func (m *mockQuerier) CreateCommunitySupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, urgency string, privacyLevel string, priorityVisibility bool, priorityExpiresAt *time.Time) (*SupportRequest, error) {
	if m.createCommunitySupportRequest != nil {
		return m.createCommunitySupportRequest(ctx, userID, reqType, message, urgency, privacyLevel, priorityVisibility, priorityExpiresAt)
	}
	return &SupportRequest{ID: uuid.New(), RequesterID: userID, Channel: SupportChannelCommunity}, nil
}
func (m *mockQuerier) RouteSupportRequest(ctx context.Context, requestID uuid.UUID) error {
	if m.routeSupportRequest != nil {
		return m.routeSupportRequest(ctx, requestID)
	}
	return nil
}
func (m *mockQuerier) AcceptSupportOffer(ctx context.Context, responderID, offerID uuid.UUID) (*SupportSession, error) {
	if m.acceptSupportOffer != nil {
		return m.acceptSupportOffer(ctx, responderID, offerID)
	}
	return &SupportSession{ID: offerID, ResponderID: responderID}, nil
}
func (m *mockQuerier) DeclineSupportOffer(ctx context.Context, responderID, offerID uuid.UUID) error {
	if m.declineSupportOffer != nil {
		return m.declineSupportOffer(ctx, responderID, offerID)
	}
	return nil
}
func (m *mockQuerier) GetSupportRequest(ctx context.Context, viewerID, requestID uuid.UUID) (*SupportRequest, error) {
	if m.getSupportRequest != nil {
		return m.getSupportRequest(ctx, viewerID, requestID)
	}
	return &SupportRequest{ID: requestID}, nil
}
func (m *mockQuerier) CloseSupportRequest(ctx context.Context, requestID, userID uuid.UUID) error {
	if m.closeSupportRequest != nil {
		return m.closeSupportRequest(ctx, requestID, userID)
	}
	return nil
}
func (m *mockQuerier) ListMySupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error) {
	if m.listMySupportRequests != nil {
		return m.listMySupportRequests(ctx, userID, before, limit)
	}
	return nil, nil
}
func (m *mockQuerier) ListVisibleSupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error) {
	if m.listVisibleSupportRequests != nil {
		return m.listVisibleSupportRequests(ctx, userID, before, limit)
	}
	return nil, nil
}
func (m *mockQuerier) ListRespondedSupportRequests(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error) {
	if m.listRespondedSupportRequests != nil {
		return m.listRespondedSupportRequests(ctx, userID, before, limit)
	}
	return nil, nil
}
func (m *mockQuerier) ListResponderQueue(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportOffer, error) {
	if m.listResponderQueue != nil {
		return m.listResponderQueue(ctx, userID, before, limit)
	}
	return nil, nil
}
func (m *mockQuerier) ListSupportSessions(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportSession, error) {
	if m.listSupportSessions != nil {
		return m.listSupportSessions(ctx, userID, before, limit)
	}
	return nil, nil
}
func (m *mockQuerier) CloseSupportSession(ctx context.Context, userID, sessionID uuid.UUID, outcome string) (*SupportSession, error) {
	if m.closeSupportSession != nil {
		return m.closeSupportSession(ctx, userID, sessionID, outcome)
	}
	return &SupportSession{ID: sessionID, Status: SupportSessionCompleted}, nil
}
func (m *mockQuerier) SweepExpiredSupportOffers(ctx context.Context) error {
	if m.sweepExpiredSupportOffers != nil {
		return m.sweepExpiredSupportOffers(ctx)
	}
	return nil
}
func (m *mockQuerier) FetchSupportSummary(ctx context.Context, viewerID uuid.UUID) (int, int, error) {
	if m.fetchSupportSummary != nil {
		return m.fetchSupportSummary(ctx, viewerID)
	}
	return 0, 0, nil
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

// ── GetMySupportProfile ───────────────────────────────────────────────────────

func TestGetMySupportProfileSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.GetMySupportProfile(rec, withUserID(httptest.NewRequest(http.MethodGet, "/", nil), fixedUser))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGetMySupportProfileDBError(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportProfile: func(_ context.Context, _ uuid.UUID) (*SupportProfile, error) {
			return nil, errors.New("db error")
		},
	})
	rec := httptest.NewRecorder()
	h.GetMySupportProfile(rec, withUserID(httptest.NewRequest(http.MethodGet, "/", nil), fixedUser))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// ── UpdateMySupportProfile ────────────────────────────────────────────────────

func TestUpdateMySupportProfileInvalidBody(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.UpdateMySupportProfile(rec, withUserID(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader("{")), fixedUser))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdateMySupportProfileSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.UpdateMySupportProfile(rec, authedRequest(http.MethodPatch, `{"is_available_to_support":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// ── CreateSupportRequest ──────────────────────────────────────────────────────

func TestCreateSupportRequestValidationFlow(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateSupportRequest(rec, authedRequest(http.MethodPost, `{"type":""}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCreateSupportRequestConflictOnExisting(t *testing.T) {
	h := NewHandler(&mockQuerier{
		countOpenSupportRequests: func(_ context.Context, _ uuid.UUID) (int, error) { return 1, nil },
	})
	rec := httptest.NewRecorder()
	h.CreateSupportRequest(rec, authedRequest(http.MethodPost, `{"type":"need_to_talk","urgency":"soon"}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestCreateSupportRequestSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateSupportRequest(rec, authedRequest(http.MethodPost, `{"type":"need_to_talk","urgency":"soon"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestCreateSupportRequestDefaultsUrgency(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateSupportRequest(rec, authedRequest(http.MethodPost, `{"type":"need_to_talk"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
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
		closeSupportRequest: func(_ context.Context, _, _ uuid.UUID) error { return ErrNotFound },
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

func TestCreateSupportResponseRequiresAvailability(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestState: func(_ context.Context, _ uuid.UUID) (uuid.UUID, string, error) {
			return fixedOther, "open", nil
		},
		getSupportProfile: func(_ context.Context, _ uuid.UUID) (*SupportProfile, error) {
			return &SupportProfile{IsAvailableToSupport: false}, nil
		},
	})
	rec := httptest.NewRecorder()
	h.CreateSupportResponse(rec, authedRequestWithID(http.MethodPost, `{"response_type":"can_chat"}`, fixedRequest.String()))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
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
