package friends

import (
	"context"
	"encoding/json"
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

// mockQuerier is a test double for Querier. Each field holds an optional
// function override; zero values return sensible defaults (no rows / no error).
type mockQuerier struct {
	getFriendshipState      func(ctx context.Context, userAID, userBID uuid.UUID) (bool, string, uuid.UUID, error)
	insertFriendship        func(ctx context.Context, userAID, userBID, requesterID uuid.UUID) error
	acceptFriendRequest     func(ctx context.Context, userAID, userBID, userID, otherUserID uuid.UUID) error
	deletePendingFriendship func(ctx context.Context, userAID, userBID, requesterID uuid.UUID) error
	removeFriend            func(ctx context.Context, userAID, userBID, userID, otherUserID uuid.UUID) error
	listFriendUsers         func(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]friendUser, error)
	listPendingRequests     func(ctx context.Context, userID uuid.UUID, outgoing bool, before *time.Time, limit int) ([]friendUser, error)
}

func (m *mockQuerier) GetFriendshipState(ctx context.Context, userAID, userBID uuid.UUID) (bool, string, uuid.UUID, error) {
	if m.getFriendshipState != nil {
		return m.getFriendshipState(ctx, userAID, userBID)
	}
	return false, "", uuid.Nil, nil
}
func (m *mockQuerier) InsertFriendship(ctx context.Context, userAID, userBID, requesterID uuid.UUID) error {
	if m.insertFriendship != nil {
		return m.insertFriendship(ctx, userAID, userBID, requesterID)
	}
	return nil
}
func (m *mockQuerier) AcceptFriendRequest(ctx context.Context, userAID, userBID, userID, otherUserID uuid.UUID) error {
	if m.acceptFriendRequest != nil {
		return m.acceptFriendRequest(ctx, userAID, userBID, userID, otherUserID)
	}
	return nil
}
func (m *mockQuerier) DeletePendingFriendship(ctx context.Context, userAID, userBID, requesterID uuid.UUID) error {
	if m.deletePendingFriendship != nil {
		return m.deletePendingFriendship(ctx, userAID, userBID, requesterID)
	}
	return nil
}
func (m *mockQuerier) RemoveFriend(ctx context.Context, userAID, userBID, userID, otherUserID uuid.UUID) error {
	if m.removeFriend != nil {
		return m.removeFriend(ctx, userAID, userBID, userID, otherUserID)
	}
	return nil
}
func (m *mockQuerier) ListFriendUsers(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]friendUser, error) {
	if m.listFriendUsers != nil {
		return m.listFriendUsers(ctx, userID, before, limit)
	}
	return nil, nil
}
func (m *mockQuerier) ListPendingRequests(ctx context.Context, userID uuid.UUID, outgoing bool, before *time.Time, limit int) ([]friendUser, error) {
	if m.listPendingRequests != nil {
		return m.listPendingRequests(ctx, userID, outgoing, before, limit)
	}
	return nil, nil
}

// withUserID injects a userID into the request context (simulates Authenticate middleware).
func withUserID(r *http.Request, id uuid.UUID) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.UserIDKey, id))
}

// withURLParam sets a chi URL param on the request context.
func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// userRequest builds a GET request with both auth and a chi {id} URL param.
func userRequest(method, userID, targetID string) *http.Request {
	req := httptest.NewRequest(method, "/", nil)
	req = withUserID(req, uuid.MustParse(userID))
	req = withURLParam(req, "id", targetID)
	return req
}

var (
	fixedUserA = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	fixedUserB = uuid.MustParse("00000000-0000-0000-0000-000000000002")
)

// ── SendRequest ───────────────────────────────────────────────────────────────

func TestSortPairOrdersUUIDsLexicographically(t *testing.T) {
	a := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	b := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	first, second := sortPair(a, b)

	if first != b || second != a {
		t.Fatalf("sortPair() = (%v, %v), want (%v, %v)", first, second, b, a)
	}
}

func TestSendRequestRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req = withUserID(req, fixedUserA)
	req = withURLParam(req, "id", "not-a-uuid")
	rec := httptest.NewRecorder()

	h.SendRequest(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSendRequestRejectsSelf(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := userRequest(http.MethodPost, fixedUserA.String(), fixedUserA.String())
	rec := httptest.NewRecorder()

	h.SendRequest(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSendRequestReturnsAcceptedWhenAlreadyFriends(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getFriendshipState: func(_ context.Context, _, _ uuid.UUID) (bool, string, uuid.UUID, error) {
			return true, "accepted", fixedUserA, nil
		},
	})
	req := userRequest(http.MethodPost, fixedUserA.String(), fixedUserB.String())
	rec := httptest.NewRecorder()

	h.SendRequest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	assertResponseField(t, rec, "status", "accepted")
}

func TestSendRequestReturnsPendingOutgoingWhenAlreadySent(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getFriendshipState: func(_ context.Context, _, _ uuid.UUID) (bool, string, uuid.UUID, error) {
			return true, "pending", fixedUserA, nil // requesterID == userID (fixedUserA)
		},
	})
	req := userRequest(http.MethodPost, fixedUserA.String(), fixedUserB.String())
	rec := httptest.NewRecorder()

	h.SendRequest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	assertResponseField(t, rec, "status", "pending_outgoing")
}

func TestSendRequestReturnsConflictWhenIncomingPending(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getFriendshipState: func(_ context.Context, _, _ uuid.UUID) (bool, string, uuid.UUID, error) {
			return true, "pending", fixedUserB, nil // requesterID == other user
		},
	})
	req := userRequest(http.MethodPost, fixedUserA.String(), fixedUserB.String())
	rec := httptest.NewRecorder()

	h.SendRequest(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestSendRequestCreatesNewRequest(t *testing.T) {
	inserted := false
	h := NewHandler(&mockQuerier{
		insertFriendship: func(_ context.Context, _, _, _ uuid.UUID) error {
			inserted = true
			return nil
		},
	})
	req := userRequest(http.MethodPost, fixedUserA.String(), fixedUserB.String())
	rec := httptest.NewRecorder()

	h.SendRequest(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if !inserted {
		t.Fatal("expected InsertFriendship to be called")
	}
}

func TestSendRequestReturns500OnDBError(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getFriendshipState: func(_ context.Context, _, _ uuid.UUID) (bool, string, uuid.UUID, error) {
			return false, "", uuid.Nil, errors.New("db error")
		},
	})
	req := userRequest(http.MethodPost, fixedUserA.String(), fixedUserB.String())
	rec := httptest.NewRecorder()

	h.SendRequest(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// ── UpdateRequest ─────────────────────────────────────────────────────────────

func TestUpdateRequestRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"action":"accept"}`))
	req = withUserID(req, fixedUserA)
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()

	h.UpdateRequest(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdateRequestRejectsInvalidAction(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := userRequest(http.MethodPatch, fixedUserA.String(), fixedUserB.String())
	req.Body = http.NoBody
	// rebuild with body
	req2 := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"action":"poke"}`))
	req2 = withUserID(req2, fixedUserA)
	req2 = withURLParam(req2, "id", fixedUserB.String())
	rec := httptest.NewRecorder()

	h.UpdateRequest(rec, req2)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdateRequestAcceptSuccess(t *testing.T) {
	accepted := false
	h := NewHandler(&mockQuerier{
		acceptFriendRequest: func(_ context.Context, _, _, _, _ uuid.UUID) error {
			accepted = true
			return nil
		},
	})
	req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"action":"accept"}`))
	req = withUserID(req, fixedUserA)
	req = withURLParam(req, "id", fixedUserB.String())
	rec := httptest.NewRecorder()

	h.UpdateRequest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !accepted {
		t.Fatal("expected AcceptFriendRequest to be called")
	}
	assertResponseField(t, rec, "status", "accepted")
}

func TestUpdateRequestAcceptNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		acceptFriendRequest: func(_ context.Context, _, _, _, _ uuid.UUID) error {
			return ErrNotFound
		},
	})
	req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"action":"accept"}`))
	req = withUserID(req, fixedUserA)
	req = withURLParam(req, "id", fixedUserB.String())
	rec := httptest.NewRecorder()

	h.UpdateRequest(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestUpdateRequestDeclineSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"action":"decline"}`))
	req = withUserID(req, fixedUserA)
	req = withURLParam(req, "id", fixedUserB.String())
	rec := httptest.NewRecorder()

	h.UpdateRequest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	assertResponseField(t, rec, "status", "none")
}

func TestUpdateRequestDeclineNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		deletePendingFriendship: func(_ context.Context, _, _, _ uuid.UUID) error {
			return ErrNotFound
		},
	})
	req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"action":"decline"}`))
	req = withUserID(req, fixedUserA)
	req = withURLParam(req, "id", fixedUserB.String())
	rec := httptest.NewRecorder()

	h.UpdateRequest(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// ── CancelRequest ─────────────────────────────────────────────────────────────

func TestCancelRequestSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := userRequest(http.MethodDelete, fixedUserA.String(), fixedUserB.String())
	rec := httptest.NewRecorder()

	h.CancelRequest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	assertResponseField(t, rec, "status", "none")
}

func TestCancelRequestNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		deletePendingFriendship: func(_ context.Context, _, _, _ uuid.UUID) error {
			return ErrNotFound
		},
	})
	req := userRequest(http.MethodDelete, fixedUserA.String(), fixedUserB.String())
	rec := httptest.NewRecorder()

	h.CancelRequest(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// ── RemoveFriend ──────────────────────────────────────────────────────────────

func TestRemoveFriendSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := userRequest(http.MethodDelete, fixedUserA.String(), fixedUserB.String())
	rec := httptest.NewRecorder()

	h.RemoveFriend(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	assertResponseField(t, rec, "status", "none")
}

func TestRemoveFriendNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		removeFriend: func(_ context.Context, _, _, _, _ uuid.UUID) error {
			return ErrNotFound
		},
	})
	req := userRequest(http.MethodDelete, fixedUserA.String(), fixedUserB.String())
	rec := httptest.NewRecorder()

	h.RemoveFriend(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// ── ListFriends ───────────────────────────────────────────────────────────────

func TestListFriendsReturnsItems(t *testing.T) {
	now := time.Now().UTC()
	h := NewHandler(&mockQuerier{
		listFriendUsers: func(_ context.Context, _ uuid.UUID, _ *time.Time, _ int) ([]friendUser, error) {
			return []friendUser{
				{UserID: fixedUserB, Username: "bob", CreatedAt: now},
			}, nil
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withUserID(req, fixedUserA)
	rec := httptest.NewRecorder()

	h.ListFriends(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestListFriendsReturns500OnDBError(t *testing.T) {
	h := NewHandler(&mockQuerier{
		listFriendUsers: func(_ context.Context, _ uuid.UUID, _ *time.Time, _ int) ([]friendUser, error) {
			return nil, errors.New("db error")
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withUserID(req, fixedUserA)
	rec := httptest.NewRecorder()

	h.ListFriends(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// ── ListIncomingRequests / ListOutgoingRequests ───────────────────────────────

func TestListIncomingRequestsPassesOutgoingFalse(t *testing.T) {
	var gotOutgoing *bool
	h := NewHandler(&mockQuerier{
		listPendingRequests: func(_ context.Context, _ uuid.UUID, outgoing bool, _ *time.Time, _ int) ([]friendUser, error) {
			gotOutgoing = &outgoing
			return nil, nil
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withUserID(req, fixedUserA)
	rec := httptest.NewRecorder()

	h.ListIncomingRequests(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotOutgoing == nil || *gotOutgoing {
		t.Fatalf("expected outgoing=false, got %v", gotOutgoing)
	}
}

func TestListOutgoingRequestsPassesOutgoingTrue(t *testing.T) {
	var gotOutgoing *bool
	h := NewHandler(&mockQuerier{
		listPendingRequests: func(_ context.Context, _ uuid.UUID, outgoing bool, _ *time.Time, _ int) ([]friendUser, error) {
			gotOutgoing = &outgoing
			return nil, nil
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withUserID(req, fixedUserA)
	rec := httptest.NewRecorder()

	h.ListOutgoingRequests(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotOutgoing == nil || !*gotOutgoing {
		t.Fatalf("expected outgoing=true, got %v", gotOutgoing)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// assertResponseField decodes {"data": {"<field>": value}} and checks the string value.
func assertResponseField(t *testing.T, rec *httptest.ResponseRecorder, field, want string) {
	t.Helper()
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	got, ok := body.Data[field].(string)
	if !ok || got != want {
		t.Fatalf("data.%s = %v, want %q", field, body.Data[field], want)
	}
}
