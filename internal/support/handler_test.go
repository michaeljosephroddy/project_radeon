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
	countHighUrgencyRequestsSince func(ctx context.Context, userID uuid.UUID, since time.Time) (int, error)
	createSupportRequest          func(ctx context.Context, userID uuid.UUID, input CreateSupportRequestInput) (*SupportRequest, error)
	acceptSupportOffer            func(ctx context.Context, requesterID, requestID, offerID uuid.UUID) (*SupportRequest, error)
	getSupportRequest             func(ctx context.Context, viewerID, requestID uuid.UUID) (*SupportRequest, error)
	closeSupportRequest           func(ctx context.Context, requestID, userID uuid.UUID) ([]uuid.UUID, error)
	listMySupportRequests         func(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]SupportRequest, error)
	listVisibleSupportRequests    func(ctx context.Context, userID uuid.UUID, filter SupportRequestFilter, cursor *SupportFeedCursor, limit int) ([]SupportRequest, error)
	getSupportRequestState        func(ctx context.Context, requestID uuid.UUID) (uuid.UUID, string, error)
	createSupportOffer            func(ctx context.Context, requestID, userID uuid.UUID, offerType string, message *string, scheduledFor *time.Time) (*CreateSupportOfferResult, error)
	getSupportRequestOwner        func(ctx context.Context, requestID uuid.UUID) (uuid.UUID, error)
	listSupportOffers             func(ctx context.Context, requestID uuid.UUID, limit, offset int) ([]SupportOffer, error)
	createSupportReply            func(ctx context.Context, requestID, authorID uuid.UUID, body string) (*SupportReply, error)
	listSupportReplies            func(ctx context.Context, requestID uuid.UUID, cursor *SupportReplyCursor, limit int) ([]SupportReply, error)
	declineSupportOffer           func(ctx context.Context, requesterID, requestID, offerID uuid.UUID) error
	cancelSupportOffer            func(ctx context.Context, responderID, requestID, offerID uuid.UUID) error
}

func (m *mockQuerier) CountOpenSupportRequests(ctx context.Context, userID uuid.UUID) (int, error) {
	if m.countOpenSupportRequests != nil {
		return m.countOpenSupportRequests(ctx, userID)
	}
	return 0, nil
}
func (m *mockQuerier) CountHighUrgencySupportRequestsSince(ctx context.Context, userID uuid.UUID, since time.Time) (int, error) {
	if m.countHighUrgencyRequestsSince != nil {
		return m.countHighUrgencyRequestsSince(ctx, userID, since)
	}
	return 0, nil
}
func (m *mockQuerier) CreateSupportRequest(ctx context.Context, userID uuid.UUID, input CreateSupportRequestInput) (*SupportRequest, error) {
	if m.createSupportRequest != nil {
		return m.createSupportRequest(ctx, userID, input)
	}
	return &SupportRequest{ID: uuid.New(), RequesterID: userID, SupportType: input.SupportType, Urgency: input.Urgency}, nil
}
func (m *mockQuerier) AcceptSupportOffer(ctx context.Context, requesterID, requestID, offerID uuid.UUID) (*SupportRequest, error) {
	if m.acceptSupportOffer != nil {
		return m.acceptSupportOffer(ctx, requesterID, requestID, offerID)
	}
	return &SupportRequest{ID: requestID, RequesterID: requesterID, AcceptedResponseID: &offerID}, nil
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
func (m *mockQuerier) ListVisibleSupportRequests(ctx context.Context, userID uuid.UUID, filter SupportRequestFilter, cursor *SupportFeedCursor, limit int) ([]SupportRequest, error) {
	if m.listVisibleSupportRequests != nil {
		return m.listVisibleSupportRequests(ctx, userID, filter, cursor, limit)
	}
	return nil, nil
}
func (m *mockQuerier) GetSupportRequestState(ctx context.Context, requestID uuid.UUID) (uuid.UUID, string, error) {
	if m.getSupportRequestState != nil {
		return m.getSupportRequestState(ctx, requestID)
	}
	return uuid.Nil, "", ErrNotFound
}
func (m *mockQuerier) CreateSupportOffer(ctx context.Context, requestID, userID uuid.UUID, offerType string, message *string, scheduledFor *time.Time) (*CreateSupportOfferResult, error) {
	if m.createSupportOffer != nil {
		return m.createSupportOffer(ctx, requestID, userID, offerType, message, scheduledFor)
	}
	return &CreateSupportOfferResult{Offer: &SupportOffer{ID: uuid.New()}}, nil
}
func (m *mockQuerier) GetSupportRequestOwner(ctx context.Context, requestID uuid.UUID) (uuid.UUID, error) {
	if m.getSupportRequestOwner != nil {
		return m.getSupportRequestOwner(ctx, requestID)
	}
	return uuid.Nil, ErrNotFound
}
func (m *mockQuerier) ListSupportOffers(ctx context.Context, requestID uuid.UUID, limit, offset int) ([]SupportOffer, error) {
	if m.listSupportOffers != nil {
		return m.listSupportOffers(ctx, requestID, limit, offset)
	}
	return nil, nil
}
func (m *mockQuerier) CreateSupportReply(ctx context.Context, requestID, authorID uuid.UUID, body string) (*SupportReply, error) {
	if m.createSupportReply != nil {
		return m.createSupportReply(ctx, requestID, authorID, body)
	}
	return &SupportReply{ID: uuid.New(), SupportRequestID: requestID, AuthorID: authorID, Body: body}, nil
}
func (m *mockQuerier) ListSupportReplies(ctx context.Context, requestID uuid.UUID, cursor *SupportReplyCursor, limit int) ([]SupportReply, error) {
	if m.listSupportReplies != nil {
		return m.listSupportReplies(ctx, requestID, cursor, limit)
	}
	return nil, nil
}
func (m *mockQuerier) DeclineSupportOffer(ctx context.Context, requesterID, requestID, offerID uuid.UUID) error {
	if m.declineSupportOffer != nil {
		return m.declineSupportOffer(ctx, requesterID, requestID, offerID)
	}
	return nil
}
func (m *mockQuerier) CancelSupportOffer(ctx context.Context, responderID, requestID, offerID uuid.UUID) error {
	if m.cancelSupportOffer != nil {
		return m.cancelSupportOffer(ctx, responderID, requestID, offerID)
	}
	return nil
}

var (
	fixedUser    = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	fixedRequest = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	fixedOther   = uuid.MustParse("00000000-0000-0000-0000-000000000003")
)

func withUserID(r *http.Request, id uuid.UUID) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.UserIDKey, id))
}

func TestListSupportRequestsRejectsInvalidFilter(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	req := authedRequest(http.MethodGet, "")
	req.URL.RawQuery = "filter=bad"

	h.ListSupportRequests(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListSupportRequestsPassesFilterAndCursor(t *testing.T) {
	var seenFilter SupportRequestFilter
	var seenCursor *SupportFeedCursor

	h := NewHandler(&mockQuerier{
		listVisibleSupportRequests: func(_ context.Context, _ uuid.UUID, filter SupportRequestFilter, cursor *SupportFeedCursor, limit int) ([]SupportRequest, error) {
			seenFilter = filter
			seenCursor = cursor
			return []SupportRequest{}, nil
		},
	})

	servedAt := time.Date(2026, 4, 28, 9, 0, 0, 0, time.UTC)
	encodedCursor, err := encodeSupportFeedCursor(SupportFeedCursor{
		Score:     245.5,
		CreatedAt: time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC),
		ID:        fixedRequest,
		ServedAt:  servedAt,
	})
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}

	rec := httptest.NewRecorder()
	req := authedRequest(http.MethodGet, "")
	req.URL.RawQuery = "filter=urgent&cursor=" + *encodedCursor

	h.ListSupportRequests(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if seenFilter != SupportRequestFilterUrgent {
		t.Fatalf("filter = %q, want %q", seenFilter, SupportRequestFilterUrgent)
	}
	if seenCursor == nil || seenCursor.ID != fixedRequest || seenCursor.Score != 245.5 || !seenCursor.ServedAt.Equal(servedAt) {
		t.Fatalf("cursor = %#v, want populated decoded cursor", seenCursor)
	}
}

func TestParseSupportFeedCursorRejectsInvalidBase64(t *testing.T) {
	if _, err := parseSupportFeedCursor("not-valid-%%%"); err == nil {
		t.Fatal("expected cursor parse error")
	}
}

func TestEncodeSupportFeedCursorProducesRoundTripPayload(t *testing.T) {
	cursor := SupportFeedCursor{
		Score:     312.25,
		CreatedAt: time.Date(2026, 4, 28, 11, 0, 0, 0, time.UTC),
		ID:        fixedOther,
		ServedAt:  time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
	}

	encoded, err := encodeSupportFeedCursor(cursor)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}

	payload, err := base64.RawURLEncoding.DecodeString(*encoded)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}

	var decoded SupportFeedCursor
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

// ── CreateSupportOffer ─────────────────────────────────────────────────────

func TestCreateSupportOfferValidationFlow(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateSupportOffer(rec, authedRequestWithID(http.MethodPost, `{"offer_type":"bad"}`, fixedRequest.String()))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCreateSupportOfferRequestNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestState: func(_ context.Context, _ uuid.UUID) (uuid.UUID, string, error) {
			return uuid.Nil, "", ErrNotFound
		},
	})
	rec := httptest.NewRecorder()
	h.CreateSupportOffer(rec, authedRequestWithID(http.MethodPost, `{"offer_type":"chat"}`, fixedRequest.String()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCreateSupportOfferCannotRespondToOwnRequest(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestState: func(_ context.Context, _ uuid.UUID) (uuid.UUID, string, error) {
			return fixedUser, "open", nil
		},
	})
	rec := httptest.NewRecorder()
	h.CreateSupportOffer(rec, authedRequestWithID(http.MethodPost, `{"offer_type":"chat"}`, fixedRequest.String()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateSupportOfferRequestNoLongerOpen(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestState: func(_ context.Context, _ uuid.UUID) (uuid.UUID, string, error) {
			return fixedOther, "closed", nil
		},
	})
	rec := httptest.NewRecorder()
	h.CreateSupportOffer(rec, authedRequestWithID(http.MethodPost, `{"offer_type":"chat"}`, fixedRequest.String()))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestCreateSupportOfferSuccessWhenRequesterIsOtherUser(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestState: func(_ context.Context, _ uuid.UUID) (uuid.UUID, string, error) {
			return fixedOther, "open", nil
		},
	})
	rec := httptest.NewRecorder()
	h.CreateSupportOffer(rec, authedRequestWithID(http.MethodPost, `{"offer_type":"chat"}`, fixedRequest.String()))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestCreateSupportOfferSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestState: func(_ context.Context, _ uuid.UUID) (uuid.UUID, string, error) {
			return fixedOther, "open", nil
		},
	})
	rec := httptest.NewRecorder()
	h.CreateSupportOffer(rec, authedRequestWithID(http.MethodPost, `{"offer_type":"chat"}`, fixedRequest.String()))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

// ── ListSupportOffers ──────────────────────────────────────────────────────

func TestListSupportOffersRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.ListSupportOffers(rec, authedRequestWithID(http.MethodGet, "", "bad"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListSupportOffersRequestNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestOwner: func(_ context.Context, _ uuid.UUID) (uuid.UUID, error) {
			return uuid.Nil, ErrNotFound
		},
	})
	rec := httptest.NewRecorder()
	h.ListSupportOffers(rec, authedRequestWithID(http.MethodGet, "", fixedRequest.String()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestListSupportOffersForbiddenForNonOwner(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestOwner: func(_ context.Context, _ uuid.UUID) (uuid.UUID, error) {
			return fixedOther, nil
		},
	})
	rec := httptest.NewRecorder()
	h.ListSupportOffers(rec, authedRequestWithID(http.MethodGet, "", fixedRequest.String()))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestListSupportOffersSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getSupportRequestOwner: func(_ context.Context, _ uuid.UUID) (uuid.UUID, error) {
			return fixedUser, nil
		},
	})
	rec := httptest.NewRecorder()
	h.ListSupportOffers(rec, authedRequestWithID(http.MethodGet, "", fixedRequest.String()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
