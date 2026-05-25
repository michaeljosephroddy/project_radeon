package recoverymeetings

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestBuildRecoveryMeetingListQueryUsesForgivingLocation(t *testing.T) {
	query, args, limit := buildRecoveryMeetingListQuery(ListParams{
		Fellowship:  "ca",
		Country:     "Ireland",
		Location:    "Carlow",
		MeetingType: "in_person",
		Limit:       25,
	})

	if limit != 25 {
		t.Fatalf("limit = %d, want 25", limit)
	}
	if strings.Contains(query, "LOWER(COALESCE(rm.city, '')) = LOWER") {
		t.Fatalf("query still uses exact city matching:\n%s", query)
	}
	for _, fragment := range []string{
		"COALESCE(rm.city, '') ILIKE",
		"COALESCE(rm.region, '') ILIKE",
		"COALESCE(rm.venue_name, '') ILIKE",
		"COALESCE(rm.address_line1, '') ILIKE",
		"COALESCE(rm.address_line2, '') ILIKE",
		"COALESCE(rm.postal_code, '') ILIKE",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("query missing %q:\n%s", fragment, query)
		}
	}
	if !containsArg(args, "%Carlow%") {
		t.Fatalf("args missing location pattern: %#v", args)
	}
	if strings.Contains(query, " OFFSET ") {
		t.Fatalf("query should use keyset pagination, not offset:\n%s", query)
	}
}

func TestBuildRecoveryMeetingListQueryFallsBackFromCityToLocation(t *testing.T) {
	query, args, _ := buildRecoveryMeetingListQuery(ListParams{City: "Carlow"})

	if !strings.Contains(query, "COALESCE(rm.city, '') ILIKE") {
		t.Fatalf("query missing location predicate:\n%s", query)
	}
	if !containsArg(args, "%Carlow%") {
		t.Fatalf("args missing legacy city pattern: %#v", args)
	}
}

func TestBuildRecoveryMeetingListQuerySearchesLocationDetails(t *testing.T) {
	query, _, _ := buildRecoveryMeetingListQuery(ListParams{Query: "cathedral"})

	for _, fragment := range []string{
		"rm.name ILIKE",
		"COALESCE(rm.venue_name, '') ILIKE",
		"COALESCE(rm.address_line1, '') ILIKE",
		"COALESCE(rm.address_line2, '') ILIKE",
		"COALESCE(rm.postal_code, '') ILIKE",
		"FROM unnest(rm.formats)",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("query missing search fragment %q:\n%s", fragment, query)
		}
	}
}

func TestBuildRecoveryMeetingListQueryTokenizesSearchAndDetectsFellowship(t *testing.T) {
	query, args, _ := buildRecoveryMeetingListQuery(ListParams{Query: "na portlaoise ireland"})

	if !strings.Contains(query, "rm.fellowship = ") {
		t.Fatalf("query missing fellowship predicate:\n%s", query)
	}
	for _, want := range []any{"na", "%portlaoise%", "%ireland%"} {
		if !containsArg(args, want) {
			t.Fatalf("args missing %q: %#v", want, args)
		}
	}
	if containsArg(args, "%na%") {
		t.Fatalf("fellowship token should not be treated as a generic search term: %#v", args)
	}
}

func TestParseMeetingSearchQueryHandlesDottedFellowship(t *testing.T) {
	fellowship, terms := parseMeetingSearchQuery("C.A. Carlow")
	if fellowship != "ca" {
		t.Fatalf("fellowship = %q, want ca", fellowship)
	}
	if len(terms) != 1 || terms[0] != "carlow" {
		t.Fatalf("terms = %#v, want [carlow]", terms)
	}
}

func TestBuildRecoveryMeetingListQueryAppliesKeysetCursor(t *testing.T) {
	id := uuid.New()
	cursor := encodeListCursor(listCursor{
		SortDay:  4,
		SortTime: "20:00:00",
		SortName: "c.a. carlow",
		ID:       id,
	})
	query, args, _ := buildRecoveryMeetingListQuery(ListParams{Cursor: cursor})

	if !strings.Contains(query, ") > (") {
		t.Fatalf("query missing keyset comparison:\n%s", query)
	}
	if !containsArg(args, 4) || !containsArg(args, "20:00:00") || !containsArg(args, "c.a. carlow") || !containsArg(args, id) {
		t.Fatalf("args missing cursor values: %#v", args)
	}
}

func containsArg(args []any, want any) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
