package reflections

import (
	"context"
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

type mockStore struct {
	getToday func(ctx context.Context, userID uuid.UUID, today time.Time) (*DailyReflection, error)
	upsert   func(ctx context.Context, userID uuid.UUID, today time.Time, input UpsertDailyReflectionInput) (*DailyReflection, error)
	list     func(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]DailyReflection, error)
	get      func(ctx context.Context, userID, reflectionID uuid.UUID) (*DailyReflection, error)
	update   func(ctx context.Context, userID, reflectionID uuid.UUID, input UpdateDailyReflectionInput) (*DailyReflection, error)
	delete   func(ctx context.Context, userID, reflectionID uuid.UUID) error
	share    func(ctx context.Context, userID, reflectionID uuid.UUID) (uuid.UUID, error)
}

func (m *mockStore) GetTodayReflection(ctx context.Context, userID uuid.UUID, today time.Time) (*DailyReflection, error) {
	if m.getToday != nil {
		return m.getToday(ctx, userID, today)
	}
	return nil, nil
}

func (m *mockStore) UpsertTodayReflection(ctx context.Context, userID uuid.UUID, today time.Time, input UpsertDailyReflectionInput) (*DailyReflection, error) {
	if m.upsert != nil {
		return m.upsert(ctx, userID, today, input)
	}
	return &DailyReflection{ID: uuid.New(), UserID: userID, ReflectionDate: "2026-05-01", Body: input.Body}, nil
}

func (m *mockStore) ListReflections(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]DailyReflection, error) {
	if m.list != nil {
		return m.list(ctx, userID, before, limit)
	}
	return nil, nil
}

func (m *mockStore) GetReflection(ctx context.Context, userID, reflectionID uuid.UUID) (*DailyReflection, error) {
	if m.get != nil {
		return m.get(ctx, userID, reflectionID)
	}
	return &DailyReflection{ID: reflectionID, UserID: userID, ReflectionDate: "2026-05-01", Body: "steady"}, nil
}

func (m *mockStore) UpdateReflection(ctx context.Context, userID, reflectionID uuid.UUID, input UpdateDailyReflectionInput) (*DailyReflection, error) {
	if m.update != nil {
		return m.update(ctx, userID, reflectionID, input)
	}
	return &DailyReflection{ID: reflectionID, UserID: userID, ReflectionDate: "2026-05-01", Body: "steady"}, nil
}

func (m *mockStore) DeleteReflection(ctx context.Context, userID, reflectionID uuid.UUID) error {
	if m.delete != nil {
		return m.delete(ctx, userID, reflectionID)
	}
	return nil
}

func (m *mockStore) ShareReflection(ctx context.Context, userID, reflectionID uuid.UUID) (uuid.UUID, error) {
	if m.share != nil {
		return m.share(ctx, userID, reflectionID)
	}
	return uuid.New(), nil
}

var (
	testUser       = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	testReflection = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	testPost       = uuid.MustParse("00000000-0000-0000-0000-000000000003")
)

func TestUpsertTodayReflectionTrimsAndValidates(t *testing.T) {
	var gotInput UpsertDailyReflectionInput
	h := NewHandler(&mockStore{
		upsert: func(_ context.Context, userID uuid.UUID, _ time.Time, input UpsertDailyReflectionInput) (*DailyReflection, error) {
			if userID != testUser {
				t.Fatalf("userID = %s, want %s", userID, testUser)
			}
			gotInput = input
			return &DailyReflection{ID: testReflection, UserID: userID, ReflectionDate: "2026-05-01", Body: input.Body}, nil
		},
	})
	h.now = func() time.Time { return time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC) }

	rec := httptest.NewRecorder()
	req := withUserID(httptest.NewRequest(http.MethodPut, "/reflections/today", strings.NewReader(`{"body":"  stayed grounded  "}`)), testUser)
	h.UpsertTodayReflection(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotInput.Body != "stayed grounded" {
		t.Fatalf("body = %q, want trimmed body", gotInput.Body)
	}
}

func TestUpsertTodayReflectionRejectsEmptyBody(t *testing.T) {
	h := NewHandler(&mockStore{})

	rec := httptest.NewRecorder()
	req := withUserID(httptest.NewRequest(http.MethodPut, "/reflections/today", strings.NewReader(`{"body":"   "}`)), testUser)
	h.UpsertTodayReflection(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestListReflectionsUsesDateCursor(t *testing.T) {
	var gotBefore *time.Time
	var gotLimit int
	h := NewHandler(&mockStore{
		list: func(_ context.Context, _ uuid.UUID, before *time.Time, limit int) ([]DailyReflection, error) {
			gotBefore = before
			gotLimit = limit
			return []DailyReflection{
				{ID: testReflection, UserID: testUser, ReflectionDate: "2026-04-30", Body: "one"},
				{ID: uuid.New(), UserID: testUser, ReflectionDate: "2026-04-29", Body: "two"},
			}, nil
		},
	})

	rec := httptest.NewRecorder()
	req := withUserID(httptest.NewRequest(http.MethodGet, "/reflections?before=2026-05-01&limit=1", nil), testUser)
	h.ListReflections(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotBefore == nil || gotBefore.Format("2006-01-02") != "2026-05-01" {
		t.Fatalf("before = %v, want 2026-05-01", gotBefore)
	}
	if gotLimit != 2 {
		t.Fatalf("limit = %d, want 2", gotLimit)
	}

	var body struct {
		Data ListResponse `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Data.HasMore || body.Data.NextCursor == nil || *body.Data.NextCursor != "2026-04-30" {
		t.Fatalf("unexpected cursor response: %+v", body.Data)
	}
}

func TestShareReflectionReturnsPostID(t *testing.T) {
	h := NewHandler(&mockStore{
		share: func(_ context.Context, userID, reflectionID uuid.UUID) (uuid.UUID, error) {
			if userID != testUser {
				t.Fatalf("userID = %s, want %s", userID, testUser)
			}
			if reflectionID != testReflection {
				t.Fatalf("reflectionID = %s, want %s", reflectionID, testReflection)
			}
			return testPost, nil
		},
	})

	rec := httptest.NewRecorder()
	req := withUserID(withURLParam(httptest.NewRequest(http.MethodPost, "/reflections/"+testReflection.String()+"/share", nil), "id", testReflection.String()), testUser)
	h.ShareReflection(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), testPost.String()) {
		t.Fatalf("response body %q does not contain post id", rec.Body.String())
	}
}

func withUserID(r *http.Request, id uuid.UUID) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.UserIDKey, id))
}

func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}
