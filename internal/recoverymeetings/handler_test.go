package recoverymeetings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type mockRecoveryQuerier struct {
	list                    func(ctx context.Context, params ListParams) (*CursorPage[RecoveryMeeting], error)
	listLocationSuggestions func(ctx context.Context, query, country, region, fellowship string, limit int) ([]LocationSuggestion, error)
	listRegionSuggestions   func(ctx context.Context, query, country, fellowship string, limit int) ([]RegionSuggestion, error)
	listCountrySuggestions  func(ctx context.Context, query, fellowship string, limit int) ([]CountrySuggestion, error)
	get                     func(ctx context.Context, id uuid.UUID) (*RecoveryMeeting, error)
}

func (m *mockRecoveryQuerier) ListRecoveryMeetings(ctx context.Context, params ListParams) (*CursorPage[RecoveryMeeting], error) {
	if m.list != nil {
		return m.list(ctx, params)
	}
	return &CursorPage[RecoveryMeeting]{Items: []RecoveryMeeting{}, Limit: params.Limit}, nil
}

func (m *mockRecoveryQuerier) ListLocationSuggestions(ctx context.Context, query, country, region, fellowship string, limit int) ([]LocationSuggestion, error) {
	if m.listLocationSuggestions != nil {
		return m.listLocationSuggestions(ctx, query, country, region, fellowship, limit)
	}
	return []LocationSuggestion{}, nil
}

func (m *mockRecoveryQuerier) ListRegionSuggestions(ctx context.Context, query, country, fellowship string, limit int) ([]RegionSuggestion, error) {
	if m.listRegionSuggestions != nil {
		return m.listRegionSuggestions(ctx, query, country, fellowship, limit)
	}
	return []RegionSuggestion{}, nil
}

func (m *mockRecoveryQuerier) ListCountrySuggestions(ctx context.Context, query, fellowship string, limit int) ([]CountrySuggestion, error) {
	if m.listCountrySuggestions != nil {
		return m.listCountrySuggestions(ctx, query, fellowship, limit)
	}
	return []CountrySuggestion{}, nil
}

func (m *mockRecoveryQuerier) GetRecoveryMeeting(ctx context.Context, id uuid.UUID) (*RecoveryMeeting, error) {
	if m.get != nil {
		return m.get(ctx, id)
	}
	return nil, ErrNotFound
}

func TestListLocationSuggestionsSuccess(t *testing.T) {
	h := NewHandler(&mockRecoveryQuerier{
		listLocationSuggestions: func(_ context.Context, query, country, region, fellowship string, limit int) ([]LocationSuggestion, error) {
			if query != "Port" || country != "Ireland" || region != "Laois" || fellowship != "ca" || limit != 8 {
				t.Fatalf("query = %q country = %q region = %q fellowship = %q limit = %d", query, country, region, fellowship, limit)
			}
			countryValue := "Ireland"
			regionValue := "Laois"
			return []LocationSuggestion{{Label: "Portlaoise, Laois, Ireland", Location: "Portlaoise", Region: &regionValue, Country: &countryValue, MeetingCount: 2}}, nil
		},
	})
	rec := httptest.NewRecorder()
	h.ListLocationSuggestions(rec, httptest.NewRequest(http.MethodGet, "/recovery-meetings/locations?q=Port&country=Ireland&region=Laois&fellowship=CA", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestListLocationSuggestionsRequiresTwoCharacters(t *testing.T) {
	h := NewHandler(&mockRecoveryQuerier{
		listLocationSuggestions: func(context.Context, string, string, string, string, int) ([]LocationSuggestion, error) {
			t.Fatal("expected short query to skip store lookup")
			return nil, nil
		},
	})
	rec := httptest.NewRecorder()
	h.ListLocationSuggestions(rec, httptest.NewRequest(http.MethodGet, "/recovery-meetings/locations?q=C", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestListLocationSuggestionsRequiresCountry(t *testing.T) {
	h := NewHandler(&mockRecoveryQuerier{
		listLocationSuggestions: func(context.Context, string, string, string, string, int) ([]LocationSuggestion, error) {
			t.Fatal("expected missing country to skip store lookup")
			return nil, nil
		},
	})
	rec := httptest.NewRecorder()
	h.ListLocationSuggestions(rec, httptest.NewRequest(http.MethodGet, "/recovery-meetings/locations?q=Port", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestListRegionSuggestionsSuccess(t *testing.T) {
	h := NewHandler(&mockRecoveryQuerier{
		listRegionSuggestions: func(_ context.Context, query, country, fellowship string, limit int) ([]RegionSuggestion, error) {
			if query != "Lai" || country != "Ireland" || fellowship != "ca" || limit != 8 {
				t.Fatalf("query = %q country = %q fellowship = %q limit = %d", query, country, fellowship, limit)
			}
			return []RegionSuggestion{{Label: "Laois, Ireland", Region: "Laois", Country: "Ireland", MeetingCount: 12}}, nil
		},
	})
	rec := httptest.NewRecorder()
	h.ListRegionSuggestions(rec, httptest.NewRequest(http.MethodGet, "/recovery-meetings/regions?q=Lai&country=Ireland&fellowship=CA", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestListRegionSuggestionsRequiresCountry(t *testing.T) {
	h := NewHandler(&mockRecoveryQuerier{
		listRegionSuggestions: func(context.Context, string, string, string, int) ([]RegionSuggestion, error) {
			t.Fatal("expected missing country to skip store lookup")
			return nil, nil
		},
	})
	rec := httptest.NewRecorder()
	h.ListRegionSuggestions(rec, httptest.NewRequest(http.MethodGet, "/recovery-meetings/regions?q=Lai", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestListCountrySuggestionsSuccess(t *testing.T) {
	h := NewHandler(&mockRecoveryQuerier{
		listCountrySuggestions: func(_ context.Context, query, fellowship string, limit int) ([]CountrySuggestion, error) {
			if query != "Ire" || fellowship != "ca" || limit != 8 {
				t.Fatalf("query = %q fellowship = %q limit = %d", query, fellowship, limit)
			}
			return []CountrySuggestion{{Label: "Ireland", Country: "Ireland", MeetingCount: 151}}, nil
		},
	})
	rec := httptest.NewRecorder()
	h.ListCountrySuggestions(rec, httptest.NewRequest(http.MethodGet, "/recovery-meetings/countries?q=Ire&fellowship=CA", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestListRecoveryMeetingsPassesFiltersAndReturnsCredentials(t *testing.T) {
	meetingID := uuid.New()
	onlineURL := "https://zoom.example/j/123456789"
	phoneJoinInfo := "Meeting ID: 123 456 789 Passcode: sober"
	var seen ListParams
	h := NewHandler(&mockRecoveryQuerier{
		list: func(_ context.Context, params ListParams) (*CursorPage[RecoveryMeeting], error) {
			seen = params
			return &CursorPage[RecoveryMeeting]{
				Items: []RecoveryMeeting{{
					ID:             meetingID,
					Fellowship:     "ca",
					SourceID:       "ca-ie-feed",
					SourceRecordID: "daily-reflection-monday",
					SourceURL:      "https://example.org/meetings.json",
					Name:           "Daily Reflection",
					MeetingType:    "online",
					OnlineURL:      &onlineURL,
					PhoneJoinInfo:  &phoneJoinInfo,
					Formats:        []string{"Open"},
					Occurrences: []MeetingOccurrence{{
						ID:             uuid.New(),
						DayOfWeek:      1,
						StartTimeLocal: "19:30:00",
						Timezone:       "Europe/Dublin",
					}},
					UpdatedAt: time.Date(2026, 5, 23, 22, 7, 0, 0, time.UTC),
				}},
				Limit: 5,
			}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/recovery-meetings?fellowship=CA&meeting_type=online&country=Ireland&region=Leinster&city=Dublin&day_of_week=1&limit=5&q=zoom", nil)
	rec := httptest.NewRecorder()
	h.ListRecoveryMeetings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if seen.Fellowship != "ca" || seen.MeetingType != "online" || seen.Country != "Ireland" || seen.Region != "Leinster" || seen.City != "Dublin" || seen.Location != "Dublin" || seen.Query != "zoom" || seen.Limit != 5 {
		t.Fatalf("params = %#v", seen)
	}
	if seen.DayOfWeek == nil || *seen.DayOfWeek != 1 {
		t.Fatalf("day_of_week = %#v", seen.DayOfWeek)
	}

	var body struct {
		Data CursorPage[RecoveryMeeting] `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(body.Data.Items))
	}
	got := body.Data.Items[0]
	if got.OnlineURL == nil || *got.OnlineURL != onlineURL {
		t.Fatalf("online_url = %#v", got.OnlineURL)
	}
	if got.PhoneJoinInfo == nil || *got.PhoneJoinInfo != phoneJoinInfo {
		t.Fatalf("phone_join_info = %#v", got.PhoneJoinInfo)
	}
}

func TestListRecoveryMeetingsPrefersLocationOverLegacyCity(t *testing.T) {
	var seen ListParams
	h := NewHandler(&mockRecoveryQuerier{
		list: func(_ context.Context, params ListParams) (*CursorPage[RecoveryMeeting], error) {
			seen = params
			return &CursorPage[RecoveryMeeting]{Items: []RecoveryMeeting{}, Limit: params.Limit}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/recovery-meetings?location=Carlow&city=Dublin&country=Ireland", nil)
	rec := httptest.NewRecorder()
	h.ListRecoveryMeetings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if seen.Location != "Carlow" || seen.City != "Dublin" || seen.Country != "Ireland" {
		t.Fatalf("params = %#v", seen)
	}
}

func TestListRecoveryMeetingsRejectsInvalidFilters(t *testing.T) {
	h := NewHandler(&mockRecoveryQuerier{})
	req := httptest.NewRequest(http.MethodGet, "/recovery-meetings?day_of_week=9&meeting_type=invalid&limit=zero", nil)
	rec := httptest.NewRecorder()

	h.ListRecoveryMeetings(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Errors map[string]string `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, field := range []string{"day_of_week", "meeting_type", "limit"} {
		if body.Errors[field] == "" {
			t.Fatalf("missing validation error for %s: %#v", field, body.Errors)
		}
	}
}

func TestGetRecoveryMeetingNotFound(t *testing.T) {
	h := NewHandler(&mockRecoveryQuerier{
		get: func(context.Context, uuid.UUID) (*RecoveryMeeting, error) {
			return nil, ErrNotFound
		},
	})
	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/recovery-meetings/"+id.String(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.GetRecoveryMeeting(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
