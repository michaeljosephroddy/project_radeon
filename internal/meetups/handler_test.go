package meetups

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
	listMeetups            func(ctx context.Context, userID uuid.UUID, cityFilter, queryFilter string, limit, offset int) ([]Meetup, error)
	listMyMeetups          func(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Meetup, error)
	attachAttendeePreviews func(ctx context.Context, meetups []Meetup, previewLimit int) error
	getMeetup              func(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error)
	createMeetup           func(ctx context.Context, userID uuid.UUID, title string, description *string, city string, startsAt time.Time, capacity *int) (*Meetup, error)
	getAttendees           func(ctx context.Context, meetupID uuid.UUID, limit, offset int) ([]Attendee, error)
	getMeetupCapacity      func(ctx context.Context, meetupID uuid.UUID) (*int, int, error)
	isRSVPd                func(ctx context.Context, meetupID, userID uuid.UUID) (bool, error)
	addRSVP                func(ctx context.Context, meetupID, userID uuid.UUID) error
	removeRSVP             func(ctx context.Context, meetupID, userID uuid.UUID) error
}

func (m *mockQuerier) ListMeetups(ctx context.Context, userID uuid.UUID, cf, qf string, limit, offset int) ([]Meetup, error) {
	if m.listMeetups != nil {
		return m.listMeetups(ctx, userID, cf, qf, limit, offset)
	}
	return nil, nil
}
func (m *mockQuerier) ListMyMeetups(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Meetup, error) {
	if m.listMyMeetups != nil {
		return m.listMyMeetups(ctx, userID, limit, offset)
	}
	return nil, nil
}
func (m *mockQuerier) AttachAttendeePreviews(ctx context.Context, meetups []Meetup, previewLimit int) error {
	if m.attachAttendeePreviews != nil {
		return m.attachAttendeePreviews(ctx, meetups, previewLimit)
	}
	return nil
}
func (m *mockQuerier) GetMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error) {
	if m.getMeetup != nil {
		return m.getMeetup(ctx, meetupID, userID)
	}
	return &Meetup{ID: meetupID}, nil
}
func (m *mockQuerier) CreateMeetup(ctx context.Context, userID uuid.UUID, title string, description *string, city string, startsAt time.Time, capacity *int) (*Meetup, error) {
	if m.createMeetup != nil {
		return m.createMeetup(ctx, userID, title, description, city, startsAt, capacity)
	}
	return &Meetup{ID: uuid.New(), OrganizerID: userID, Title: title, City: city, StartsAt: startsAt}, nil
}
func (m *mockQuerier) GetAttendees(ctx context.Context, meetupID uuid.UUID, limit, offset int) ([]Attendee, error) {
	if m.getAttendees != nil {
		return m.getAttendees(ctx, meetupID, limit, offset)
	}
	return nil, nil
}
func (m *mockQuerier) GetMeetupCapacity(ctx context.Context, meetupID uuid.UUID) (*int, int, error) {
	if m.getMeetupCapacity != nil {
		return m.getMeetupCapacity(ctx, meetupID)
	}
	return nil, 0, nil
}
func (m *mockQuerier) IsRSVPd(ctx context.Context, meetupID, userID uuid.UUID) (bool, error) {
	if m.isRSVPd != nil {
		return m.isRSVPd(ctx, meetupID, userID)
	}
	return false, nil
}
func (m *mockQuerier) AddRSVP(ctx context.Context, meetupID, userID uuid.UUID) error {
	if m.addRSVP != nil {
		return m.addRSVP(ctx, meetupID, userID)
	}
	return nil
}
func (m *mockQuerier) RemoveRSVP(ctx context.Context, meetupID, userID uuid.UUID) error {
	if m.removeRSVP != nil {
		return m.removeRSVP(ctx, meetupID, userID)
	}
	return nil
}

var (
	fixedUser   = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	fixedMeetup = uuid.MustParse("00000000-0000-0000-0000-000000000002")
)

func withUserID(r *http.Request, id uuid.UUID) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.UserIDKey, id))
}

func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func authedRequest(method, target, body string) *http.Request {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	return withUserID(req, fixedUser)
}

// ── ListMeetups ───────────────────────────────────────────────────────────────

func TestListMeetupsSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.ListMeetups(rec, authedRequest(http.MethodGet, "/meetups", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestListMeetupsDBError(t *testing.T) {
	h := NewHandler(&mockQuerier{
		listMeetups: func(_ context.Context, _ uuid.UUID, _, _ string, _, _ int) ([]Meetup, error) {
			return nil, errors.New("db error")
		},
	})
	rec := httptest.NewRecorder()
	h.ListMeetups(rec, authedRequest(http.MethodGet, "/meetups", ""))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// ── GetMeetup ─────────────────────────────────────────────────────────────────

func TestGetMeetupRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := authedRequest(http.MethodGet, "/meetups/bad", "")
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()
	h.GetMeetup(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetMeetupNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getMeetup: func(_ context.Context, _, _ uuid.UUID) (*Meetup, error) {
			return nil, ErrNotFound
		},
	})
	req := authedRequest(http.MethodGet, "/", "")
	req = withURLParam(req, "id", fixedMeetup.String())
	rec := httptest.NewRecorder()
	h.GetMeetup(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGetMeetupSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := authedRequest(http.MethodGet, "/", "")
	req = withURLParam(req, "id", fixedMeetup.String())
	rec := httptest.NewRecorder()
	h.GetMeetup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// ── CreateMeetup ──────────────────────────────────────────────────────────────

func TestCreateMeetupValidationFlow(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateMeetup(rec, authedRequest(http.MethodPost, "/meetups", `{"title":"","city":"","starts_at":""}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCreateMeetupRejectsInvalidStartsAt(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateMeetup(rec, authedRequest(http.MethodPost, "/meetups", `{"title":"Coffee","city":"Dublin","starts_at":"tomorrow"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateMeetupRejectsInvalidCapacity(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateMeetup(rec, authedRequest(http.MethodPost, "/meetups", `{"title":"Coffee","city":"Dublin","starts_at":"2026-04-19T18:00:00Z","capacity":0}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCreateMeetupSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateMeetup(rec, authedRequest(http.MethodPost, "/meetups", `{"title":"Coffee","city":"Dublin","starts_at":"2026-04-19T18:00:00Z"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestCreateMeetupDBError(t *testing.T) {
	h := NewHandler(&mockQuerier{
		createMeetup: func(_ context.Context, _ uuid.UUID, _ string, _ *string, _ string, _ time.Time, _ *int) (*Meetup, error) {
			return nil, errors.New("db error")
		},
	})
	rec := httptest.NewRecorder()
	h.CreateMeetup(rec, authedRequest(http.MethodPost, "/meetups", `{"title":"Coffee","city":"Dublin","starts_at":"2026-04-19T18:00:00Z"}`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// ── RSVP ──────────────────────────────────────────────────────────────────────

func TestRSVPRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := authedRequest(http.MethodPost, "/", "")
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()
	h.RSVP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRSVPMeetupNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getMeetupCapacity: func(_ context.Context, _ uuid.UUID) (*int, int, error) {
			return nil, 0, ErrNotFound
		},
	})
	req := authedRequest(http.MethodPost, "/", "")
	req = withURLParam(req, "id", fixedMeetup.String())
	rec := httptest.NewRecorder()
	h.RSVP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestRSVPAtCapacity(t *testing.T) {
	cap := 5
	h := NewHandler(&mockQuerier{
		getMeetupCapacity: func(_ context.Context, _ uuid.UUID) (*int, int, error) {
			return &cap, 5, nil
		},
	})
	req := authedRequest(http.MethodPost, "/", "")
	req = withURLParam(req, "id", fixedMeetup.String())
	rec := httptest.NewRecorder()
	h.RSVP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestRSVPTogglesOn(t *testing.T) {
	added := false
	h := NewHandler(&mockQuerier{
		isRSVPd: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return false, nil },
		addRSVP: func(_ context.Context, _, _ uuid.UUID) error { added = true; return nil },
	})
	req := authedRequest(http.MethodPost, "/", "")
	req = withURLParam(req, "id", fixedMeetup.String())
	rec := httptest.NewRecorder()
	h.RSVP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !added {
		t.Fatal("expected AddRSVP to be called")
	}
}

func TestRSVPTogglesOff(t *testing.T) {
	removed := false
	h := NewHandler(&mockQuerier{
		isRSVPd:    func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		removeRSVP: func(_ context.Context, _, _ uuid.UUID) error { removed = true; return nil },
	})
	req := authedRequest(http.MethodPost, "/", "")
	req = withURLParam(req, "id", fixedMeetup.String())
	rec := httptest.NewRecorder()
	h.RSVP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !removed {
		t.Fatal("expected RemoveRSVP to be called")
	}
}

// ── GetAttendees ──────────────────────────────────────────────────────────────

func TestGetAttendeesRejectsInvalidUUID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withURLParam(req, "id", "bad")
	rec := httptest.NewRecorder()
	h.GetAttendees(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetAttendeesSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withURLParam(req, "id", fixedMeetup.String())
	rec := httptest.NewRecorder()
	h.GetAttendees(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
