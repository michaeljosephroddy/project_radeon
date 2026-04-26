package user

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMergeDiscoverCandidatesDeduplicatesAndPreservesSignals(t *testing.T) {
	t.Parallel()

	candidateID := uuid.New()
	now := time.Now().UTC()
	distance := 12.0
	sobrietyBand := 4

	merged := mergeDiscoverCandidates(
		[]discoverCandidate{{
			ID:                  candidateID,
			DistanceKm:          &distance,
			MutualFriendCount:   2,
			ProfileCompleteness: 5,
			SobrietyBand:        &sobrietyBand,
			LastActiveAt:        &now,
			Sources:             []string{discoverSourceMutual},
		}},
		[]discoverCandidate{{
			ID:                  candidateID,
			SharedInterestCount: 3,
			ProfileCompleteness: 7,
			Sources:             []string{discoverSourceInterest},
		}},
	)
	if len(merged) != 1 {
		t.Fatalf("expected one merged candidate, got %d", len(merged))
	}
	if merged[0].MutualFriendCount != 2 {
		t.Fatalf("expected mutual count to be preserved, got %d", merged[0].MutualFriendCount)
	}
	if merged[0].SharedInterestCount != 3 {
		t.Fatalf("expected interest count to be merged, got %d", merged[0].SharedInterestCount)
	}
	if merged[0].ProfileCompleteness != 7 {
		t.Fatalf("expected highest completeness to win, got %d", merged[0].ProfileCompleteness)
	}
	if len(merged[0].Sources) != 2 {
		t.Fatalf("expected source deduplication to keep both sources, got %v", merged[0].Sources)
	}
}

func TestFilterDiscoverSuppressedCandidatesDropsRecentImpressions(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	recentID := uuid.New()
	staleID := uuid.New()
	candidates := []discoverCandidate{
		{ID: recentID},
		{ID: staleID},
	}

	filtered, changed := filterDiscoverSuppressedCandidates(candidates, map[uuid.UUID]time.Time{
		recentID: now.Add(-10 * time.Minute),
		staleID:  now.Add(-2 * time.Hour),
	}, now)
	if !changed {
		t.Fatal("expected recent impression suppression to report a change")
	}
	if len(filtered) != 1 || filtered[0].ID != staleID {
		t.Fatalf("expected only the stale candidate to survive, got %+v", filtered)
	}
}

func TestRerankDiscoverCandidatesAvoidsImmediateSourceRepeatsWhenPossible(t *testing.T) {
	t.Parallel()

	candidates := []discoverCandidate{
		{ID: uuid.New(), Score: 0.95, Sources: []string{discoverSourceMutual}},
		{ID: uuid.New(), Score: 0.90, Sources: []string{discoverSourceMutual}},
		{ID: uuid.New(), Score: 0.89, Sources: []string{discoverSourceInterest}},
	}

	reranked := rerankDiscoverCandidates(candidates)
	if len(reranked) != len(candidates) {
		t.Fatalf("expected reranker to preserve candidate count, got %d", len(reranked))
	}
	if discoverPrimarySource(reranked[0]) == discoverPrimarySource(reranked[1]) {
		t.Fatalf("expected reranker to interleave sources when possible, got %v then %v", reranked[0].Sources, reranked[1].Sources)
	}
}
