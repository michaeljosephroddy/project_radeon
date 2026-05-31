package places

import (
	"strings"
	"testing"
)

func TestRefreshRecoveryMeetingPlaceMatchesSQLDeletesStaleAutomatedMatches(t *testing.T) {
	for _, fragment := range []string{
		"deleted_stale_matches AS",
		"DELETE FROM recovery_meeting_place_matches existing",
		"AND rm.status = 'active'",
		"AND existing.match_level <> 'manual'",
		"NOT EXISTS",
		"FROM chosen_matches chosen",
		"deleted_inactive_matches AS",
		"AND rm.status <> 'active'",
	} {
		if !strings.Contains(refreshRecoveryMeetingPlaceMatchesSQL, fragment) {
			t.Fatalf("refresh SQL missing %q:\n%s", fragment, refreshRecoveryMeetingPlaceMatchesSQL)
		}
	}
}

func TestRefreshRecoveryMeetingPlaceMatchesSQLRequiresCoordinateCountryConsistency(t *testing.T) {
	for _, fragment := range []string{
		"(am.country_code IS NOT NULL AND p.country_code = am.country_code)",
		"(am.country_code IS NULL AND am.country IS NOT NULL AND LOWER(COALESCE(p.country_name, '')) = LOWER(am.country))",
		"(am.country_code IS NULL AND am.country IS NULL)",
	} {
		if !strings.Contains(refreshRecoveryMeetingPlaceMatchesSQL, fragment) {
			t.Fatalf("refresh SQL missing coordinate country guard %q:\n%s", fragment, refreshRecoveryMeetingPlaceMatchesSQL)
		}
	}
	if strings.Contains(refreshRecoveryMeetingPlaceMatchesSQL, "OR am.country_code IS NULL\n") {
		t.Fatalf("refresh SQL still allows any coordinate place when meeting country_code is empty:\n%s", refreshRecoveryMeetingPlaceMatchesSQL)
	}
}

func TestRefreshRecoveryMeetingPlaceMatchesSQLPreservesManualMatches(t *testing.T) {
	for _, fragment := range []string{
		"WHERE recovery_meeting_place_matches.match_level <> 'manual'",
		"AND existing.match_level <> 'manual'",
	} {
		if !strings.Contains(refreshRecoveryMeetingPlaceMatchesSQL, fragment) {
			t.Fatalf("refresh SQL missing manual match guard %q:\n%s", fragment, refreshRecoveryMeetingPlaceMatchesSQL)
		}
	}
}
