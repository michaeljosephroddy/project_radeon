package recoverymeetings

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPgStoreListRecoveryMeetingsFindsCarlowByLocation(t *testing.T) {
	pool := recoveryMeetingTestPool(t)

	page, err := NewPgStore(pool).ListRecoveryMeetings(context.Background(), ListParams{
		Fellowships: []string{"ca"},
		Country:     "ireland",
		Location:    "Carlow",
		MeetingType: "in_person",
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("list recovery meetings: %v", err)
	}

	carlowMeetings := 0
	for _, meeting := range page.Items {
		if meeting.Name == "C.A. Carlow" {
			carlowMeetings++
		}
	}
	if carlowMeetings != 2 {
		t.Fatalf("C.A. Carlow meetings = %d, want 2; items = %#v", carlowMeetings, page.Items)
	}
}

func TestPgStoreRecoveryMeetingSuggestionsFindCarlowAndIreland(t *testing.T) {
	pool := recoveryMeetingTestPool(t)
	store := NewPgStore(pool)

	locations, err := store.ListLocationSuggestions(context.Background(), "Carl", "Ireland", "", "ca", 8)
	if err != nil {
		t.Fatalf("list location suggestions: %v", err)
	}
	foundCarlow := false
	for _, suggestion := range locations {
		if suggestion.Location == "Carlow" && suggestion.Region != nil && *suggestion.Region == "Carlow" {
			foundCarlow = true
			break
		}
	}
	if !foundCarlow {
		t.Fatalf("Carlow location and county suggestion missing: %#v", locations)
	}

	countries, err := store.ListCountrySuggestions(context.Background(), "Ire", "ca", 8)
	if err != nil {
		t.Fatalf("list country suggestions: %v", err)
	}
	foundIreland := false
	for _, suggestion := range countries {
		if suggestion.Country == "Ireland" {
			foundIreland = true
			break
		}
	}
	if !foundIreland {
		t.Fatalf("Ireland suggestion missing: %#v", countries)
	}
}

func TestPgStoreRecoveryMeetingSuggestionsDeriveCountyFromCity(t *testing.T) {
	pool := recoveryMeetingTestPool(t)
	store := NewPgStore(pool)

	regions, err := store.ListRegionSuggestions(context.Background(), "Dub", "Ireland", "ca", 8)
	if err != nil {
		t.Fatalf("list region suggestions: %v", err)
	}
	foundDublinRegion := false
	for _, suggestion := range regions {
		if suggestion.Region == "Dublin" {
			foundDublinRegion = true
			break
		}
	}
	if !foundDublinRegion {
		t.Fatalf("Dublin region suggestion missing: %#v", regions)
	}

	locations, err := store.ListLocationSuggestions(context.Background(), "Dub", "Ireland", "Dublin", "ca", 8)
	if err != nil {
		t.Fatalf("list location suggestions: %v", err)
	}
	foundDublinLocation := false
	for _, suggestion := range locations {
		if suggestion.Location == "Dublin" && suggestion.Region != nil && *suggestion.Region == "Dublin" {
			foundDublinLocation = true
			break
		}
	}
	if !foundDublinLocation {
		t.Fatalf("Dublin location suggestion missing: %#v", locations)
	}
}

func TestPgStoreRecoveryMeetingFilterOptions(t *testing.T) {
	pool := recoveryMeetingTestPool(t)
	store := NewPgStore(pool)

	countries, err := store.ListFilterOptions(context.Background(), FilterOptionsParams{
		Level: FilterOptionLevelCountry,
		Query: "ire",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("list country filter options: %v", err)
	}
	foundIreland := false
	for _, option := range countries {
		if option.Country != nil && *option.Country == "Ireland" && option.MeetingCount > 0 {
			foundIreland = true
			break
		}
	}
	if !foundIreland {
		t.Fatalf("Ireland country option missing: %#v", countries)
	}

	regions, err := store.ListFilterOptions(context.Background(), FilterOptionsParams{
		Level:   FilterOptionLevelRegion,
		Query:   "dub",
		Country: "Ireland",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("list region filter options: %v", err)
	}
	foundDublinRegion := false
	for _, option := range regions {
		if option.Region != nil && *option.Region == "Dublin" && option.Country != nil && *option.Country == "Ireland" {
			foundDublinRegion = true
			break
		}
	}
	if !foundDublinRegion {
		t.Fatalf("Dublin region option missing: %#v", regions)
	}

	localities, err := store.ListFilterOptions(context.Background(), FilterOptionsParams{
		Level:   FilterOptionLevelLocality,
		Query:   "dub",
		Country: "Ireland",
		Region:  "Dublin",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("list locality filter options: %v", err)
	}
	foundDublinLocality := false
	for _, option := range localities {
		if option.Locality != nil && *option.Locality == "Dublin" && option.Region != nil && *option.Region == "Dublin" {
			foundDublinLocality = true
			break
		}
	}
	if !foundDublinLocality {
		t.Fatalf("Dublin locality option missing: %#v", localities)
	}
}

func TestPgStoreListRecoveryMeetingsSearchesPortlaoiseByFellowshipAndPlaceTokens(t *testing.T) {
	pool := recoveryMeetingTestPool(t)

	page, err := NewPgStore(pool).ListRecoveryMeetings(context.Background(), ListParams{
		Query: "na portlaoise ireland",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("list recovery meetings: %v", err)
	}

	found := false
	for _, meeting := range page.Items {
		if meeting.Fellowship == "na" && meeting.Name == "Step To Freedom Group Portlaoise" && meeting.Country != nil && *meeting.Country == "Ireland" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("NA Portlaoise Ireland result missing: %#v", page.Items)
	}
}

func recoveryMeetingTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("RECOVERY_MEETINGS_DB_TEST") != "1" {
		t.Skip("set RECOVERY_MEETINGS_DB_TEST=1 with DATABASE_URL to run database-backed recovery meeting finder checks")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
