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
	updateAvatarURL         func(ctx context.Context, userID uuid.UUID, avatarURL string) error
	updateBannerURL         func(ctx context.Context, userID uuid.UUID, bannerURL string) error
	discoverUsers           func(ctx context.Context, params DiscoverUsersParams) ([]User, error)
	countDiscoverUsers      func(ctx context.Context, params DiscoverUsersParams) (int, error)
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
func (m *mockQuerier) UpdateUser(ctx context.Context, userID uuid.UUID, username, city, country, gender, bio *string, soberSince *time.Time, replaceSoberSince bool, birthDate *time.Time, replaceBirthDate bool, interests []string, replaceInterests bool, lat, lng *float64) error {
	if m.updateUser != nil {
		return m.updateUser(ctx, userID, username, city, country, gender, bio, soberSince, replaceSoberSince, birthDate, replaceBirthDate, interests, replaceInterests, lat, lng)
	}
	return nil
}
func (m *mockQuerier) UpdateAvatarURL(ctx context.Context, userID uuid.UUID, avatarURL string) error {
	if m.updateAvatarURL != nil {
		return m.updateAvatarURL(ctx, userID, avatarURL)
	}
	return nil
}
func (m *mockQuerier) UpdateBannerURL(ctx context.Context, userID uuid.UUID, bannerURL string) error {
	if m.updateBannerURL != nil {
		return m.updateBannerURL(ctx, userID, bannerURL)
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

func (m *mockQuerier) ListInterests(ctx context.Context) ([]string, error) {
	if m.listInterests != nil {
		return m.listInterests(ctx)
	}
	return []string{"Coffee", "Hiking", "Meditation"}, nil
}

func (m *mockQuerier) UpdateCurrentLocation(ctx context.Context, userID uuid.UUID, lat, lng float64, city string) error {
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
	if got.Gender != "woman" {
		t.Fatalf("gender = %q, want woman", got.Gender)
	}
	if got.Sobriety != "years_1" {
		t.Fatalf("sobriety = %q, want years_1", got.Sobriety)
	}
	if got.AgeMin == nil || *got.AgeMin != 25 {
		t.Fatalf("ageMin = %v, want 25", got.AgeMin)
	}
	if got.AgeMax == nil || *got.AgeMax != 40 {
		t.Fatalf("ageMax = %v, want 40", got.AgeMax)
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

func TestDiscoverIgnoresAdvancedFiltersWithoutPlus(t *testing.T) {
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
	req := withUserID(httptest.NewRequest(http.MethodGet, "/users/discover?q=hello&gender=robot&age_min=40&age_max=25&distance_km=30&sobriety=not-real&lat=53.34&lng=-6.26", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.Discover(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got.Query != "hello" {
		t.Fatalf("query = %q, want hello", got.Query)
	}
	if got.Gender != "" {
		t.Fatalf("gender = %q, want empty", got.Gender)
	}
	if got.Sobriety != "" {
		t.Fatalf("sobriety = %q, want empty", got.Sobriety)
	}
	if got.AgeMin != nil {
		t.Fatalf("ageMin = %v, want nil", got.AgeMin)
	}
	if got.AgeMax != nil {
		t.Fatalf("ageMax = %v, want nil", got.AgeMax)
	}
	if got.DistanceKm != nil {
		t.Fatalf("distanceKm = %v, want nil", got.DistanceKm)
	}
	if got.Lat == nil || *got.Lat != 53.34 {
		t.Fatalf("lat = %v, want 53.34", got.Lat)
	}
	if got.Lng == nil || *got.Lng != -6.26 {
		t.Fatalf("lng = %v, want -6.26", got.Lng)
	}
}

func TestDiscoverRejectsInvalidAgeRange(t *testing.T) {
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
	req := withUserID(httptest.NewRequest(http.MethodGet, "/users/discover?age_min=40&age_max=25", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.Discover(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
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
