package user

import (
	"context"
	"errors"
	"io"
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
	getUser                 func(ctx context.Context, viewerID, userID uuid.UUID) (*User, error)
	usernameExistsForOthers func(ctx context.Context, username string, userID uuid.UUID) (bool, error)
	updateUser              func(ctx context.Context, userID uuid.UUID, username, city, country, bio *string, soberSince *time.Time, replaceSoberSince bool, interests []string, replaceInterests bool) error
	updateAvatarURL         func(ctx context.Context, userID uuid.UUID, avatarURL string) error
	discoverUsers           func(ctx context.Context, currentUserID uuid.UUID, city, query string, limit, offset int) ([]User, error)
	listInterests           func(ctx context.Context) ([]string, error)
}

func (m *mockQuerier) GetUser(ctx context.Context, viewerID, userID uuid.UUID) (*User, error) {
	if m.getUser != nil {
		return m.getUser(ctx, viewerID, userID)
	}
	return &User{ID: userID, Username: "testuser"}, nil
}
func (m *mockQuerier) UsernameExistsForOthers(ctx context.Context, uname string, userID uuid.UUID) (bool, error) {
	if m.usernameExistsForOthers != nil {
		return m.usernameExistsForOthers(ctx, uname, userID)
	}
	return false, nil
}
func (m *mockQuerier) UpdateUser(ctx context.Context, userID uuid.UUID, username, city, country, bio *string, soberSince *time.Time, replaceSoberSince bool, interests []string, replaceInterests bool) error {
	if m.updateUser != nil {
		return m.updateUser(ctx, userID, username, city, country, bio, soberSince, replaceSoberSince, interests, replaceInterests)
	}
	return nil
}
func (m *mockQuerier) UpdateAvatarURL(ctx context.Context, userID uuid.UUID, avatarURL string) error {
	if m.updateAvatarURL != nil {
		return m.updateAvatarURL(ctx, userID, avatarURL)
	}
	return nil
}
func (m *mockQuerier) DiscoverUsers(ctx context.Context, currentUserID uuid.UUID, city, query string, limit, offset int) ([]User, error) {
	if m.discoverUsers != nil {
		return m.discoverUsers(ctx, currentUserID, city, query, limit, offset)
	}
	return nil, nil
}

func (m *mockQuerier) ListInterests(ctx context.Context) ([]string, error) {
	if m.listInterests != nil {
		return m.listInterests(ctx)
	}
	return []string{"Coffee", "Hiking", "Meditation"}, nil
}

type mockUploader struct {
	upload func(ctx context.Context, key, contentType string, body io.Reader) (string, error)
}

func (m *mockUploader) Upload(ctx context.Context, key, contentType string, body io.Reader) (string, error) {
	if m.upload != nil {
		return m.upload(ctx, key, contentType, body)
	}
	return "https://example.com/avatar.jpg", nil
}

var (
	fixedUser  = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	fixedOther = uuid.MustParse("00000000-0000-0000-0000-000000000002")
)

func withUserID(r *http.Request, id uuid.UUID) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.UserIDKey, id))
}

func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// ── GetMe ─────────────────────────────────────────────────────────────────────

func TestGetMeSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/users/me", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGetMeNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getUser: func(_ context.Context, _, _ uuid.UUID) (*User, error) { return nil, ErrNotFound },
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/users/me", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// ── GetUser ───────────────────────────────────────────────────────────────────

func TestGetUserRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/", nil), fixedUser)
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()

	h.GetUser(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetUserNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getUser: func(_ context.Context, _, _ uuid.UUID) (*User, error) { return nil, ErrNotFound },
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/", nil), fixedUser)
	req = withURLParam(req, "id", fixedOther.String())
	rec := httptest.NewRecorder()

	h.GetUser(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGetUserSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/", nil), fixedUser)
	req = withURLParam(req, "id", fixedOther.String())
	rec := httptest.NewRecorder()

	h.GetUser(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// ── UpdateMe ──────────────────────────────────────────────────────────────────

func TestUpdateMeInvalidBody(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/users/me", strings.NewReader("{")), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMe(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdateMeInvalidUsername(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/users/me", strings.NewReader(`{"username":"ab"}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMe(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestUpdateMeUsernameConflict(t *testing.T) {
	h := NewHandler(&mockQuerier{
		usernameExistsForOthers: func(_ context.Context, _ string, _ uuid.UUID) (bool, error) {
			return true, nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/users/me", strings.NewReader(`{"username":"taken.name"}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMe(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestUpdateMeSuccess(t *testing.T) {
	city := "Dublin"
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/users/me", strings.NewReader(`{"city":"Dublin"}`)), fixedUser)
	_ = city
	rec := httptest.NewRecorder()

	h.UpdateMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestUpdateMeDBError(t *testing.T) {
	h := NewHandler(&mockQuerier{
		updateUser: func(_ context.Context, _ uuid.UUID, _, _, _, _ *string, _ *time.Time, _ bool, _ []string, _ bool) error {
			return errors.New("db error")
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/users/me", strings.NewReader(`{"city":"Dublin"}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMe(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestUpdateMePersistsSoberSince(t *testing.T) {
	var gotSoberSince *time.Time
	var gotReplace bool
	h := NewHandler(&mockQuerier{
		updateUser: func(_ context.Context, _ uuid.UUID, _, _, _, _ *string, soberSince *time.Time, replaceSoberSince bool, _ []string, _ bool) error {
			gotSoberSince = soberSince
			gotReplace = replaceSoberSince
			return nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/users/me", strings.NewReader(`{"sober_since":"2026-04-01"}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !gotReplace {
		t.Fatal("expected sober_since replacement flag to be true")
	}
	if gotSoberSince == nil || gotSoberSince.Format("2006-01-02") != "2026-04-01" {
		t.Fatalf("unexpected sober_since value: %v", gotSoberSince)
	}
}

func TestUpdateMeRejectsInvalidSoberSince(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/users/me", strings.NewReader(`{"sober_since":"04/01/2026"}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMe(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdateMeRejectsLongBio(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/users/me", strings.NewReader(`{"bio":"`+strings.Repeat("a", 161)+`"}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMe(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestUpdateMeRejectsInvalidInterest(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/users/me", strings.NewReader(`{"interests":["Coffee","Clubbing"]}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMe(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestListInterestsSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/interests", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.ListInterests(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// ── Discover ──────────────────────────────────────────────────────────────────

func TestDiscoverSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/users/discover", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.Discover(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestDiscoverDBError(t *testing.T) {
	h := NewHandler(&mockQuerier{
		discoverUsers: func(_ context.Context, _ uuid.UUID, _, _ string, _, _ int) ([]User, error) {
			return nil, errors.New("db error")
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/users/discover", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.Discover(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
