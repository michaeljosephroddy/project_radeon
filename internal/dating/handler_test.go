package dating

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
	"github.com/project_radeon/api/internal/user"
	"github.com/project_radeon/api/pkg/middleware"
)

var (
	fixedUser  = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	fixedOther = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	fixedMatch = uuid.MustParse("33333333-3333-3333-3333-333333333333")
	fixedChat  = uuid.MustParse("44444444-4444-4444-4444-444444444444")
)

type mockQuerier struct {
	discover      func(ctx context.Context, params DiscoverParams) ([]user.User, error)
	countDiscover func(ctx context.Context, params DiscoverParams) (int, error)
	listLikes     func(ctx context.Context, userID uuid.UUID, before *string, limit int) ([]DatingLike, error)
	countLikes    func(ctx context.Context, userID uuid.UUID) (int, error)
	recordAction  func(ctx context.Context, actorID, targetID uuid.UUID, action string) (*ActionResult, error)
	listMatches   func(ctx context.Context, userID uuid.UUID, before *string, limit int) ([]DatingMatch, error)
	getMatch      func(ctx context.Context, userID, matchID uuid.UUID) (*DatingMatch, error)
	unmatch       func(ctx context.Context, userID, matchID uuid.UUID) (*DatingMatch, error)
}

func (m *mockQuerier) Discover(ctx context.Context, params DiscoverParams) ([]user.User, error) {
	if m.discover != nil {
		return m.discover(ctx, params)
	}
	return []user.User{}, nil
}

func (m *mockQuerier) CountDiscover(ctx context.Context, params DiscoverParams) (int, error) {
	if m.countDiscover != nil {
		return m.countDiscover(ctx, params)
	}
	return 0, nil
}

func (m *mockQuerier) ListLikes(ctx context.Context, userID uuid.UUID, before *string, limit int) ([]DatingLike, error) {
	if m.listLikes != nil {
		return m.listLikes(ctx, userID, before, limit)
	}
	return []DatingLike{}, nil
}

func (m *mockQuerier) CountLikes(ctx context.Context, userID uuid.UUID) (int, error) {
	if m.countLikes != nil {
		return m.countLikes(ctx, userID)
	}
	return 0, nil
}

func (m *mockQuerier) RecordAction(ctx context.Context, actorID, targetID uuid.UUID, action string) (*ActionResult, error) {
	if m.recordAction != nil {
		return m.recordAction(ctx, actorID, targetID, action)
	}
	return &ActionResult{Action: action}, nil
}

func (m *mockQuerier) ListMatches(ctx context.Context, userID uuid.UUID, before *string, limit int) ([]DatingMatch, error) {
	if m.listMatches != nil {
		return m.listMatches(ctx, userID, before, limit)
	}
	return []DatingMatch{}, nil
}

func (m *mockQuerier) GetMatch(ctx context.Context, userID, matchID uuid.UUID) (*DatingMatch, error) {
	if m.getMatch != nil {
		return m.getMatch(ctx, userID, matchID)
	}
	return &DatingMatch{ID: matchID, Status: "active", MatchedAt: time.Now().UTC()}, nil
}

func (m *mockQuerier) Unmatch(ctx context.Context, userID, matchID uuid.UUID) (*DatingMatch, error) {
	if m.unmatch != nil {
		return m.unmatch(ctx, userID, matchID)
	}
	now := time.Now().UTC()
	return &DatingMatch{ID: matchID, Status: "unmatched", MatchedAt: now, UnmatchedAt: &now}, nil
}

type mockNotifier struct {
	called bool
}

func (m *mockNotifier) NotifyDatingMatch(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) error {
	m.called = true
	return nil
}

func withUserID(req *http.Request, userID uuid.UUID) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
}

func withURLParam(req *http.Request, key string, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestDiscoverParsesDatingFilters(t *testing.T) {
	var got DiscoverParams
	h := NewHandler(&mockQuerier{
		discover: func(_ context.Context, params DiscoverParams) ([]user.User, error) {
			got = params
			return []user.User{{ID: fixedOther, Username: "casey", CreatedAt: time.Now().UTC(), FriendshipStatus: "none"}}, nil
		},
	}, nil)

	req := withUserID(httptest.NewRequest(http.MethodGet, "/dating/discover?gender=woman&age_min=25&age_max=40&distance_km=30&sobriety=years_1&interest=Coffee&lat=53.34&lng=-6.26&limit=10&cursor=20", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.Discover(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if got.CurrentUserID != fixedUser || got.Gender != "woman" || got.Sobriety != "years_1" || got.Cursor != "20" || got.Limit != 10 {
		t.Fatalf("parsed params = %+v", got)
	}
	if got.CursorOffset != 20 || got.CursorRequestID == "" {
		t.Fatalf("cursor params = offset %d request %q", got.CursorOffset, got.CursorRequestID)
	}
	if got.AgeMin == nil || *got.AgeMin != 25 || got.AgeMax == nil || *got.AgeMax != 40 || got.DistanceKm == nil || *got.DistanceKm != 30 {
		t.Fatalf("numeric filters = %+v", got)
	}
	if got.Lat == nil || *got.Lat != 53.34 || got.Lng == nil || *got.Lng != -6.26 {
		t.Fatalf("coords = %+v %+v", got.Lat, got.Lng)
	}
}

func TestDiscoverReturnsOpaqueRankedCursor(t *testing.T) {
	var firstRequestID string
	h := NewHandler(&mockQuerier{
		discover: func(_ context.Context, params DiscoverParams) ([]user.User, error) {
			firstRequestID = params.CursorRequestID
			return []user.User{
				{ID: fixedOther, Username: "casey", CreatedAt: time.Now().UTC(), FriendshipStatus: "none"},
				{ID: fixedMatch, Username: "riley", CreatedAt: time.Now().UTC(), FriendshipStatus: "none"},
			}, nil
		},
	}, nil)

	req := withUserID(httptest.NewRequest(http.MethodGet, "/dating/discover?limit=1", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.Discover(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			NextCursor *string `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.NextCursor == nil {
		t.Fatal("expected next cursor")
	}
	decoded := decodeDatingCursor(*body.Data.NextCursor)
	if decoded.Offset != 1 || decoded.RequestID != firstRequestID {
		t.Fatalf("decoded cursor = %+v, request %q", decoded, firstRequestID)
	}
}

func TestDiscoverRequiresCoordsWithDistance(t *testing.T) {
	h := NewHandler(&mockQuerier{}, nil)
	req := withUserID(httptest.NewRequest(http.MethodGet, "/dating/discover?distance_km=50", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.Discover(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "lat and lng are required") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestListLikesReturnsUsersAndCursor(t *testing.T) {
	likedAt := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	var gotUserID uuid.UUID
	var gotLimit int
	h := NewHandler(&mockQuerier{
		listLikes: func(_ context.Context, userID uuid.UUID, before *string, limit int) ([]DatingLike, error) {
			gotUserID = userID
			gotLimit = limit
			return []DatingLike{
				{User: user.User{ID: fixedOther, Username: "casey", CreatedAt: likedAt, FriendshipStatus: "none"}, LikedAt: likedAt},
				{User: user.User{ID: fixedMatch, Username: "riley", CreatedAt: likedAt, FriendshipStatus: "none"}, LikedAt: likedAt.Add(-time.Hour)},
			}, nil
		},
	}, nil)

	req := withUserID(httptest.NewRequest(http.MethodGet, "/dating/likes?limit=1", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.ListLikes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if gotUserID != fixedUser || gotLimit != 2 {
		t.Fatalf("list args = %s %d", gotUserID, gotLimit)
	}
	var body struct {
		Data struct {
			Items      []user.User `json:"items"`
			HasMore    bool        `json:"has_more"`
			NextCursor *string     `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data.Items) != 1 || body.Data.Items[0].ID != fixedOther || !body.Data.HasMore || body.Data.NextCursor == nil {
		t.Fatalf("body = %+v", body.Data)
	}
}

func TestLikesPreviewReturnsCount(t *testing.T) {
	h := NewHandler(&mockQuerier{
		countLikes: func(_ context.Context, userID uuid.UUID) (int, error) {
			if userID != fixedUser {
				t.Fatalf("userID = %s, want %s", userID, fixedUser)
			}
			return 7, nil
		},
	}, nil)

	req := withUserID(httptest.NewRequest(http.MethodGet, "/dating/likes/preview", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.LikesPreview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"exact_count":7`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestRecordActionNotifiesOnMatch(t *testing.T) {
	notifier := &mockNotifier{}
	h := NewHandler(&mockQuerier{
		recordAction: func(_ context.Context, actorID, targetID uuid.UUID, action string) (*ActionResult, error) {
			if actorID != fixedUser || targetID != fixedOther || action != ActionLike {
				t.Fatalf("action args = %s %s %s", actorID, targetID, action)
			}
			return &ActionResult{
				Action:  ActionLike,
				Matched: true,
				Match:   &DatingMatch{ID: fixedMatch, ChatID: &fixedChat, Status: "active", MatchedAt: time.Now().UTC()},
			}, nil
		},
	}, notifier)

	req := withUserID(httptest.NewRequest(http.MethodPost, "/dating/actions", strings.NewReader(`{"target_user_id":"`+fixedOther.String()+`","action":"like"}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.RecordAction(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
	if !notifier.called {
		t.Fatal("expected dating match notification")
	}
}

func TestRecordActionRejectsInvalidAction(t *testing.T) {
	h := NewHandler(&mockQuerier{}, nil)
	req := withUserID(httptest.NewRequest(http.MethodPost, "/dating/actions", strings.NewReader(`{"target_user_id":"`+fixedOther.String()+`","action":"superlike"}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.RecordAction(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestUnmatchUsesRouteMatchID(t *testing.T) {
	var gotUserID uuid.UUID
	var gotMatchID uuid.UUID
	h := NewHandler(&mockQuerier{
		unmatch: func(_ context.Context, userID, matchID uuid.UUID) (*DatingMatch, error) {
			gotUserID = userID
			gotMatchID = matchID
			now := time.Now().UTC()
			return &DatingMatch{ID: matchID, Status: "unmatched", MatchedAt: now, UnmatchedAt: &now}, nil
		},
	}, nil)

	req := withURLParam(withUserID(httptest.NewRequest(http.MethodPost, "/dating/matches/"+fixedMatch.String()+"/unmatch", nil), fixedUser), "id", fixedMatch.String())
	rec := httptest.NewRecorder()

	h.Unmatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if gotUserID != fixedUser || gotMatchID != fixedMatch {
		t.Fatalf("unmatch args = %s %s", gotUserID, gotMatchID)
	}
}
