package meetups

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/middleware"
)

type mockUploader struct {
	upload func(ctx context.Context, key, contentType string, body io.Reader) (string, error)
}

func (m *mockUploader) Upload(ctx context.Context, key, contentType string, body io.Reader) (string, error) {
	if m.upload != nil {
		return m.upload(ctx, key, contentType, body)
	}
	return "https://example.com/" + key, nil
}

type mockQuerier struct {
	listCategories  func(ctx context.Context) ([]MeetupCategory, error)
	discoverMeetups func(ctx context.Context, userID uuid.UUID, params DiscoverMeetupsParams) (*CursorPage[Meetup], error)
	listMyMeetups   func(ctx context.Context, userID uuid.UUID, params MyMeetupsParams) (*CursorPage[Meetup], error)
	getMeetup       func(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error)
	createMeetup    func(ctx context.Context, userID uuid.UUID, input CreateMeetupInput) (*Meetup, error)
	updateMeetup    func(ctx context.Context, meetupID, userID uuid.UUID, input UpdateMeetupInput) (*Meetup, error)
	publishMeetup   func(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error)
	cancelMeetup    func(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error)
	deleteMeetup    func(ctx context.Context, meetupID, userID uuid.UUID) error
	getAttendees    func(ctx context.Context, meetupID uuid.UUID, limit, offset int) ([]Attendee, error)
	getWaitlist     func(ctx context.Context, meetupID, userID uuid.UUID, limit, offset int) ([]Attendee, error)
	toggleRSVP      func(ctx context.Context, meetupID, userID uuid.UUID) (*RSVPResult, error)
}

func (m *mockQuerier) ListCategories(ctx context.Context) ([]MeetupCategory, error) {
	if m.listCategories != nil {
		return m.listCategories(ctx)
	}
	return []MeetupCategory{{Slug: "coffee", Label: "Coffee", SortOrder: 1}}, nil
}

func (m *mockQuerier) DiscoverMeetups(ctx context.Context, userID uuid.UUID, params DiscoverMeetupsParams) (*CursorPage[Meetup], error) {
	if m.discoverMeetups != nil {
		return m.discoverMeetups(ctx, userID, params)
	}
	return &CursorPage[Meetup]{Items: []Meetup{}, Limit: params.Limit}, nil
}

func (m *mockQuerier) ListMyMeetups(ctx context.Context, userID uuid.UUID, params MyMeetupsParams) (*CursorPage[Meetup], error) {
	if m.listMyMeetups != nil {
		return m.listMyMeetups(ctx, userID, params)
	}
	return &CursorPage[Meetup]{Items: []Meetup{}, Limit: params.Limit}, nil
}

func (m *mockQuerier) GetMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error) {
	if m.getMeetup != nil {
		return m.getMeetup(ctx, meetupID, userID)
	}
	return &Meetup{ID: meetupID, OrganizerID: userID, Title: "Coffee", CategorySlug: "coffee", CategoryLabel: "Coffee", City: "Dublin", StartsAt: time.Now().UTC()}, nil
}

func (m *mockQuerier) CreateMeetup(ctx context.Context, userID uuid.UUID, input CreateMeetupInput) (*Meetup, error) {
	if m.createMeetup != nil {
		return m.createMeetup(ctx, userID, input)
	}
	return &Meetup{ID: uuid.New(), OrganizerID: userID, Title: input.Title, CategorySlug: input.CategorySlug, CategoryLabel: "Coffee", City: input.City, StartsAt: input.StartsAt}, nil
}

func (m *mockQuerier) UpdateMeetup(ctx context.Context, meetupID, userID uuid.UUID, input UpdateMeetupInput) (*Meetup, error) {
	if m.updateMeetup != nil {
		return m.updateMeetup(ctx, meetupID, userID, input)
	}
	return &Meetup{ID: meetupID, OrganizerID: userID, Title: input.Title, CategorySlug: input.CategorySlug, CategoryLabel: "Coffee", City: input.City, StartsAt: input.StartsAt}, nil
}

func (m *mockQuerier) PublishMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error) {
	if m.publishMeetup != nil {
		return m.publishMeetup(ctx, meetupID, userID)
	}
	return &Meetup{ID: meetupID, OrganizerID: userID, Status: "published"}, nil
}

func (m *mockQuerier) CancelMeetup(ctx context.Context, meetupID, userID uuid.UUID) (*Meetup, error) {
	if m.cancelMeetup != nil {
		return m.cancelMeetup(ctx, meetupID, userID)
	}
	return &Meetup{ID: meetupID, OrganizerID: userID, Status: "cancelled"}, nil
}

func (m *mockQuerier) DeleteMeetup(ctx context.Context, meetupID, userID uuid.UUID) error {
	if m.deleteMeetup != nil {
		return m.deleteMeetup(ctx, meetupID, userID)
	}
	return nil
}

func (m *mockQuerier) GetAttendees(ctx context.Context, meetupID uuid.UUID, limit, offset int) ([]Attendee, error) {
	if m.getAttendees != nil {
		return m.getAttendees(ctx, meetupID, limit, offset)
	}
	return nil, nil
}

func (m *mockQuerier) GetWaitlist(ctx context.Context, meetupID, userID uuid.UUID, limit, offset int) ([]Attendee, error) {
	if m.getWaitlist != nil {
		return m.getWaitlist(ctx, meetupID, userID, limit, offset)
	}
	return nil, nil
}

func (m *mockQuerier) ToggleRSVP(ctx context.Context, meetupID, userID uuid.UUID) (*RSVPResult, error) {
	if m.toggleRSVP != nil {
		return m.toggleRSVP(ctx, meetupID, userID)
	}
	return &RSVPResult{State: "going", Attending: true, AttendeeCount: 1}, nil
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

func validCreateBody() string {
	return `{"title":"Coffee Walk","category_slug":"coffee","event_type":"in_person","status":"published","visibility":"public","city":"Dublin","starts_at":"2026-05-01T19:00:00Z"}`
}

func TestListCategoriesSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.ListCategories(rec, httptest.NewRequest(http.MethodGet, "/meetups/categories", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestListMeetupsSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.ListMeetups(rec, authedRequest(http.MethodGet, "/meetups", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestListMyMeetupsRejectsInvalidScope(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := authedRequest(http.MethodGet, "/users/me/meetups?scope=nope", "")
	rec := httptest.NewRecorder()
	h.ListMyMeetups(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

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

func TestCreateMeetupValidationFlow(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateMeetup(rec, authedRequest(http.MethodPost, "/meetups", `{"title":"","category_slug":"","city":"","starts_at":""}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCreateMeetupRejectsInvalidStartsAt(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateMeetup(rec, authedRequest(http.MethodPost, "/meetups", `{"title":"Coffee","category_slug":"coffee","event_type":"in_person","status":"published","visibility":"public","city":"Dublin","starts_at":"tomorrow"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateMeetupRejectsUnknownCategory(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateMeetup(rec, authedRequest(http.MethodPost, "/meetups", `{"title":"Coffee","category_slug":"not-real","event_type":"in_person","status":"published","visibility":"public","city":"Dublin","starts_at":"2026-05-01T19:00:00Z"}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCreateMeetupSuccess(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	rec := httptest.NewRecorder()
	h.CreateMeetup(rec, authedRequest(http.MethodPost, "/meetups", validCreateBody()))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestCreateMeetupParsesCoHosts(t *testing.T) {
	coHostID := uuid.New()
	h := NewHandler(&mockQuerier{
		createMeetup: func(_ context.Context, userID uuid.UUID, input CreateMeetupInput) (*Meetup, error) {
			if len(input.CoHostIDs) != 1 || input.CoHostIDs[0] != coHostID {
				t.Fatalf("co-host ids = %v, want [%s]", input.CoHostIDs, coHostID)
			}
			return &Meetup{ID: uuid.New(), OrganizerID: userID, Title: input.Title, CategorySlug: input.CategorySlug, CategoryLabel: "Coffee", City: input.City, StartsAt: input.StartsAt}, nil
		},
	})
	body := `{"title":"Coffee Walk","category_slug":"coffee","event_type":"in_person","status":"published","visibility":"public","city":"Dublin","starts_at":"2026-05-01T19:00:00Z","co_host_ids":["` + coHostID.String() + `"]}`
	rec := httptest.NewRecorder()
	h.CreateMeetup(rec, authedRequest(http.MethodPost, "/meetups", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestCreateMeetupRejectsInvalidCoHostID(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	body := `{"title":"Coffee Walk","category_slug":"coffee","event_type":"in_person","status":"published","visibility":"public","city":"Dublin","starts_at":"2026-05-01T19:00:00Z","co_host_ids":["bad-id"]}`
	rec := httptest.NewRecorder()
	h.CreateMeetup(rec, authedRequest(http.MethodPost, "/meetups", body))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestUploadCoverImageSuccess(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("cover", "cover.png")
	if err != nil {
		t.Fatalf("CreateFormFile error = %v", err)
	}

	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{B: 255, A: 255})
	if err := png.Encode(part, img); err != nil {
		t.Fatalf("png.Encode error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close error = %v", err)
	}

	var uploadedKeys []string
	h := NewHandler(&mockQuerier{}, &mockUploader{
		upload: func(_ context.Context, key, _ string, _ io.Reader) (string, error) {
			uploadedKeys = append(uploadedKeys, key)
			return "https://example.com/" + key, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/meetups/images", &body)
	req = withUserID(req, fixedUser)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	h.UploadCoverImage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(uploadedKeys) != 1 {
		t.Fatalf("upload count = %d, want 1", len(uploadedKeys))
	}
}

func TestUpdateMeetupForbidden(t *testing.T) {
	h := NewHandler(&mockQuerier{
		updateMeetup: func(_ context.Context, _, _ uuid.UUID, _ UpdateMeetupInput) (*Meetup, error) {
			return nil, ErrForbidden
		},
	})
	req := authedRequest(http.MethodPatch, "/", validCreateBody())
	req = withURLParam(req, "id", fixedMeetup.String())
	rec := httptest.NewRecorder()
	h.UpdateMeetup(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestUpdateMeetupRejectsInvalidTransition(t *testing.T) {
	h := NewHandler(&mockQuerier{
		updateMeetup: func(_ context.Context, _, _ uuid.UUID, _ UpdateMeetupInput) (*Meetup, error) {
			return nil, ErrInvalidTransition
		},
	})
	req := authedRequest(http.MethodPatch, "/", validCreateBody())
	req = withURLParam(req, "id", fixedMeetup.String())
	rec := httptest.NewRecorder()
	h.UpdateMeetup(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestPublishMeetupNotFound(t *testing.T) {
	h := NewHandler(&mockQuerier{
		publishMeetup: func(_ context.Context, _, _ uuid.UUID) (*Meetup, error) {
			return nil, ErrNotFound
		},
	})
	req := authedRequest(http.MethodPost, "/", "")
	req = withURLParam(req, "id", fixedMeetup.String())
	rec := httptest.NewRecorder()
	h.PublishMeetup(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestRSVPTogglesState(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	req := authedRequest(http.MethodPost, "/", "")
	req = withURLParam(req, "id", fixedMeetup.String())
	rec := httptest.NewRecorder()
	h.RSVP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRSVPAtCapacity(t *testing.T) {
	h := NewHandler(&mockQuerier{
		toggleRSVP: func(_ context.Context, _, _ uuid.UUID) (*RSVPResult, error) {
			return nil, ErrCapacityReached
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

func TestGetWaitlistForbidden(t *testing.T) {
	h := NewHandler(&mockQuerier{
		getWaitlist: func(_ context.Context, _, _ uuid.UUID, _, _ int) ([]Attendee, error) {
			return nil, ErrForbidden
		},
	})
	req := authedRequest(http.MethodGet, "/", "")
	req = withURLParam(req, "id", fixedMeetup.String())
	rec := httptest.NewRecorder()
	h.GetWaitlist(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestDeleteMeetupConflict(t *testing.T) {
	h := NewHandler(&mockQuerier{
		deleteMeetup: func(_ context.Context, _, _ uuid.UUID) error {
			return ErrDeleteNotAllowed
		},
	})
	req := authedRequest(http.MethodDelete, "/", "")
	req = withURLParam(req, "id", fixedMeetup.String())
	rec := httptest.NewRecorder()
	h.DeleteMeetup(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestListCategoriesError(t *testing.T) {
	h := NewHandler(&mockQuerier{
		listCategories: func(_ context.Context) ([]MeetupCategory, error) {
			return nil, errors.New("db error")
		},
	})
	rec := httptest.NewRecorder()
	h.ListCategories(rec, httptest.NewRequest(http.MethodGet, "/meetups/categories", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
