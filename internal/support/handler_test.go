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
	getSupportProfile          func(ctx context.Context, userID uuid.UUID) (*SupportProfile, error)
	updateSupportProfile       func(ctx context.Context, userID uuid.UUID, available bool, modes []string) (*SupportProfile, error)
	countOpenSupportRequests   func(ctx context.Context, userID uuid.UUID) (int, error)
	createSupportRequest       func(ctx context.Context, userID uuid.UUID, reqType string, message *string, audience string, expiresAt time.Time) (*SupportRequest, error)
	getSupportRequest          func(ctx context.Context, viewerID, requestID uuid.UUID) (*SupportRequest, error)
	closeSupportRequest        func(ctx context.Context, requestID, userID uuid.UUID) error
	listMySupportRequests      func(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error)
	listVisibleSupportRequests func(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error)
	fetchSupportSummary        func(ctx context.Context, viewerID uuid.UUID) (int, int, error)
	getSupportRequestState     func(ctx context.Context, requestID uuid.UUID) (uuid.UUID, string, time.Time, error)
	createSupportResponse      func(ctx context.Context, requestID, userID uuid.UUID, responseType string, message *string) (*SupportResponse, error)
	getSupportRequestOwner     func(ctx context.Context, requestID uuid.UUID) (uuid.UUID, error)
	listSupportResponses       func(ctx context.Context, requestID uuid.UUID, limit, offset int) ([]SupportResponse, error)
}

func (m *mockQuerier) GetSupportProfile(ctx context.Context, userID uuid.UUID) (*SupportProfile, error) {
	if m.getSupportProfile != nil {
		return m.getSupportProfile(ctx, userID)
	}
	return &SupportProfile{SupportModes: []string{}}, nil
}
func (m *mockQuerier) UpdateSupportProfile(ctx context.Context, userID uuid.UUID, available bool, modes []string) (*SupportProfile, error) {
	if m.updateSupportProfile != nil {
		return m.updateSupportProfile(ctx, userID, available, modes)
	}
	return &SupportProfile{IsAvailableToSupport: available, SupportModes: modes}, nil
}
func (m *mockQuerier) CountOpenSupportRequests(ctx context.Context, userID uuid.UUID) (int, error) {
	if m.countOpenSupportRequests != nil {
		return m.countOpenSupportRequests(ctx, userID)
	}
	return 0, nil
}
func (m *mockQuerier) CreateSupportRequest(ctx context.Context, userID uuid.UUID, reqType string, message *string, audience string, expiresAt time.Time) (*SupportRequest, error) {
	if m.createSupportRequest != nil {
		return m.createSupportRequest(ctx, userID, reqType, message, audience, expiresAt)
	}
	return &SupportRequest{ID: uuid.New(), RequesterID: userID}, nil
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
func (m *mockQuerier) FetchSupportSummary(ctx context.Context, viewerID uuid.UUID) (int, int, error) {
	if m.fetchSupportSummary != nil {
		return m.fetchSupportSummary(ctx, viewerID)
	}
	return 0, 0, nil
}
func (m *mockQuerier) GetSupportRequestState(ctx context.Context, requestID uuid.UUID) (uuid.UUID, string, time.Time, error) {
	if m.getSupportRequestState != nil {
		return m.getSupportRequestState(ctx, requestID)
	}
	return uuid.Nil, "", time.Time{}, ErrNotFound
}
func (m *mockQuerier) CreateSupportResponse(ctx context.Context, requestID, userID uuid.UUID, responseType string, message *string) (*SupportResponse, error) {
	if m.createSupportResponse != nil {
		return m.createSupportResponse(ctx, requestID, userID, responseType, message)
	}
	return &SupportResponse{ID: uuid.New()}, nil
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
	h.UpdateMySupportProfile(rec, authedRequest(http.MethodPatch, `{"is_available_to_support":true,"support_modes":["can_chat"]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// ── CreateSupportRequest ──────────────────────────────────────────────────────

func TestCreateSupportRequestValidationFlow(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateSupportRequest(rec, authedRequest(http.MethodPost, `{"type":"","audience":"","expires_at":""}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCreateSupportRequestRejectsPastExpiry(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateSupportRequest(rec, authedRequest(http.MethodPost, `{"type":"need_to_talk","audience":"community","expires_at":"2020-01-01T00:00:00Z"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateSupportRequestConflictOnExisting(t *testing.T) {
	h := NewHandler(&mockQuerier{
		countOpenSupportRequests: func(_ context.Context, _ uuid.UUID) (int, error) { return 1, nil },
	})
	rec := httptest.NewRecorder()
	h.CreateSupportRequest(rec, authedRequest(http.MethodPost, `{"type":"need_to_talk","audience":"community","expires_at":"2030-01-01T00:00:00Z"}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestCreateSupportRequestSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateSupportRequest(rec, authedRequest(http.MethodPost, `{"type":"need_to_talk","audience":"community","expires_at":"2030-01-01T00:00:00Z"}`))
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
		getSupportRequestState: func(_ context.Context, _ uuid.UUID) (uuid.UUID, string, time.Time, error) {
			return uuid.Nil, "", time.Time{}, ErrNotFound
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
		getSupportRequestState: func(_ context.Context, _ uuid.UUID) (uuid.UUID, string, time.Time, error) {
			return fixedUser, "open", time.Now().Add(time.Hour), nil
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
		getSupportRequestState: func(_ context.Context, _ uuid.UUID) (uuid.UUID, string, time.Time, error) {
			return fixedOther, "closed", time.Now().Add(time.Hour), nil
		},
	})
	rec := httptest.NewRecorder()
	h.CreateSupportResponse(rec, authedRequestWithID(http.MethodPost, `{"response_type":"can_chat"}`, fixedRequest.String()))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestCreateSupportResponseSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestState: func(_ context.Context, _ uuid.UUID) (uuid.UUID, string, time.Time, error) {
			return fixedOther, "open", time.Now().Add(time.Hour), nil
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
			return fixedOther, nil // owner is not the caller
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
			return fixedUser, nil // caller is the owner
		},
	})
	rec := httptest.NewRecorder()
	h.ListSupportResponses(rec, authedRequestWithID(http.MethodGet, "", fixedRequest.String()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
