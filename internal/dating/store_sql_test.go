package dating

import (
	"strings"
	"testing"
)

func TestDatingCandidateDistanceAliasIsUnambiguous(t *testing.T) {
	if !strings.Contains(datingCandidateColumns, " AS candidate_distance_km,") {
		t.Fatal("expected computed dating candidate distance to use candidate_distance_km alias")
	}
	if strings.Contains(datingCandidateColumns, " AS distance_km,") {
		t.Fatal("computed dating candidate distance must not reuse profile distance_km alias")
	}
}
