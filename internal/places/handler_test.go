package places

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

type mockPlacesQuerier struct {
	autocomplete func(ctx context.Context, params AutocompleteParams) ([]PlaceSuggestion, error)
}

func (m *mockPlacesQuerier) AutocompletePlaces(ctx context.Context, params AutocompleteParams) ([]PlaceSuggestion, error) {
	if m.autocomplete != nil {
		return m.autocomplete(ctx, params)
	}
	return []PlaceSuggestion{}, nil
}

func TestAutocompletePlacesRequiresTwoCharacters(t *testing.T) {
	h := NewHandler(&mockPlacesQuerier{
		autocomplete: func(context.Context, AutocompleteParams) ([]PlaceSuggestion, error) {
			t.Fatal("AutocompletePlaces should not be called for short query")
			return nil, nil
		},
	})

	rec := httptest.NewRecorder()
	h.AutocompletePlaces(rec, httptest.NewRequest(http.MethodGet, "/places/autocomplete?q=b", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAutocompletePlacesParsesParams(t *testing.T) {
	placeID := uuid.New()
	var seen AutocompleteParams
	h := NewHandler(&mockPlacesQuerier{
		autocomplete: func(_ context.Context, params AutocompleteParams) ([]PlaceSuggestion, error) {
			seen = params
			return []PlaceSuggestion{{
				ID:          placeID,
				Label:       "Barcelona, Catalonia, Spain",
				Name:        "Barcelona",
				Country:     "Spain",
				CountryCode: "ES",
				Latitude:    41.38879,
				Longitude:   2.15899,
				Population:  1621537,
				Source:      "geonames",
			}}, nil
		},
	})

	rec := httptest.NewRecorder()
	h.AutocompletePlaces(rec, httptest.NewRequest(http.MethodGet, "/places/autocomplete?q=barcel&country_code=es&limit=50", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if seen.Query != "barcel" || seen.CountryCode != "ES" || seen.Limit != 10 {
		t.Fatalf("params = %#v", seen)
	}
	var body struct {
		Data []PlaceSuggestion `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0].ID != placeID {
		t.Fatalf("data = %#v", body.Data)
	}
}

func TestAutocompletePlacesRejectsInvalidLimit(t *testing.T) {
	h := NewHandler(&mockPlacesQuerier{})
	rec := httptest.NewRecorder()

	h.AutocompletePlaces(rec, httptest.NewRequest(http.MethodGet, "/places/autocomplete?q=barcel&limit=zero", nil))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
