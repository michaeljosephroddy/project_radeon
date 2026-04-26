package user

import (
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
)

const (
	discoverSourceNearby   = "nearby"
	discoverSourceMutual   = "mutual"
	discoverSourceInterest = "interests"
	discoverSourceSobriety = "sobriety"
	discoverSourceActive   = "active"
)

const (
	discoverDismissalCooldown  = 14 * 24 * time.Hour
	discoverImpressionCooldown = 45 * time.Minute
)

type discoverViewerFeatures struct {
	UserID       uuid.UUID
	InterestIDs  []uuid.UUID
	SobrietyBand *int
	Lat          *float64
	Lng          *float64
}

type discoverCandidate struct {
	ID                  uuid.UUID
	DistanceKm          *float64
	SharedInterestCount int
	MutualFriendCount   int
	SobrietyBand        *int
	LastActiveAt        *time.Time
	ProfileCompleteness int
	Sources             []string
	Score               float64
}

type discoverBounds struct {
	MinLat *float64
	MaxLat *float64
	MinLng *float64
	MaxLng *float64
}

type discoverFilters struct {
	SobrietyMinBand *int
	Bounds          discoverBounds
}

func sobrietyMinimumBand(raw string) *int {
	switch raw {
	case "days_30":
		value := 2
		return &value
	case "days_90":
		value := 3
		return &value
	case "years_1":
		value := 4
		return &value
	case "years_5":
		value := 6
		return &value
	default:
		return nil
	}
}

func buildDiscoverFilters(params DiscoverUsersParams) discoverFilters {
	return discoverFilters{
		SobrietyMinBand: sobrietyMinimumBand(params.Sobriety),
		Bounds:          computeDiscoverBounds(params.Lat, params.Lng, params.DistanceKm),
	}
}

func computeDiscoverBounds(lat, lng *float64, distanceKm *int) discoverBounds {
	if lat == nil || lng == nil || distanceKm == nil || *distanceKm <= 0 {
		return discoverBounds{}
	}

	latDelta := float64(*distanceKm) / 111.32
	if latDelta <= 0 {
		return discoverBounds{}
	}
	lngDenominator := 111.32 * math.Cos(*lat*math.Pi/180.0)
	if lngDenominator == 0 {
		return discoverBounds{}
	}
	lngDelta := float64(*distanceKm) / math.Abs(lngDenominator)

	minLat := *lat - latDelta
	maxLat := *lat + latDelta
	minLng := *lng - lngDelta
	maxLng := *lng + lngDelta

	return discoverBounds{
		MinLat: &minLat,
		MaxLat: &maxLat,
		MinLng: &minLng,
		MaxLng: &maxLng,
	}
}

func discoverCandidatePoolLimit(params DiscoverUsersParams) int {
	target := (params.Offset + params.Limit) * 5
	if target < 100 {
		target = 100
	}
	if target > 300 {
		target = 300
	}
	return target
}

func discoverVisibleLimit(params DiscoverUsersParams) int {
	if params.DisplayLimit > 0 && params.DisplayLimit <= params.Limit {
		return params.DisplayLimit
	}
	if params.DisplayLimit > params.Limit && params.Limit > 0 {
		return params.Limit
	}
	return params.Limit
}

func shouldApplyDiscoverImpressionSuppression(params DiscoverUsersParams) bool {
	return params.Offset == 0
}

func discoverPrimarySource(candidate discoverCandidate) string {
	priority := []string{
		discoverSourceMutual,
		discoverSourceInterest,
		discoverSourceNearby,
		discoverSourceSobriety,
		discoverSourceActive,
	}
	for _, source := range priority {
		if slices.Contains(candidate.Sources, source) {
			return source
		}
	}
	if len(candidate.Sources) == 0 {
		return discoverSourceActive
	}
	return candidate.Sources[0]
}

func mergeDiscoverCandidates(groups ...[]discoverCandidate) []discoverCandidate {
	merged := make(map[uuid.UUID]discoverCandidate)
	order := make([]uuid.UUID, 0)

	for _, group := range groups {
		for _, candidate := range group {
			existing, found := merged[candidate.ID]
			if !found {
				candidate.Sources = dedupeSources(candidate.Sources)
				merged[candidate.ID] = candidate
				order = append(order, candidate.ID)
				continue
			}

			existing.SharedInterestCount = maxInt(existing.SharedInterestCount, candidate.SharedInterestCount)
			existing.MutualFriendCount = maxInt(existing.MutualFriendCount, candidate.MutualFriendCount)
			existing.ProfileCompleteness = maxInt(existing.ProfileCompleteness, candidate.ProfileCompleteness)
			existing.SobrietyBand = preferBand(existing.SobrietyBand, candidate.SobrietyBand)
			existing.LastActiveAt = preferTime(existing.LastActiveAt, candidate.LastActiveAt)
			existing.DistanceKm = preferDistance(existing.DistanceKm, candidate.DistanceKm)
			existing.Sources = dedupeSources(append(existing.Sources, candidate.Sources...))
			merged[candidate.ID] = existing
		}
	}

	result := make([]discoverCandidate, 0, len(order))
	for _, id := range order {
		result = append(result, merged[id])
	}
	return result
}

func dedupeSources(sources []string) []string {
	if len(sources) <= 1 {
		return sources
	}
	result := make([]string, 0, len(sources))
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if source == "" {
			continue
		}
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		result = append(result, source)
	}
	return result
}

func filterDiscoverSuppressedCandidates(candidates []discoverCandidate, impressions map[uuid.UUID]time.Time, now time.Time) ([]discoverCandidate, bool) {
	if len(candidates) == 0 || len(impressions) == 0 {
		return candidates, false
	}

	filtered := make([]discoverCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		shownAt, found := impressions[candidate.ID]
		if found && now.Sub(shownAt) < discoverImpressionCooldown {
			continue
		}
		filtered = append(filtered, candidate)
	}

	return filtered, len(filtered) != len(candidates)
}

func preferBand(left, right *int) *int {
	if left != nil {
		return left
	}
	return right
}

func preferTime(left, right *time.Time) *time.Time {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	if right.After(*left) {
		return right
	}
	return left
}

func preferDistance(left, right *float64) *float64 {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	if *right < *left {
		return right
	}
	return left
}

func maxInt(left, right int) int {
	if right > left {
		return right
	}
	return left
}
