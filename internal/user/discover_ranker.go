package user

import (
	"math"
	"slices"
	"time"
)

const (
	discoverDistanceWeight      = 0.30
	discoverMutualWeight        = 0.22
	discoverInterestWeight      = 0.20
	discoverSobrietyWeight      = 0.12
	discoverActivityWeight      = 0.10
	discoverCompletenessWeight  = 0.06
	discoverFreshnessPenalty    = 0.08
	discoverNearbySourceBonus   = 0.08
	discoverMutualSourceBonus   = 0.16
	discoverInterestSourceBonus = 0.12
	discoverSobrietySourceBonus = 0.08
	discoverActiveSourceBonus   = 0.04
)

func scoreDiscoverCandidate(viewer discoverViewerFeatures, candidate discoverCandidate, now time.Time) float64 {
	score := 0.0

	if candidate.DistanceKm != nil && viewer.Lat != nil && viewer.Lng != nil {
		score += discoverDistanceWeight * math.Exp(-(*candidate.DistanceKm / 50.0))
	}

	if candidate.MutualFriendCount > 0 {
		score += discoverMutualWeight * minFloat(float64(candidate.MutualFriendCount)/5.0, 1.0)
	}

	if candidate.SharedInterestCount > 0 {
		score += discoverInterestWeight * minFloat(float64(candidate.SharedInterestCount)/4.0, 1.0)
	}

	if viewer.SobrietyBand != nil && candidate.SobrietyBand != nil {
		switch diff := absInt(*viewer.SobrietyBand - *candidate.SobrietyBand); diff {
		case 0:
			score += discoverSobrietyWeight
		case 1:
			score += discoverSobrietyWeight / 2
		}
	}

	if candidate.LastActiveAt != nil {
		age := now.Sub(*candidate.LastActiveAt)
		switch {
		case age <= 7*24*time.Hour:
			score += discoverActivityWeight
		case age <= 30*24*time.Hour:
			score += discoverActivityWeight / 2
		case age > 180*24*time.Hour:
			score -= discoverFreshnessPenalty
		}
	}

	score += discoverCompletenessWeight * minFloat(float64(candidate.ProfileCompleteness)/8.0, 1.0)

	if slices.Contains(candidate.Sources, discoverSourceNearby) {
		score += discoverNearbySourceBonus
	}
	if slices.Contains(candidate.Sources, discoverSourceMutual) {
		score += discoverMutualSourceBonus
	}
	if slices.Contains(candidate.Sources, discoverSourceInterest) {
		score += discoverInterestSourceBonus
	}
	if slices.Contains(candidate.Sources, discoverSourceSobriety) {
		score += discoverSobrietySourceBonus
	}
	if slices.Contains(candidate.Sources, discoverSourceActive) {
		score += discoverActiveSourceBonus
	}

	return score
}

func rerankDiscoverCandidates(candidates []discoverCandidate) []discoverCandidate {
	if len(candidates) < 3 {
		return candidates
	}

	remaining := make([]discoverCandidate, len(candidates))
	copy(remaining, candidates)

	result := make([]discoverCandidate, 0, len(candidates))
	lastSource := ""
	for len(remaining) > 0 {
		nextIndex := 0
		if lastSource != "" {
			for index, candidate := range remaining {
				if discoverPrimarySource(candidate) != lastSource {
					nextIndex = index
					break
				}
			}
		}

		chosen := remaining[nextIndex]
		result = append(result, chosen)
		lastSource = discoverPrimarySource(chosen)
		remaining = append(remaining[:nextIndex], remaining[nextIndex+1:]...)
	}

	return result
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
