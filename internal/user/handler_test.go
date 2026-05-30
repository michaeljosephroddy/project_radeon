package user

import (
	"context"
	"encoding/json"
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
	updateUser              func(ctx context.Context, userID uuid.UUID, username, city, country, gender, bio *string, soberSince *time.Time, replaceSoberSince bool, birthDate *time.Time, replaceBirthDate bool, interests []string, replaceInterests bool, lat, lng *float64) error
	completeOnboarding      func(ctx context.Context, userID uuid.UUID) error
	updateAvatarURL         func(ctx context.Context, userID uuid.UUID, avatarURL string) error
	deleteCurrentUser       func(ctx context.Context, userID uuid.UUID) error
	discoverUsers           func(ctx context.Context, params DiscoverUsersParams) ([]User, error)
	countDiscoverUsers      func(ctx context.Context, params DiscoverUsersParams) (int, error)
	blockUser               func(ctx context.Context, blockerID, blockedID uuid.UUID) error
	unblockUser             func(ctx context.Context, blockerID, blockedID uuid.UUID) error
	listBlockedUsers        func(ctx context.Context, userID uuid.UUID, before *BlockedUsersCursor, limit int) ([]BlockedUser, error)
	reportUser              func(ctx context.Context, reporterID, reportedUserID uuid.UUID, reason string, details *string) error
	listInterests           func(ctx context.Context) ([]string, error)
	updateCurrentLocation   func(ctx context.Context, userID uuid.UUID, lat, lng float64, city, country string) error
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
func (m *mockQuerier) UpdateUser(ctx context.Context, userID uuid.UUID, username, city, country, gender, bio *string, soberSince *time.Time, replaceSoberSince bool, birthDate *time.Time, replaceBirthDate bool, interests []string, replaceInterests bool, lat, lng *float64) error {
	if m.updateUser != nil {
		return m.updateUser(ctx, userID, username, city, country, gender, bio, soberSince, replaceSoberSince, birthDate, replaceBirthDate, interests, replaceInterests, lat, lng)
	}
	return nil
}
func (m *mockQuerier) CompleteOnboarding(ctx context.Context, userID uuid.UUID) error {
	if m.completeOnboarding != nil {
		return m.completeOnboarding(ctx, userID)
	}
	return nil
}
func (m *mockQuerier) UpdateAvatarURL(ctx context.Context, userID uuid.UUID, avatarURL string) error {
	if m.updateAvatarURL != nil {
		return m.updateAvatarURL(ctx, userID, avatarURL)
	}
	return nil
}
func (m *mockQuerier) DeleteCurrentUser(ctx context.Context, userID uuid.UUID) error {
	if m.deleteCurrentUser != nil {
		return m.deleteCurrentUser(ctx, userID)
	}
	return nil
}
func (m *mockQuerier) DiscoverUsers(ctx context.Context, params DiscoverUsersParams) ([]User, error) {
	if m.discoverUsers != nil {
		return m.discoverUsers(ctx, params)
	}
	return nil, nil
}

func (m *mockQuerier) CountDiscoverUsers(ctx context.Context, params DiscoverUsersParams) (int, error) {
	if m.countDiscoverUsers != nil {
		return m.countDiscoverUsers(ctx, params)
	}
	return 0, nil
}

func (m *mockQuerier) BlockUser(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	if m.blockUser != nil {
		return m.blockUser(ctx, blockerID, blockedID)
	}
	return nil
}

func (m *mockQuerier) UnblockUser(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	if m.unblockUser != nil {
		return m.unblockUser(ctx, blockerID, blockedID)
	}
	return nil
}

func (m *mockQuerier) ListBlockedUsers(ctx context.Context, userID uuid.UUID, before *BlockedUsersCursor, limit int) ([]BlockedUser, error) {
	if m.listBlockedUsers != nil {
		return m.listBlockedUsers(ctx, userID, before, limit)
	}
	return nil, nil
}

func (m *mockQuerier) ReportUser(ctx context.Context, reporterID, reportedUserID uuid.UUID, reason string, details *string) error {
	if m.reportUser != nil {
		return m.reportUser(ctx, reporterID, reportedUserID, reason, details)
	}
	return nil
}

func (m *mockQuerier) ListInterests(ctx context.Context) ([]string, error) {
	if m.listInterests != nil {
		return m.listInterests(ctx)
	}
	return []string{"Coffee", "Hiking", "Meditation"}, nil
}

func (m *mockQuerier) UpdateCurrentLocation(ctx context.Context, userID uuid.UUID, lat, lng float64, city, country string) error {
	if m.updateCurrentLocation != nil {
		return m.updateCurrentLocation(ctx, userID, lat, lng, city, country)
	}
	return nil
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
	h := NewHandler(&mockQuerier{
		getUser: func(_ context.Context, _, userID uuid.UUID) (*User, error) {
			return &User{
				ID:                 userID,
				Username:           "testuser",
				IsPlus:             true,
				SubscriptionTier:   "plus",
				SubscriptionStatus: "active",
			}, nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/users/me", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Data User `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !body.Data.IsPlus {
		t.Fatal("expected is_plus to be true")
	}
	if body.Data.SubscriptionTier != "plus" {
		t.Fatalf("subscription_tier = %q, want plus", body.Data.SubscriptionTier)
	}
	if body.Data.SubscriptionStatus != "active" {
		t.Fatalf("subscription_status = %q, want active", body.Data.SubscriptionStatus)
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
		updateUser: func(_ context.Context, _ uuid.UUID, _, _, _, _, _ *string, _ *time.Time, _ bool, _ *time.Time, _ bool, _ []string, _ bool, _, _ *float64) error {
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
		updateUser: func(_ context.Context, _ uuid.UUID, _, _, _, _, _ *string, soberSince *time.Time, replaceSoberSince bool, _ *time.Time, _ bool, _ []string, _ bool, _, _ *float64) error {
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

func TestUpdateMeRejectsInvalidGender(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/users/me", strings.NewReader(`{"gender":"robot"}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMe(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestUpdateMePersistsGenderAndBirthDate(t *testing.T) {
	var gotGender *string
	var gotBirthDate *time.Time
	var gotReplaceBirthDate bool
	h := NewHandler(&mockQuerier{
		updateUser: func(_ context.Context, _ uuid.UUID, _, _, _, gender, _ *string, _ *time.Time, _ bool, birthDate *time.Time, replaceBirthDate bool, _ []string, _ bool, _, _ *float64) error {
			gotGender = gender
			gotBirthDate = birthDate
			gotReplaceBirthDate = replaceBirthDate
			return nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/users/me", strings.NewReader(`{"gender":"women","birth_date":"1990-05-14"}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotGender == nil || *gotGender != "woman" {
		t.Fatalf("unexpected gender value: %v", gotGender)
	}
	if !gotReplaceBirthDate {
		t.Fatal("expected birth_date replacement flag to be true")
	}
	if gotBirthDate == nil || gotBirthDate.Format("2006-01-02") != "1990-05-14" {
		t.Fatalf("unexpected birth_date value: %v", gotBirthDate)
	}
}

func TestUpdateMeRejectsInvalidBirthDate(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/users/me", strings.NewReader(`{"birth_date":"14-05-1990"}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMe(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
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

func TestDeleteMeSuccess(t *testing.T) {
	var gotUserID uuid.UUID
	h := NewHandler(&mockQuerier{
		deleteCurrentUser: func(_ context.Context, userID uuid.UUID) error {
			gotUserID = userID
			return nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodDelete, "/users/me", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.DeleteMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotUserID != fixedUser {
		t.Fatalf("userID = %s, want %s", gotUserID, fixedUser)
	}
}

func TestDeleteMeDBError(t *testing.T) {
	h := NewHandler(&mockQuerier{
		deleteCurrentUser: func(_ context.Context, _ uuid.UUID) error {
			return errors.New("db error")
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodDelete, "/users/me", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.DeleteMe(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// ── Safety ───────────────────────────────────────────────────────────────────

func TestBlockUserSuccess(t *testing.T) {
	var gotBlocker uuid.UUID
	var gotBlocked uuid.UUID
	h := NewHandler(&mockQuerier{
		blockUser: func(_ context.Context, blockerID, blockedID uuid.UUID) error {
			gotBlocker = blockerID
			gotBlocked = blockedID
			return nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPost, "/users/"+fixedOther.String()+"/block", nil), fixedUser)
	req = withURLParam(req, "id", fixedOther.String())
	rec := httptest.NewRecorder()

	h.BlockUser(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotBlocker != fixedUser || gotBlocked != fixedOther {
		t.Fatalf("block args = %s -> %s, want %s -> %s", gotBlocker, gotBlocked, fixedUser, fixedOther)
	}
}

func TestBlockUserRejectsSelf(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPost, "/users/"+fixedUser.String()+"/block", nil), fixedUser)
	req = withURLParam(req, "id", fixedUser.String())
	rec := httptest.NewRecorder()

	h.BlockUser(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListBlockedUsersSuccessPaginates(t *testing.T) {
	blockedAt := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	thirdID := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	fourthID := uuid.MustParse("00000000-0000-0000-0000-000000000004")
	h := NewHandler(&mockQuerier{
		listBlockedUsers: func(_ context.Context, userID uuid.UUID, before *BlockedUsersCursor, limit int) ([]BlockedUser, error) {
			if userID != fixedUser {
				t.Fatalf("userID = %s, want %s", userID, fixedUser)
			}
			if before != nil {
				t.Fatalf("before = %v, want nil", before)
			}
			if limit != 3 {
				t.Fatalf("limit = %d, want 3", limit)
			}
			return []BlockedUser{
				{ID: fixedOther, BlockedAt: blockedAt, User: BlockedUserProfile{ID: fixedOther, Username: "first"}},
				{ID: thirdID, BlockedAt: blockedAt.Add(-time.Minute), User: BlockedUserProfile{ID: thirdID, Username: "second"}},
				{ID: fourthID, BlockedAt: blockedAt.Add(-2 * time.Minute), User: BlockedUserProfile{ID: fourthID, Username: "third"}},
			}, nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/users/me/blocks?limit=2", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.ListBlockedUsers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Data struct {
			Items      []BlockedUser `json:"items"`
			Limit      int           `json:"limit"`
			HasMore    bool          `json:"has_more"`
			NextCursor *string       `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(body.Data.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(body.Data.Items))
	}
	if body.Data.Limit != 2 {
		t.Fatalf("limit = %d, want 2", body.Data.Limit)
	}
	if !body.Data.HasMore {
		t.Fatal("expected has_more to be true")
	}
	if body.Data.NextCursor == nil || *body.Data.NextCursor == "" {
		t.Fatal("expected next_cursor")
	}
	next, err := decodeBlockedUsersCursor(*body.Data.NextCursor)
	if err != nil {
		t.Fatalf("decode next_cursor: %v", err)
	}
	if next.BlockedID != thirdID || !next.BlockedAt.Equal(blockedAt.Add(-time.Minute)) {
		t.Fatalf("next cursor = %s %s, want %s %s", next.BlockedID, next.BlockedAt, thirdID, blockedAt.Add(-time.Minute))
	}
}

func TestListBlockedUsersPassesCursor(t *testing.T) {
	blockedAt := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	cursor := encodeBlockedUsersCursor(BlockedUser{ID: fixedOther, BlockedAt: blockedAt})
	h := NewHandler(&mockQuerier{
		listBlockedUsers: func(_ context.Context, userID uuid.UUID, before *BlockedUsersCursor, limit int) ([]BlockedUser, error) {
			if userID != fixedUser {
				t.Fatalf("userID = %s, want %s", userID, fixedUser)
			}
			if before == nil {
				t.Fatal("before = nil, want cursor")
			}
			if before.BlockedID != fixedOther || !before.BlockedAt.Equal(blockedAt) {
				t.Fatalf("before = %s %s, want %s %s", before.BlockedID, before.BlockedAt, fixedOther, blockedAt)
			}
			if limit != 26 {
				t.Fatalf("limit = %d, want 26", limit)
			}
			return nil, nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/users/me/blocks?before="+cursor, nil), fixedUser)
	rec := httptest.NewRecorder()

	h.ListBlockedUsers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestListBlockedUsersRejectsInvalidCursor(t *testing.T) {
	called := false
	h := NewHandler(&mockQuerier{
		listBlockedUsers: func(_ context.Context, _ uuid.UUID, _ *BlockedUsersCursor, _ int) ([]BlockedUser, error) {
			called = true
			return nil, nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/users/me/blocks?before=not-a-cursor", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.ListBlockedUsers(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if called {
		t.Fatal("store should not be called for invalid cursor")
	}
}

func TestReportUserSuccess(t *testing.T) {
	var gotReason string
	var gotDetails *string
	h := NewHandler(&mockQuerier{
		reportUser: func(_ context.Context, reporterID, reportedUserID uuid.UUID, reason string, details *string) error {
			if reporterID != fixedUser || reportedUserID != fixedOther {
				t.Fatalf("report args = %s -> %s, want %s -> %s", reporterID, reportedUserID, fixedUser, fixedOther)
			}
			gotReason = reason
			gotDetails = details
			return nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPost, "/users/"+fixedOther.String()+"/report", strings.NewReader(`{"reason":"unwanted_advances","details":"  crossed a boundary  "}`)), fixedUser)
	req = withURLParam(req, "id", fixedOther.String())
	rec := httptest.NewRecorder()

	h.ReportUser(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if gotReason != "unwanted_advances" {
		t.Fatalf("reason = %q, want unwanted_advances", gotReason)
	}
	if gotDetails == nil || *gotDetails != "crossed a boundary" {
		t.Fatalf("details = %v, want trimmed details", gotDetails)
	}
}

func TestReportUserRejectsInvalidReason(t *testing.T) {
	h := NewHandler(&mockQuerier{}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodPost, "/users/"+fixedOther.String()+"/report", strings.NewReader(`{"reason":"bad"}`)), fixedUser)
	req = withURLParam(req, "id", fixedOther.String())
	rec := httptest.NewRecorder()

	h.ReportUser(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
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
		discoverUsers: func(_ context.Context, _ DiscoverUsersParams) ([]User, error) {
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

func TestDiscoverParsesAdvancedFilters(t *testing.T) {
	var got DiscoverUsersParams
	h := NewHandler(&mockQuerier{
		getUser: func(_ context.Context, _, userID uuid.UUID) (*User, error) {
			return &User{
				ID:                 userID,
				Username:           "testuser",
				IsPlus:             true,
				SubscriptionTier:   "plus",
				SubscriptionStatus: "active",
			}, nil
		},
		discoverUsers: func(_ context.Context, params DiscoverUsersParams) ([]User, error) {
			got = params
			return []User{}, nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/users/discover?q=hello&gender=woman&age_min=25&age_max=40&distance_km=30&sobriety=years_1&interest=Coffee&interest=Hiking&lat=53.34&lng=-6.26", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.Discover(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got.Query != "hello" {
		t.Fatalf("query = %q, want hello", got.Query)
	}
	if got.Sobriety != "years_1" {
		t.Fatalf("sobriety = %q, want years_1", got.Sobriety)
	}
	if got.DistanceKm == nil || *got.DistanceKm != 30 {
		t.Fatalf("distanceKm = %v, want 30", got.DistanceKm)
	}
	if len(got.Interests) != 2 || got.Interests[0] != "Coffee" || got.Interests[1] != "Hiking" {
		t.Fatalf("interests = %v, want [Coffee Hiking]", got.Interests)
	}
	if got.Lat == nil || *got.Lat != 53.34 {
		t.Fatalf("lat = %v, want 53.34", got.Lat)
	}
	if got.Lng == nil || *got.Lng != -6.26 {
		t.Fatalf("lng = %v, want -6.26", got.Lng)
	}
}

func TestDiscoverParsesAdvancedFiltersWithoutPlus(t *testing.T) {
	var got DiscoverUsersParams
	h := NewHandler(&mockQuerier{
		getUser: func(_ context.Context, _, userID uuid.UUID) (*User, error) {
			return &User{
				ID:                 userID,
				Username:           "testuser",
				SubscriptionTier:   "free",
				SubscriptionStatus: "inactive",
			}, nil
		},
		discoverUsers: func(_ context.Context, params DiscoverUsersParams) ([]User, error) {
			got = params
			return []User{}, nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/users/discover?q=hello&gender=woman&age_min=25&age_max=40&distance_km=30&sobriety=years_1&interest=Coffee&lat=53.34&lng=-6.26", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.Discover(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got.Query != "hello" {
		t.Fatalf("query = %q, want hello", got.Query)
	}
	if got.Sobriety != "years_1" {
		t.Fatalf("sobriety = %q, want years_1", got.Sobriety)
	}
	if got.DistanceKm == nil || *got.DistanceKm != 30 {
		t.Fatalf("distanceKm = %v, want 30", got.DistanceKm)
	}
	if len(got.Interests) != 1 || got.Interests[0] != "Coffee" {
		t.Fatalf("interests = %v, want [Coffee]", got.Interests)
	}
	if got.Lat == nil || *got.Lat != 53.34 {
		t.Fatalf("lat = %v, want 53.34", got.Lat)
	}
	if got.Lng == nil || *got.Lng != -6.26 {
		t.Fatalf("lng = %v, want -6.26", got.Lng)
	}
}

func TestDiscoverAcceptsButDoesNotApplyDatingLikeFilterParams(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getUser: func(_ context.Context, _, userID uuid.UUID) (*User, error) {
			return &User{
				ID:                 userID,
				Username:           "testuser",
				IsPlus:             true,
				SubscriptionTier:   "plus",
				SubscriptionStatus: "active",
			}, nil
		},
		discoverUsers: func(_ context.Context, _ DiscoverUsersParams) ([]User, error) {
			return []User{}, nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/users/discover?gender=robot&age_min=40&age_max=25", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.Discover(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestDiscoverPreviewBuildsBroadenedResponse(t *testing.T) {
	h := NewHandler(&mockQuerier{
		countDiscoverUsers: func(_ context.Context, params DiscoverUsersParams) (int, error) {
			if len(params.Interests) > 0 || params.Sobriety != "" {
				return 0, nil
			}
			return 14, nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/users/discover/preview?gender=woman&distance_km=25&sobriety=years_1&interest=Coffee", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.DiscoverPreview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Data DiscoverPreviewResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Data.ExactCount != 0 {
		t.Fatalf("exact_count = %d, want 0", body.Data.ExactCount)
	}
	if !body.Data.BroadenedAvailable {
		t.Fatalf("broadened_available = false, want true")
	}
	if body.Data.BroadenedCount == nil || *body.Data.BroadenedCount != 14 {
		t.Fatalf("broadened_count = %v, want 14", body.Data.BroadenedCount)
	}
	if len(body.Data.RelaxedFilters) == 0 {
		t.Fatalf("relaxed_filters = %v, want non-empty", body.Data.RelaxedFilters)
	}
	if body.Data.EffectiveFilters.Sobriety != "" {
		t.Fatalf("effective sobriety = %q, want empty after broadening", body.Data.EffectiveFilters.Sobriety)
	}
	if len(body.Data.EffectiveFilters.Interests) != 0 {
		t.Fatalf("effective interests = %v, want empty after broadening", body.Data.EffectiveFilters.Interests)
	}
}

func TestDiscoverPreviewKeepsExactMatchesExact(t *testing.T) {
	h := NewHandler(&mockQuerier{
		countDiscoverUsers: func(_ context.Context, params DiscoverUsersParams) (int, error) {
			if len(params.Interests) > 0 || params.Sobriety != "" {
				return 1, nil
			}
			return 12, nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(http.MethodGet, "/users/discover/preview?distance_km=25&sobriety=years_1&interest=Coffee", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.DiscoverPreview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Data DiscoverPreviewResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Data.ExactCount != 1 {
		t.Fatalf("exact_count = %d, want 1", body.Data.ExactCount)
	}
	if body.Data.BroadenedAvailable {
		t.Fatalf("broadened_available = true, want false")
	}
	if body.Data.BroadenedCount != nil {
		t.Fatalf("broadened_count = %v, want nil", body.Data.BroadenedCount)
	}
	if len(body.Data.RelaxedFilters) != 0 {
		t.Fatalf("relaxed_filters = %v, want empty", body.Data.RelaxedFilters)
	}
}

func TestUpdateMyCurrentLocationPassesTownAndCountry(t *testing.T) {
	h := NewHandler(&mockQuerier{
		updateCurrentLocation: func(_ context.Context, userID uuid.UUID, lat, lng float64, city, country string) error {
			if userID != fixedUser {
				t.Fatalf("userID = %s, want %s", userID, fixedUser)
			}
			if lat != 51.5074 || lng != -0.1278 {
				t.Fatalf("lat/lng = %f/%f, want London coords", lat, lng)
			}
			if city != "London" || country != "United Kingdom" {
				t.Fatalf("city/country = %q/%q, want London/United Kingdom", city, country)
			}
			return nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(
		http.MethodPatch,
		"/users/me/location",
		strings.NewReader(`{"lat":51.5074,"lng":-0.1278,"city":"London","country":"United Kingdom"}`),
	), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMyCurrentLocation(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestUpdateMyCurrentLocationTrimsAndNormalizesCountryCode(t *testing.T) {
	h := NewHandler(&mockQuerier{
		updateCurrentLocation: func(_ context.Context, _ uuid.UUID, _ float64, _ float64, city, country string) error {
			if city != "Greater London" || country != "United Kingdom" {
				t.Fatalf("city/country = %q/%q, want Greater London/United Kingdom", city, country)
			}
			return nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(
		http.MethodPatch,
		"/users/me/location",
		strings.NewReader(`{"lat":51.5035,"lng":-0.3322,"city":" Greater London ","country":"gb"}`),
	), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMyCurrentLocation(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestUpdateMyCurrentLocationRejectsBlankPlaceParts(t *testing.T) {
	h := NewHandler(&mockQuerier{
		updateCurrentLocation: func(_ context.Context, _ uuid.UUID, _ float64, _ float64, _, _ string) error {
			t.Fatal("UpdateCurrentLocation should not be called")
			return nil
		},
	}, &mockUploader{})
	req := withUserID(httptest.NewRequest(
		http.MethodPatch,
		"/users/me/location",
		strings.NewReader(`{"lat":51.5035,"lng":-0.3322,"city":"Greater London","country":" "}`),
	), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMyCurrentLocation(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}
