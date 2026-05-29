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
	"github.com/project_radeon/api/pkg/middleware"
)

var (
	fixedUser  = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	fixedOther = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	fixedMatch = uuid.MustParse("33333333-3333-3333-3333-333333333333")
	fixedChat  = uuid.MustParse("44444444-4444-4444-4444-444444444444")
)

func testDatingProfile(id uuid.UUID, username string, createdAt time.Time) DatingProfile {
	return DatingProfile{
		ID:                  id,
		UserID:              id,
		Username:            username,
		RelationshipGoal:    "long_term",
		InterestedInGenders: []string{"woman"},
		AgeMin:              25,
		AgeMax:              45,
		DistanceKm:          50,
		CreatedAt:           createdAt,
		UpdatedAt:           createdAt,
	}
}

type mockQuerier struct {
	getMyProfile  func(ctx context.Context, userID uuid.UUID) (*DatingProfile, error)
	getProfile    func(ctx context.Context, viewerID, profileID uuid.UUID) (*DatingProfile, error)
	updateProfile func(ctx context.Context, userID uuid.UUID, input UpdateProfileInput) (*DatingProfile, error)
	listInterests func(ctx context.Context) ([]string, error)
	addPhoto      func(ctx context.Context, userID uuid.UUID, imageURL string, width, height int) (*DatingProfile, error)
	deletePhoto   func(ctx context.Context, userID, photoID uuid.UUID) (*DatingProfile, error)
	reorderPhotos func(ctx context.Context, userID uuid.UUID, photoIDs []uuid.UUID) (*DatingProfile, error)
	discover      func(ctx context.Context, params DiscoverParams) ([]DatingProfile, error)
	countDiscover func(ctx context.Context, params DiscoverParams) (int, error)
	listLikes     func(ctx context.Context, userID uuid.UUID, before *string, limit int) ([]DatingLike, error)
	countLikes    func(ctx context.Context, userID uuid.UUID) (int, error)
	recordAction  func(ctx context.Context, actorID, targetID uuid.UUID, action string) (*ActionResult, error)
	listMatches   func(ctx context.Context, userID uuid.UUID, before *string, limit int) ([]DatingMatch, error)
	countUnseen   func(ctx context.Context, userID uuid.UUID) (int, error)
	markSeen      func(ctx context.Context, userID uuid.UUID) (time.Time, error)
	getMatch      func(ctx context.Context, userID, matchID uuid.UUID) (*DatingMatch, error)
	unmatch       func(ctx context.Context, userID, matchID uuid.UUID) (*DatingMatch, error)
	logEvents     func(ctx context.Context, userID uuid.UUID, events []DatingEventInput) error
}

func (m *mockQuerier) GetMyProfile(ctx context.Context, userID uuid.UUID) (*DatingProfile, error) {
	if m.getMyProfile != nil {
		return m.getMyProfile(ctx, userID)
	}
	profile := testDatingProfile(userID, "self", time.Now().UTC())
	return &profile, nil
}

func (m *mockQuerier) GetProfile(ctx context.Context, viewerID, profileID uuid.UUID) (*DatingProfile, error) {
	if m.getProfile != nil {
		return m.getProfile(ctx, viewerID, profileID)
	}
	profile := testDatingProfile(profileID, "casey", time.Now().UTC())
	return &profile, nil
}

func (m *mockQuerier) UpdateMyProfile(ctx context.Context, userID uuid.UUID, input UpdateProfileInput) (*DatingProfile, error) {
	if m.updateProfile != nil {
		return m.updateProfile(ctx, userID, input)
	}
	profile := testDatingProfile(userID, "self", time.Now().UTC())
	return &profile, nil
}

func (m *mockQuerier) ListInterests(ctx context.Context) ([]string, error) {
	if m.listInterests != nil {
		return m.listInterests(ctx)
	}
	return []string{"Coffee", "Hiking", "Music"}, nil
}

func (m *mockQuerier) AddPhoto(ctx context.Context, userID uuid.UUID, imageURL string, width, height int) (*DatingProfile, error) {
	if m.addPhoto != nil {
		return m.addPhoto(ctx, userID, imageURL, width, height)
	}
	profile := testDatingProfile(userID, "self", time.Now().UTC())
	return &profile, nil
}

func (m *mockQuerier) DeletePhoto(ctx context.Context, userID, photoID uuid.UUID) (*DatingProfile, error) {
	if m.deletePhoto != nil {
		return m.deletePhoto(ctx, userID, photoID)
	}
	profile := testDatingProfile(userID, "self", time.Now().UTC())
	return &profile, nil
}

func (m *mockQuerier) ReorderPhotos(ctx context.Context, userID uuid.UUID, photoIDs []uuid.UUID) (*DatingProfile, error) {
	if m.reorderPhotos != nil {
		return m.reorderPhotos(ctx, userID, photoIDs)
	}
	profile := testDatingProfile(userID, "self", time.Now().UTC())
	return &profile, nil
}

func (m *mockQuerier) Discover(ctx context.Context, params DiscoverParams) ([]DatingProfile, error) {
	if m.discover != nil {
		return m.discover(ctx, params)
	}
	return []DatingProfile{}, nil
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

func (m *mockQuerier) CountUnseenMatches(ctx context.Context, userID uuid.UUID) (int, error) {
	if m.countUnseen != nil {
		return m.countUnseen(ctx, userID)
	}
	return 0, nil
}

func (m *mockQuerier) MarkMatchesSeen(ctx context.Context, userID uuid.UUID) (time.Time, error) {
	if m.markSeen != nil {
		return m.markSeen(ctx, userID)
	}
	return time.Now().UTC(), nil
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

func (m *mockQuerier) LogEvents(ctx context.Context, userID uuid.UUID, events []DatingEventInput) error {
	if m.logEvents != nil {
		return m.logEvents(ctx, userID, events)
	}
	return nil
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
		discover: func(_ context.Context, params DiscoverParams) ([]DatingProfile, error) {
			got = params
			return []DatingProfile{testDatingProfile(fixedOther, "casey", time.Now().UTC())}, nil
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
		discover: func(_ context.Context, params DiscoverParams) ([]DatingProfile, error) {
			firstRequestID = params.CursorRequestID
			return []DatingProfile{
				testDatingProfile(fixedOther, "casey", time.Now().UTC()),
				testDatingProfile(fixedMatch, "riley", time.Now().UTC()),
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

func TestDiscoverUsesPublicProfilePayload(t *testing.T) {
	h := NewHandler(&mockQuerier{
		discover: func(_ context.Context, _ DiscoverParams) ([]DatingProfile, error) {
			profile := testDatingProfile(fixedOther, "casey", time.Now().UTC())
			profile.InterestedInGenders = []string{"woman"}
			profile.AgeMin = 30
			profile.AgeMax = 45
			profile.DistanceKm = 25
			return []DatingProfile{profile}, nil
		},
	}, nil)

	req := withUserID(httptest.NewRequest(http.MethodGet, "/dating/discover", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.Discover(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, privateField := range []string{"interested_in_genders", "age_min", "age_max", "distance_km", "paused", "completed_at"} {
		if strings.Contains(body, privateField) {
			t.Fatalf("public discover response leaked %s: %s", privateField, body)
		}
	}
	if !strings.Contains(body, `"username":"casey"`) {
		t.Fatalf("response missing public profile data: %s", body)
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

func TestUpdateMyProfileAcceptsDatingDetails(t *testing.T) {
	var got UpdateProfileInput
	h := NewHandler(&mockQuerier{
		updateProfile: func(_ context.Context, userID uuid.UUID, input UpdateProfileInput) (*DatingProfile, error) {
			if userID != fixedUser {
				t.Fatalf("userID = %s, want %s", userID, fixedUser)
			}
			got = input
			profile := testDatingProfile(userID, "self", time.Now().UTC())
			profile.HeightCm = input.HeightCm
			profile.JobTitle = input.JobTitle
			profile.Company = input.Company
			profile.Work = input.Work
			profile.School = input.School
			profile.Course = input.Course
			profile.Education = input.Education
			if input.KidsStatus != nil {
				profile.KidsStatus = *input.KidsStatus
			}
			profile.Interests = input.Interests
			return &profile, nil
		},
	}, nil)

	req := withUserID(httptest.NewRequest(http.MethodPatch, "/dating/profile", strings.NewReader(`{
		"height_cm": 178,
		"job_title": "Designer",
		"company": "Studio Co",
		"school": "Trinity",
		"course": "BA Psychology",
		"kids_status": "dont_have_kids",
		"interests": ["Hiking", "Coffee"]
	}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMyProfile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if got.HeightCm == nil || *got.HeightCm != 178 || !got.ReplaceHeight {
		t.Fatalf("height = %v replace %v", got.HeightCm, got.ReplaceHeight)
	}
	if got.JobTitle == nil || *got.JobTitle != "Designer" || got.Company == nil || *got.Company != "Studio Co" {
		t.Fatalf("job/company = %v %v", got.JobTitle, got.Company)
	}
	if got.School == nil || *got.School != "Trinity" || got.Course == nil || *got.Course != "BA Psychology" {
		t.Fatalf("school/course = %v %v", got.School, got.Course)
	}
	if got.KidsStatus == nil || *got.KidsStatus != "dont_have_kids" {
		t.Fatalf("kids status = %v", got.KidsStatus)
	}
	if !got.ReplaceInterests || len(got.Interests) != 2 || got.Interests[0] != "Coffee" || got.Interests[1] != "Hiking" {
		t.Fatalf("interests = %v replace %v", got.Interests, got.ReplaceInterests)
	}
}

func TestUpdateMyProfileRejectsInvalidDatingInterest(t *testing.T) {
	h := NewHandler(&mockQuerier{}, nil)
	req := withUserID(httptest.NewRequest(http.MethodPatch, "/dating/profile", strings.NewReader(`{"interests":["Clubbing"]}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.UpdateMyProfile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
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
				{Profile: testDatingProfile(fixedOther, "casey", likedAt), LikedAt: likedAt},
				{Profile: testDatingProfile(fixedMatch, "riley", likedAt), LikedAt: likedAt.Add(-time.Hour)},
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
			Items      []DatingProfile `json:"items"`
			HasMore    bool            `json:"has_more"`
			NextCursor *string         `json:"next_cursor"`
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
				Match:   &DatingMatch{ID: fixedMatch, Profile: testDatingProfile(fixedOther, "casey", time.Now().UTC()), ChatID: &fixedChat, Status: "active", MatchedAt: time.Now().UTC()},
			}, nil
		},
	}, notifier)

	req := withUserID(httptest.NewRequest(http.MethodPost, "/dating/actions", strings.NewReader(`{"target_profile_id":"`+fixedOther.String()+`","action":"like"}`)), fixedUser)
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
	req := withUserID(httptest.NewRequest(http.MethodPost, "/dating/actions", strings.NewReader(`{"target_profile_id":"`+fixedOther.String()+`","action":"superlike"}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.RecordAction(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestListMatchesIncludesUnseenCount(t *testing.T) {
	matchedAt := time.Date(2026, 5, 29, 14, 0, 0, 0, time.UTC)
	var gotLimit int
	h := NewHandler(&mockQuerier{
		listMatches: func(_ context.Context, userID uuid.UUID, before *string, limit int) ([]DatingMatch, error) {
			if userID != fixedUser {
				t.Fatalf("userID = %s, want %s", userID, fixedUser)
			}
			gotLimit = limit
			return []DatingMatch{
				{ID: fixedMatch, Profile: testDatingProfile(fixedOther, "casey", matchedAt), Status: "active", MatchedAt: matchedAt},
			}, nil
		},
		countUnseen: func(_ context.Context, userID uuid.UUID) (int, error) {
			if userID != fixedUser {
				t.Fatalf("count userID = %s, want %s", userID, fixedUser)
			}
			return 3, nil
		},
	}, nil)

	req := withUserID(httptest.NewRequest(http.MethodGet, "/dating/matches?limit=1", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.ListMatches(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if gotLimit != 2 {
		t.Fatalf("limit = %d, want 2", gotLimit)
	}
	var body struct {
		Data struct {
			Items       []DatingMatchResponse `json:"items"`
			UnseenCount int                   `json:"unseen_count"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.UnseenCount != 3 || len(body.Data.Items) != 1 || body.Data.Items[0].ID != fixedMatch {
		t.Fatalf("body = %+v", body.Data)
	}
}

func TestMarkMatchesSeenUsesCurrentUser(t *testing.T) {
	seenAt := time.Date(2026, 5, 29, 15, 0, 0, 0, time.UTC)
	var gotUserID uuid.UUID
	h := NewHandler(&mockQuerier{
		markSeen: func(_ context.Context, userID uuid.UUID) (time.Time, error) {
			gotUserID = userID
			return seenAt, nil
		},
	}, nil)

	req := withUserID(httptest.NewRequest(http.MethodPost, "/dating/matches/seen", nil), fixedUser)
	rec := httptest.NewRecorder()

	h.MarkMatchesSeen(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if gotUserID != fixedUser {
		t.Fatalf("userID = %s, want %s", gotUserID, fixedUser)
	}
	if !strings.Contains(rec.Body.String(), `"unseen_count":0`) {
		t.Fatalf("body = %s", rec.Body.String())
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

func TestLogEventsValidatesAndForwardsEvents(t *testing.T) {
	var gotUserID uuid.UUID
	var gotEvents []DatingEventInput
	h := NewHandler(&mockQuerier{
		logEvents: func(_ context.Context, userID uuid.UUID, events []DatingEventInput) error {
			gotUserID = userID
			gotEvents = events
			return nil
		},
	}, nil)

	req := withUserID(httptest.NewRequest(http.MethodPost, "/dating/events", strings.NewReader(`{"events":[{"profile_id":"`+fixedOther.String()+`","event_type":"profile_opened","payload":{"surface":"discover"}}]}`)), fixedUser)
	rec := httptest.NewRecorder()

	h.LogEvents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if gotUserID != fixedUser || len(gotEvents) != 1 || gotEvents[0].EventType != DatingEventProfileOpened {
		t.Fatalf("events = user %s %+v", gotUserID, gotEvents)
	}
}
