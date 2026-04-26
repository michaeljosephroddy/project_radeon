package meetups

import (
	"math"
	"sort"
	"strings"
	"time"
)

func rankRecommendedCandidates(candidates []recommendedCandidate, viewer viewerContext, now time.Time) []recommendedCandidate {
	if len(candidates) == 0 {
		return candidates
	}
	ranked := make([]recommendedCandidate, len(candidates))
	copy(ranked, candidates)
	for index := range ranked {
		ranked[index].Score = recommendedCandidateScore(ranked[index], viewer, now)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		left := ranked[i]
		right := ranked[j]
		if math.Abs(left.Score-right.Score) > 0.001 {
			return left.Score > right.Score
		}
		if !left.StartsAt.Equal(right.StartsAt) {
			return left.StartsAt.Before(right.StartsAt)
		}
		if !stableRecommendationTime(left.Meetup).Equal(stableRecommendationTime(right.Meetup)) {
			return stableRecommendationTime(left.Meetup).After(stableRecommendationTime(right.Meetup))
		}
		return left.ID.String() < right.ID.String()
	})
	return diversifyRecommendedCandidates(ranked)
}

func recommendedCandidateScore(candidate recommendedCandidate, viewer viewerContext, now time.Time) float64 {
	score := 100.0
	if candidate.DistanceKMComputed != nil {
		score -= math.Min(*candidate.DistanceKMComputed, 120.0) * 0.6
	}
	hoursUntil := candidate.StartsAt.Sub(now).Hours()
	if hoursUntil > 0 {
		score -= math.Min(hoursUntil, 336) * 0.1
	}
	score += math.Min(float64(candidate.AttendeeCt), 24) * 1.15
	if candidate.WaitlistEnabled && candidate.Capacity != nil && candidate.AttendeeCt >= *candidate.Capacity {
		score -= 10
	}
	if candidate.InterestMatched {
		score += 20
	}
	if candidate.EventType == "online" {
		score += 2
	}
	if candidate.FriendAttendeeCount > 0 {
		score += math.Min(float64(candidate.FriendAttendeeCount), 4) * 7
	}
	if metricTotal := candidate.OrganizerPublishedCount + candidate.OrganizerCancelledCount; metricTotal > 0 {
		cancelRate := float64(candidate.OrganizerCancelledCount) / float64(metricTotal)
		score -= cancelRate * 16
		score += math.Min(candidate.OrganizerAverageAudience, 20) * 0.45
		score += math.Min(float64(candidate.OrganizerPublishedCount), 10) * 0.25
	}
	if candidate.SavedCount > 0 {
		score += math.Min(float64(candidate.SavedCount), 15) * 0.35
	}
	if len(candidate.SourceHits) > 1 {
		score += float64(len(candidate.SourceHits)-1) * 2
	}
	if viewer.Interests != nil {
		if _, ok := viewer.Interests[strings.ToLower(candidate.CategoryLabel)]; ok {
			score += 4
		}
	}
	return score
}

func diversifyRecommendedCandidates(candidates []recommendedCandidate) []recommendedCandidate {
	if len(candidates) < 3 {
		return candidates
	}
	remaining := make([]recommendedCandidate, len(candidates))
	copy(remaining, candidates)
	reranked := make([]recommendedCandidate, 0, len(candidates))
	organizerSeen := make(map[string]int, len(candidates))
	categorySeen := make(map[string]int, len(candidates))

	for len(remaining) > 0 {
		bestIndex := 0
		bestAdjusted := adjustedRecommendationScore(remaining[0], organizerSeen, categorySeen)
		for index := 1; index < len(remaining); index++ {
			adjusted := adjustedRecommendationScore(remaining[index], organizerSeen, categorySeen)
			if adjusted > bestAdjusted+0.001 {
				bestAdjusted = adjusted
				bestIndex = index
				continue
			}
			if math.Abs(adjusted-bestAdjusted) <= 0.001 {
				if remaining[index].StartsAt.Before(remaining[bestIndex].StartsAt) {
					bestIndex = index
					bestAdjusted = adjusted
				}
			}
		}
		chosen := remaining[bestIndex]
		reranked = append(reranked, chosen)
		organizerSeen[chosen.OrganizerID.String()]++
		categorySeen[chosen.CategorySlug]++
		remaining = append(remaining[:bestIndex], remaining[bestIndex+1:]...)
	}
	return reranked
}

func adjustedRecommendationScore(candidate recommendedCandidate, organizerSeen, categorySeen map[string]int) float64 {
	score := candidate.Score
	if count := organizerSeen[candidate.OrganizerID.String()]; count > 0 {
		score -= float64(count) * 6
	}
	if count := categorySeen[candidate.CategorySlug]; count > 0 {
		score -= float64(count) * 2.5
	}
	return score
}

func sliceRecommendedMeetups(candidates []recommendedCandidate, limit int, cursor string) *CursorPage[Meetup] {
	if limit < 1 {
		limit = 20
	}
	decoded := decodeRecommendedCursor(cursor)
	startIndex := 0
	if decoded.LastID != "" {
		for index, candidate := range candidates {
			if candidate.ID.String() == decoded.LastID {
				startIndex = index + 1
				break
			}
		}
		if startIndex == 0 && decoded.LastOffset > 0 && decoded.LastOffset < len(candidates) {
			startIndex = decoded.LastOffset
		}
	}
	if startIndex >= len(candidates) {
		return &CursorPage[Meetup]{
			Items:      []Meetup{},
			Limit:      limit,
			HasMore:    false,
			NextCursor: nil,
		}
	}
	end := startIndex + limit
	if end > len(candidates) {
		end = len(candidates)
	}
	items := make([]Meetup, 0, end-startIndex)
	for _, candidate := range candidates[startIndex:end] {
		meetup := candidate.Meetup
		meetup.DistanceKM = candidate.DistanceKMComputed
		items = append(items, meetup)
	}
	hasMore := end < len(candidates)
	var nextCursor *string
	if hasMore && len(items) > 0 {
		nextCursor = encodeRecommendedCursor(items[len(items)-1].ID, end)
	}
	return &CursorPage[Meetup]{
		Items:      items,
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}
}
