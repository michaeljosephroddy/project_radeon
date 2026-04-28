package meetups

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRankRecommendedCandidatesBoostsFriendAttendance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	baseStartsAt := now.Add(24 * time.Hour)
	organizerID := uuid.New()
	viewer := viewerContext{Interests: map[string]struct{}{"coffee": {}}}

	withoutFriends := recommendedCandidate{
		Meetup: Meetup{
			ID:            uuid.New(),
			OrganizerID:   organizerID,
			CategorySlug:  "coffee",
			CategoryLabel: "Coffee",
			Title:         "Quiet coffee walk",
			StartsAt:      baseStartsAt,
			CreatedAt:     now.Add(-2 * time.Hour),
		},
		InterestMatched: true,
	}
	withFriends := withoutFriends
	withFriends.ID = uuid.New()
	withFriends.FriendAttendeeCount = 2

	ranked := rankRecommendedCandidates([]recommendedCandidate{withoutFriends, withFriends}, viewer, now)
	if ranked[0].ID != withFriends.ID {
		t.Fatalf("expected meetup with friend attendance to rank first")
	}
}

func TestDiversifyRecommendedCandidatesSplitsRepeatedOrganizers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	organizerA := uuid.New()
	organizerB := uuid.New()
	candidates := []recommendedCandidate{
		{
			Meetup: Meetup{
				ID:            uuid.New(),
				OrganizerID:   organizerA,
				CategorySlug:  "coffee",
				CategoryLabel: "Coffee",
				Title:         "A1",
				StartsAt:      now.Add(24 * time.Hour),
				CreatedAt:     now,
			},
			Score: 120,
		},
		{
			Meetup: Meetup{
				ID:            uuid.New(),
				OrganizerID:   organizerA,
				CategorySlug:  "coffee",
				CategoryLabel: "Coffee",
				Title:         "A2",
				StartsAt:      now.Add(25 * time.Hour),
				CreatedAt:     now,
			},
			Score: 119.5,
		},
		{
			Meetup: Meetup{
				ID:            uuid.New(),
				OrganizerID:   organizerB,
				CategorySlug:  "walk",
				CategoryLabel: "Walk",
				Title:         "B1",
				StartsAt:      now.Add(26 * time.Hour),
				CreatedAt:     now,
			},
			Score: 118.9,
		},
	}

	reranked := diversifyRecommendedCandidates(candidates)
	if reranked[1].OrganizerID != organizerB {
		t.Fatalf("expected second slot to diversify away from organizer A")
	}
}

func TestSliceRecommendedMeetupsUsesStableLastSeenCursor(t *testing.T) {
	t.Parallel()

	candidates := make([]recommendedCandidate, 0, 3)
	for index := 0; index < 3; index++ {
		candidates = append(candidates, recommendedCandidate{
			Meetup: Meetup{
				ID:        uuid.New(),
				Title:     "Meetup",
				StartsAt:  time.Date(2026, 4, 27+index, 12, 0, 0, 0, time.UTC),
				CreatedAt: time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC),
			},
			Score: float64(100 - index),
		})
	}

	firstPage := sliceRecommendedMeetups(candidates, 2, "")
	if len(firstPage.Items) != 2 || firstPage.NextCursor == nil {
		t.Fatalf("expected first page of 2 items and a next cursor")
	}
	secondPage := sliceRecommendedMeetups(candidates, 2, *firstPage.NextCursor)
	if len(secondPage.Items) != 1 {
		t.Fatalf("expected one remaining item on second page, got %d", len(secondPage.Items))
	}
	if secondPage.Items[0].ID != candidates[2].ID {
		t.Fatalf("expected stable cursor to continue after last seen meetup")
	}
}

func TestRecommendedRankedWindowLimitBucketsAdjacentPages(t *testing.T) {
	t.Parallel()

	first := recommendedRankedWindowLimit(20, "")
	secondCursor := encodeRecommendedCursor(uuid.New(), 20)
	thirdCursor := encodeRecommendedCursor(uuid.New(), 40)
	deeperCursor := encodeRecommendedCursor(uuid.New(), 140)

	second := recommendedRankedWindowLimit(20, *secondCursor)
	third := recommendedRankedWindowLimit(20, *thirdCursor)
	deeper := recommendedRankedWindowLimit(20, *deeperCursor)

	if first != second || second != third {
		t.Fatalf("expected adjacent recommended meetup pages to share a ranked window, got %d, %d, %d", first, second, third)
	}
	if deeper <= third {
		t.Fatalf("expected deeper recommended meetup pages to expand the ranked window, got %d then %d", third, deeper)
	}
}

func TestRecommendedRankedWindowLimitCapsAtCandidatePoolMaximum(t *testing.T) {
	t.Parallel()

	cursor := encodeRecommendedCursor(uuid.New(), 600)
	if cursor == nil {
		t.Fatal("expected cursor to encode")
	}

	got := recommendedRankedWindowLimit(50, *cursor)
	want := recommendedCandidatePoolLimits(50).Total
	if got != want {
		t.Fatalf("recommendedRankedWindowLimit should cap at %d, got %d", want, got)
	}
}
