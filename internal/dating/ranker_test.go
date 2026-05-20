package dating

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/project_radeon/api/internal/user"
)

func TestRankDatingCandidatesPrefersSharedInterests(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	base := baseDatingCandidate(now)
	low := base
	low.User.ID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	low.SharedInterestCount = 0
	high := base
	high.User.ID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	high.SharedInterestCount = 4

	ranked := rankDatingCandidates(datingViewerFeatures{}, []datingCandidate{low, high}, now)
	if ranked[0].User.ID != high.User.ID {
		t.Fatalf("expected shared-interest candidate first, got %s", ranked[0].User.ID)
	}
}

func TestRankDatingCandidatesPrefersRecentActivity(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	recentActiveAt := now.Add(-12 * time.Hour)
	staleActiveAt := now.Add(-120 * 24 * time.Hour)
	recent := baseDatingCandidate(now)
	recent.User.ID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	recent.LastActiveAt = &recentActiveAt
	stale := baseDatingCandidate(now)
	stale.User.ID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	stale.LastActiveAt = &staleActiveAt

	ranked := rankDatingCandidates(datingViewerFeatures{}, []datingCandidate{stale, recent}, now)
	if ranked[0].User.ID != recent.User.ID {
		t.Fatalf("expected recently active candidate first, got %s", ranked[0].User.ID)
	}
}

func TestRankDatingCandidatesPrefersCloserDistance(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	nearDistance := 5.0
	farDistance := 45.0
	near := baseDatingCandidate(now)
	near.User.ID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	near.DistanceKm = &nearDistance
	far := baseDatingCandidate(now)
	far.User.ID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	far.DistanceKm = &farDistance

	ranked := rankDatingCandidates(datingViewerFeatures{}, []datingCandidate{far, near}, now)
	if ranked[0].User.ID != near.User.ID {
		t.Fatalf("expected nearer candidate first, got %s", ranked[0].User.ID)
	}
}

func TestScoreDatingCandidateDoesNotBoostPlus(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	free := baseDatingCandidate(now)
	free.User.IsPlus = false
	plus := baseDatingCandidate(now)
	plus.User.IsPlus = true

	freeScore := scoreDatingCandidate(datingViewerFeatures{}, free, now)
	plusScore := scoreDatingCandidate(datingViewerFeatures{}, plus, now)
	if freeScore != plusScore {
		t.Fatalf("plus score = %f, free score = %f; want equal", plusScore, freeScore)
	}
}

func TestScoreDatingCandidateBoostsIncomingLike(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	base := baseDatingCandidate(now)
	incoming := base
	incoming.IncomingLike = true

	baseScore := scoreDatingCandidate(datingViewerFeatures{}, base, now)
	incomingScore := scoreDatingCandidate(datingViewerFeatures{}, incoming, now)
	if incomingScore <= baseScore {
		t.Fatalf("incoming like score = %f, base score = %f; want incoming higher", incomingScore, baseScore)
	}
}

func TestScoreDatingCandidatePenalizesRecentImpression(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	recentImpressionAt := now.Add(-2 * time.Hour)
	base := baseDatingCandidate(now)
	recent := base
	recent.RecentImpressionAt = &recentImpressionAt

	baseScore := scoreDatingCandidate(datingViewerFeatures{}, base, now)
	recentScore := scoreDatingCandidate(datingViewerFeatures{}, recent, now)
	if recentScore >= baseScore {
		t.Fatalf("recent impression score = %f, base score = %f; want recent lower", recentScore, baseScore)
	}
}

func TestMergeDatingCandidatesDedupesAndKeepsIncomingLike(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	first := baseDatingCandidate(now)
	first.User.ID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	first.Source = "nearby"
	second := first
	second.Source = "incoming_like"
	second.IncomingLike = true

	merged := mergeDatingCandidates([]datingCandidate{first}, []datingCandidate{second})
	if len(merged) != 1 {
		t.Fatalf("merged len = %d, want 1", len(merged))
	}
	if !merged[0].IncomingLike {
		t.Fatal("expected merged candidate to keep incoming-like signal")
	}
	if merged[0].Source != "nearby,incoming_like" {
		t.Fatalf("source = %q, want nearby,incoming_like", merged[0].Source)
	}
}

func baseDatingCandidate(now time.Time) datingCandidate {
	lastActiveAt := now.Add(-48 * time.Hour)
	return datingCandidate{
		User: user.User{
			ID:        uuid.MustParse("99999999-9999-9999-9999-999999999999"),
			Username:  "casey",
			CreatedAt: now.Add(-180 * 24 * time.Hour),
		},
		SharedInterestCount: 0,
		ProfileCompleteness: 8,
		LastActiveAt:        &lastActiveAt,
	}
}
