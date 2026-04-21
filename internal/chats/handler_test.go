package chats

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
	listChats          func(ctx context.Context, userID uuid.UUID, query string, limit, offset int) ([]Chat, error)
	listChatRequests   func(ctx context.Context, userID uuid.UUID) ([]Chat, error)
	getChat            func(ctx context.Context, userID, chatID uuid.UUID) (*Chat, error)
	getChatStatus      func(ctx context.Context, chatID uuid.UUID) (string, error)
	findDirectChat     func(ctx context.Context, userID, otherUserID uuid.UUID) (uuid.UUID, bool, error)
	createChat         func(ctx context.Context, userID uuid.UUID, isGroup bool, name *string, memberIDs []uuid.UUID) (uuid.UUID, error)
	isAddresseeOfChat  func(ctx context.Context, chatID, userID uuid.UUID) (bool, error)
	acceptChatRequest  func(ctx context.Context, chatID uuid.UUID) error
	declineChatRequest func(ctx context.Context, chatID uuid.UUID) error
	isMemberOfChat     func(ctx context.Context, chatID, userID uuid.UUID) (bool, error)
	listMessages       func(ctx context.Context, chatID uuid.UUID, before *time.Time, limit int) ([]Message, error)
	insertMessage      func(ctx context.Context, chatID, userID uuid.UUID, body string) (uuid.UUID, error)
	deleteOrLeaveChat  func(ctx context.Context, chatID, userID uuid.UUID) (string, error)
}

func (m *mockQuerier) ListChats(ctx context.Context, userID uuid.UUID, query string, limit, offset int) ([]Chat, error) {
	if m.listChats != nil {
		return m.listChats(ctx, userID, query, limit, offset)
	}
	return nil, nil
}
func (m *mockQuerier) ListChatRequests(ctx context.Context, userID uuid.UUID) ([]Chat, error) {
	if m.listChatRequests != nil {
		return m.listChatRequests(ctx, userID)
	}
	return nil, nil
}
func (m *mockQuerier) GetChat(ctx context.Context, userID, chatID uuid.UUID) (*Chat, error) {
	if m.getChat != nil {
		return m.getChat(ctx, userID, chatID)
	}
	return &Chat{ID: chatID, Status: "active"}, nil
}
func (m *mockQuerier) GetChatStatus(ctx context.Context, chatID uuid.UUID) (string, error) {
	if m.getChatStatus != nil {
		return m.getChatStatus(ctx, chatID)
	}
	return "active", nil
}
func (m *mockQuerier) FindDirectChat(ctx context.Context, userID, otherUserID uuid.UUID) (uuid.UUID, bool, error) {
	if m.findDirectChat != nil {
		return m.findDirectChat(ctx, userID, otherUserID)
	}
	return uuid.Nil, false, nil
}
func (m *mockQuerier) CreateChat(ctx context.Context, userID uuid.UUID, isGroup bool, name *string, memberIDs []uuid.UUID) (uuid.UUID, error) {
	if m.createChat != nil {
		return m.createChat(ctx, userID, isGroup, name, memberIDs)
	}
	return uuid.MustParse("00000000-0000-0000-0000-000000000010"), nil
}
func (m *mockQuerier) IsAddresseeOfChat(ctx context.Context, chatID, userID uuid.UUID) (bool, error) {
	if m.isAddresseeOfChat != nil {
		return m.isAddresseeOfChat(ctx, chatID, userID)
	}
	return true, nil
}
func (m *mockQuerier) AcceptChatRequest(ctx context.Context, chatID uuid.UUID) error {
	if m.acceptChatRequest != nil {
		return m.acceptChatRequest(ctx, chatID)
	}
	return nil
}
func (m *mockQuerier) DeclineChatRequest(ctx context.Context, chatID uuid.UUID) error {
	if m.declineChatRequest != nil {
		return m.declineChatRequest(ctx, chatID)
	}
	return nil
}
func (m *mockQuerier) IsMemberOfChat(ctx context.Context, chatID, userID uuid.UUID) (bool, error) {
	if m.isMemberOfChat != nil {
		return m.isMemberOfChat(ctx, chatID, userID)
	}
	return true, nil
}
func (m *mockQuerier) ListMessages(ctx context.Context, chatID uuid.UUID, before *time.Time, limit int) ([]Message, error) {
	if m.listMessages != nil {
		return m.listMessages(ctx, chatID, before, limit)
	}
	return nil, nil
}
func (m *mockQuerier) InsertMessage(ctx context.Context, chatID, userID uuid.UUID, body string) (uuid.UUID, error) {
	if m.insertMessage != nil {
		return m.insertMessage(ctx, chatID, userID, body)
	}
	return uuid.MustParse("00000000-0000-0000-0000-000000000020"), nil
}
func (m *mockQuerier) DeleteOrLeaveChat(ctx context.Context, chatID, userID uuid.UUID) (string, error) {
	if m.deleteOrLeaveChat != nil {
		return m.deleteOrLeaveChat(ctx, chatID, userID)
	}
	return "deleted", nil
}

var (
	fixedUser  = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	fixedChat  = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	fixedOther = uuid.MustParse("00000000-0000-0000-0000-000000000003")
)

func withUserID(r *http.Request, id uuid.UUID) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.UserIDKey, id))
}

func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// ── ListChats ─────────────────────────────────────────────────────────────────

func TestListChatsSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/chats", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.ListChats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestListChatsDBError(t *testing.T) {
	h := NewHandler(&mockQuerier{
		listChats: func(_ context.Context, _ uuid.UUID, _ string, _, _ int) ([]Chat, error) {
			return nil, errors.New("db error")
		},
	})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/chats", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.ListChats(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// ── ListChatRequests ──────────────────────────────────────────────────────────

func TestListChatRequestsSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/chats/requests", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.ListChatRequests(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestListChatRequestsDBError(t *testing.T) {
	h := NewHandler(&mockQuerier{
		listChatRequests: func(_ context.Context, _ uuid.UUID) ([]Chat, error) {
			return nil, errors.New("db error")
		},
	})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/chats/requests", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.ListChatRequests(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// ── CreateChat ────────────────────────────────────────────────────────────────

func TestCreateChatInvalidBody(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodPost, "/chats", strings.NewReader("{")), fixedUser)
	rec := httptest.NewRecorder()

	h.CreateChat(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateChatDirectChatAlreadyExists(t *testing.T) {
	h := NewHandler(&mockQuerier{
		findDirectChat: func(_ context.Context, _, _ uuid.UUID) (uuid.UUID, bool, error) {
			return fixedChat, true, nil
		},
	})
	body := `{"member_ids":["` + fixedOther.String() + `"]}`
	req := withUserID(httptest.NewRequest(http.MethodPost, "/chats", strings.NewReader(body)), fixedUser)
	rec := httptest.NewRecorder()

	h.CreateChat(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestCreateChatNewDirectChat(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	body := `{"member_ids":["` + fixedOther.String() + `"]}`
	req := withUserID(httptest.NewRequest(http.MethodPost, "/chats", strings.NewReader(body)), fixedUser)
	rec := httptest.NewRecorder()

	h.CreateChat(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestCreateChatGroupChat(t *testing.T) {
	other2 := uuid.MustParse("00000000-0000-0000-0000-000000000004")
	h := NewHandler(&mockQuerier{})
	body := `{"member_ids":["` + fixedOther.String() + `","` + other2.String() + `"]}`
	req := withUserID(httptest.NewRequest(http.MethodPost, "/chats", strings.NewReader(body)), fixedUser)
	rec := httptest.NewRecorder()

	h.CreateChat(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestCreateChatDBError(t *testing.T) {
	h := NewHandler(&mockQuerier{
		createChat: func(_ context.Context, _ uuid.UUID, _ bool, _ *string, _ []uuid.UUID) (uuid.UUID, error) {
			return uuid.Nil, errors.New("db error")
		},
	})
	body := `{"member_ids":["` + fixedOther.String() + `"]}`
	req := withUserID(httptest.NewRequest(http.MethodPost, "/chats", strings.NewReader(body)), fixedUser)
	rec := httptest.NewRecorder()

	h.CreateChat(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// ── UpdateChatStatus ──────────────────────────────────────────────────────────

func TestUpdateChatStatusInvalidID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/chats/bad/status", strings.NewReader(`{"status":"active"}`)), fixedUser)
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()

	h.UpdateChatStatus(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdateChatStatusInvalidBody(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader("{")), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.UpdateChatStatus(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdateChatStatusInvalidStatus(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"status":"invalid"}`)), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.UpdateChatStatus(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdateChatStatusNotAddressee(t *testing.T) {
	h := NewHandler(&mockQuerier{
		isAddresseeOfChat: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return false, nil
		},
	})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"status":"active"}`)), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.UpdateChatStatus(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestUpdateChatStatusAccept(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"status":"active"}`)), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.UpdateChatStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestUpdateChatStatusDecline(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"status":"declined"}`)), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.UpdateChatStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// ── GetMessages ───────────────────────────────────────────────────────────────

func TestGetMessagesInvalidID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/", nil), fixedUser)
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()

	h.GetMessages(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetMessagesNotMember(t *testing.T) {
	h := NewHandler(&mockQuerier{
		isMemberOfChat: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return false, nil
		},
	})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/", nil), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.GetMessages(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestGetMessagesInvalidBefore(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/?before=notadate", nil), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.GetMessages(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetMessagesSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/", nil), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.GetMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGetMessagesDBError(t *testing.T) {
	h := NewHandler(&mockQuerier{
		listMessages: func(_ context.Context, _ uuid.UUID, _ *time.Time, _ int) ([]Message, error) {
			return nil, errors.New("db error")
		},
	})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/", nil), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.GetMessages(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// ── SendMessage ───────────────────────────────────────────────────────────────

func TestSendMessageInvalidID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"body":"hi"}`)), fixedUser)
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()

	h.SendMessage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSendMessageNotMember(t *testing.T) {
	h := NewHandler(&mockQuerier{
		isMemberOfChat: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return false, nil
		},
	})
	req := withUserID(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"body":"hi"}`)), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.SendMessage(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestSendMessageEmptyBody(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"body":"   "}`)), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.SendMessage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSendMessageSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"body":"hello"}`)), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.SendMessage(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

// ── DeleteChat ────────────────────────────────────────────────────────────────

func TestDeleteChatInvalidID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodDelete, "/", nil), fixedUser)
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()

	h.DeleteChat(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestDeleteChatNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		deleteOrLeaveChat: func(_ context.Context, _, _ uuid.UUID) (string, error) {
			return "", ErrNotFound
		},
	})
	req := withUserID(httptest.NewRequest(http.MethodDelete, "/", nil), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.DeleteChat(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDeleteChatForbidden(t *testing.T) {
	h := NewHandler(&mockQuerier{
		deleteOrLeaveChat: func(_ context.Context, _, _ uuid.UUID) (string, error) {
			return "", ErrForbidden
		},
	})
	req := withUserID(httptest.NewRequest(http.MethodDelete, "/", nil), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.DeleteChat(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestDeleteChatSuccessDeleted(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := withUserID(httptest.NewRequest(http.MethodDelete, "/", nil), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.DeleteChat(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestDeleteChatSuccessLeft(t *testing.T) {
	h := NewHandler(&mockQuerier{
		deleteOrLeaveChat: func(_ context.Context, _, _ uuid.UUID) (string, error) {
			return "left", nil
		},
	})
	req := withUserID(httptest.NewRequest(http.MethodDelete, "/", nil), fixedUser)
	req = withURLParam(req, "id", fixedChat.String())
	rec := httptest.NewRecorder()

	h.DeleteChat(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
