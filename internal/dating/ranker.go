package dating

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	datingDistanceWeight     = 0.30
	datingActivityWeight     = 0.20
	datingInterestWeight     = 0.18
	datingIncomingLikeWeight = 0.15
	datingCompletenessWeight = 0.07
	datingSobrietyWeight     = 0.05
	datingFreshnessWeight    = 0.05
	datingRankedWindowMax    = 500
	datingRankedWindowMin    = 100
	datingSourceWindowMin    = 60
	datingSourceWindowMax    = 150
	datingImpressionCooldown = 72 * time.Hour
)

type datingViewerFeatures struct {
	UserID       uuid.UUID
	SobrietyBand *int
}

type datingCandidate struct {
	Profile             DatingProfile
	DistanceKm          *float64
	SharedInterestCount int
	SobrietyBand        *int
	ProfileCompleteness int
	LastActiveAt        *time.Time
	RecentImpressionAt  *time.Time
	IncomingLike        bool
	Source              string
	Score               float64
}

func datingRankedWindowLimit(params DiscoverParams) int {
	if params.RankedWindowLimit > 0 {
		if params.RankedWindowLimit > datingRankedWindowMax {
			return datingRankedWindowMax
		}
		return params.RankedWindowLimit
	}
	offset := params.CursorOffset
	if offset == 0 {
		offset = parseOffset(params.Cursor)
	}
	target := (offset + params.Limit + 1) * 5
	if target < datingRankedWindowMin {
		target = datingRankedWindowMin
	}
	if target > datingRankedWindowMax {
		target = datingRankedWindowMax
	}
	return target
}

func datingSourceWindowLimit(params DiscoverParams) int {
	if params.SourceWindowLimit > 0 {
		if params.SourceWindowLimit > datingSourceWindowMax {
			return datingSourceWindowMax
		}
		return params.SourceWindowLimit
	}
	offset := params.CursorOffset
	if offset == 0 {
		offset = parseOffset(params.Cursor)
	}
	target := (offset + params.Limit + 1) * 4
	if target < datingSourceWindowMin {
		target = datingSourceWindowMin
	}
	if target > datingSourceWindowMax {
		target = datingSourceWindowMax
	}
	return target
}

func mergeDatingCandidates(groups ...[]datingCandidate) []datingCandidate {
	merged := make([]datingCandidate, 0, datingRankedWindowMax)
	seen := make(map[uuid.UUID]int)
	for _, group := range groups {
		for _, candidate := range group {
			if index, ok := seen[candidate.Profile.UserID]; ok {
				merged[index] = mergeDatingCandidateFeatures(merged[index], candidate)
				continue
			}
			if len(merged) >= datingRankedWindowMax {
				continue
			}
			seen[candidate.Profile.UserID] = len(merged)
			merged = append(merged, candidate)
		}
	}
	return merged
}

func mergeDatingCandidateFeatures(existing, incoming datingCandidate) datingCandidate {
	if incoming.IncomingLike {
		existing.IncomingLike = true
	}
	if existing.Source == "" {
		existing.Source = incoming.Source
	} else if incoming.Source != "" && !strings.Contains(existing.Source, incoming.Source) {
		existing.Source += "," + incoming.Source
	}
	if existing.RecentImpressionAt == nil || (incoming.RecentImpressionAt != nil && incoming.RecentImpressionAt.After(*existing.RecentImpressionAt)) {
		existing.RecentImpressionAt = incoming.RecentImpressionAt
	}
	return existing
}

func rankDatingCandidates(viewer datingViewerFeatures, candidates []datingCandidate, now time.Time) []datingCandidate {
	for index := range candidates {
		candidates[index].Score = scoreDatingCandidate(viewer, candidates[index], now)
	}

	sort.SliceStable(candidates, func(left, right int) bool {
		if math.Abs(candidates[left].Score-candidates[right].Score) > 0.0001 {
			return candidates[left].Score > candidates[right].Score
		}
		if candidates[left].LastActiveAt != nil && candidates[right].LastActiveAt != nil && !candidates[left].LastActiveAt.Equal(*candidates[right].LastActiveAt) {
			return candidates[left].LastActiveAt.After(*candidates[right].LastActiveAt)
		}
		if candidates[left].LastActiveAt != nil && candidates[right].LastActiveAt == nil {
			return true
		}
		if candidates[left].LastActiveAt == nil && candidates[right].LastActiveAt != nil {
			return false
		}
		return candidates[left].Profile.UserID.String() > candidates[right].Profile.UserID.String()
	})

	return candidates
}

func scoreDatingCandidate(viewer datingViewerFeatures, candidate datingCandidate, now time.Time) float64 {
	score := 0.0
	if candidate.DistanceKm != nil {
		score += datingDistanceWeight * math.Exp(-(*candidate.DistanceKm / 50.0))
	}
	score += datingActivityWeight * datingActivityScore(candidate.LastActiveAt, now)
	score += datingInterestWeight * minDatingFloat(float64(candidate.SharedInterestCount)/4.0, 1.0)
	if candidate.IncomingLike {
		score += datingIncomingLikeWeight
	}
	score += datingCompletenessWeight * minDatingFloat(float64(candidate.ProfileCompleteness)/8.0, 1.0)
	score += datingSobrietyWeight * datingSobrietyScore(viewer.SobrietyBand, candidate.SobrietyBand)
	score += datingFreshnessWeight * datingFreshnessScore(candidate.Profile.CreatedAt, now)
	score *= datingRecentImpressionMultiplier(candidate.RecentImpressionAt, now)
	return score
}

func datingActivityScore(lastActiveAt *time.Time, now time.Time) float64 {
	if lastActiveAt == nil {
		return 0
	}
	age := now.Sub(*lastActiveAt)
	switch {
	case age <= 24*time.Hour:
		return 1
	case age <= 7*24*time.Hour:
		return 0.75
	case age <= 30*24*time.Hour:
		return 0.5
	case age <= 90*24*time.Hour:
		return 0.25
	default:
		return 0
	}
}

func datingSobrietyScore(viewerBand, candidateBand *int) float64 {
	if viewerBand == nil || candidateBand == nil {
		return 0
	}
	diff := *viewerBand - *candidateBand
	if diff < 0 {
		diff = -diff
	}
	switch diff {
	case 0:
		return 1
	case 1:
		return 0.5
	default:
		return 0
	}
}

func datingFreshnessScore(createdAt time.Time, now time.Time) float64 {
	age := now.Sub(createdAt)
	switch {
	case age < 0:
		return 0
	case age <= 14*24*time.Hour:
		return 1
	case age <= 45*24*time.Hour:
		return 0.5
	default:
		return 0
	}
}

func datingRecentImpressionMultiplier(recentImpressionAt *time.Time, now time.Time) float64 {
	if recentImpressionAt == nil {
		return 1
	}
	age := now.Sub(*recentImpressionAt)
	switch {
	case age < 0:
		return 0.5
	case age <= datingImpressionCooldown:
		return 0.55
	case age <= 14*24*time.Hour:
		return 0.8
	default:
		return 1
	}
}

func minDatingFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}
