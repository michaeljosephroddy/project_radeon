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
		Fellowship:  "ca",
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
		if suggestion.Location == "Co. Carlow" {
			foundCarlow = true
			break
		}
	}
	if !foundCarlow {
		t.Fatalf("Co. Carlow suggestion missing: %#v", locations)
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
