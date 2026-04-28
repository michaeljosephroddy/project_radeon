package meetups

import (
	"encoding/base64"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"
)

const recommendedPipelineVersion = "v2"
const recommendedRankedWindowPages = 4

type recommendedSource string

const (
	recommendedSourceNearby   recommendedSource = "nearby"
	recommendedSourceInterest recommendedSource = "interest"
	recommendedSourceSocial   recommendedSource = "social"
	recommendedSourcePopular  recommendedSource = "popular"
)

type recommendedCandidate struct {
	Meetup
	DistanceKMComputed       *float64
	FriendAttendeeCount      int
	OrganizerPublishedCount  int
	OrganizerCancelledCount  int
	OrganizerAverageAudience float64
	InterestMatched          bool
	SourceHits               map[recommendedSource]struct{}
	Score                    float64
}

type recommendedCursor struct {
	Version    string `json:"v"`
	LastID     string `json:"last_id,omitempty"`
	LastOffset int    `json:"last_offset,omitempty"`
}

type organizerRecommendationMetrics struct {
	PublishedCount  int
	CancelledCount  int
	AverageAudience float64
}

type recommendationFeatureSet struct {
	FriendAttendeeCount map[uuid.UUID]int
	OrganizerMetrics    map[uuid.UUID]organizerRecommendationMetrics
}

type candidatePoolLimits struct {
	PerSource int
	Total     int
}

func recommendedCandidatePoolLimits(pageLimit int) candidatePoolLimits {
	perSource := pageLimit * 6
	if perSource < 60 {
		perSource = 60
	}
	if perSource > 180 {
		perSource = 180
	}
	total := perSource * 4
	if total < 180 {
		total = 180
	}
	if total > 480 {
		total = 480
	}
	return candidatePoolLimits{PerSource: perSource, Total: total}
}

func recommendedRankedWindowLimit(limit int, cursor string) int {
	if limit < 1 {
		limit = 20
	}

	decoded := decodeRecommendedCursor(cursor)
	total := limit
	if decoded.LastOffset > 0 {
		total = decoded.LastOffset + limit
	}

	windowSize := limit * recommendedRankedWindowPages
	limits := recommendedCandidatePoolLimits(limit)
	if windowSize < limits.PerSource {
		windowSize = limits.PerSource
	}
	if windowSize > limits.Total {
		windowSize = limits.Total
	}
	if total > limits.Total {
		return limits.Total
	}

	windows := (total + windowSize - 1) / windowSize
	return windows * windowSize
}

func encodeRecommendedCursor(lastID uuid.UUID, lastOffset int) *string {
	if lastID == uuid.Nil {
		return nil
	}
	payload, err := json.Marshal(recommendedCursor{
		Version:    recommendedPipelineVersion,
		LastID:     lastID.String(),
		LastOffset: lastOffset,
	})
	if err != nil {
		return nil
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return &encoded
}

func decodeRecommendedCursor(cursor string) recommendedCursor {
	if cursor == "" {
		return recommendedCursor{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return recommendedCursor{}
	}
	var decoded recommendedCursor
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return recommendedCursor{}
	}
	if decoded.Version != recommendedPipelineVersion {
		return recommendedCursor{}
	}
	return decoded
}

func stableRecommendationTime(meetup Meetup) time.Time {
	if meetup.PublishedAt != nil {
		return meetup.PublishedAt.UTC()
	}
	return meetup.CreatedAt.UTC()
}

func sortedInterestNames(interests map[string]struct{}) []string {
	if len(interests) == 0 {
		return nil
	}
	names := make([]string, 0, len(interests))
	for name := range interests {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
