package support

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSupportAttentionBucket(t *testing.T) {
	now := time.Date(2026, 4, 28, 15, 0, 0, 0, time.UTC)
	recentImmediate := now.Add(-5 * time.Minute)
	staleImmediate := now.Add(-15 * time.Minute)
	recentGeneral := now.Add(-20 * time.Minute)
	staleGeneral := now.Add(-2 * time.Hour)

	tests := []struct {
		name         string
		channel      SupportChannel
		hasResponded bool
		responseCount int
		lastResponse *time.Time
		want         int
	}{
		{name: "viewer responded sinks request", channel: SupportChannelImmediate, hasResponded: true, responseCount: 0, want: 3},
		{name: "unanswered immediate request leads", channel: SupportChannelImmediate, responseCount: 0, want: 0},
		{name: "recently answered immediate request cools", channel: SupportChannelImmediate, responseCount: 1, lastResponse: &recentImmediate, want: 2},
		{name: "stale immediate request resurfaces", channel: SupportChannelImmediate, responseCount: 1, lastResponse: &staleImmediate, want: 1},
		{name: "heavy response saturation cools", channel: SupportChannelImmediate, responseCount: 3, lastResponse: &staleImmediate, want: 2},
		{name: "recently answered general request cools", channel: SupportChannelCommunity, responseCount: 1, lastResponse: &recentGeneral, want: 2},
		{name: "stale general request resurfaces later", channel: SupportChannelCommunity, responseCount: 1, lastResponse: &staleGeneral, want: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := supportAttentionBucket(tc.channel, tc.hasResponded, tc.responseCount, tc.lastResponse, now)
			if got != tc.want {
				t.Fatalf("bucket = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSupportQueueSliceEncodesLastVisibleItemCursor(t *testing.T) {
	firstID := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	secondID := uuid.MustParse("00000000-0000-0000-0000-000000000020")
	thirdID := uuid.MustParse("00000000-0000-0000-0000-000000000030")

	items := []SupportRequest{
		{ID: firstID, CreatedAt: time.Date(2026, 4, 28, 9, 0, 0, 0, time.UTC), AttentionBucket: 0, UrgencyRank: 0},
		{ID: secondID, CreatedAt: time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC), AttentionBucket: 1, UrgencyRank: 1},
		{ID: thirdID, CreatedAt: time.Date(2026, 4, 28, 11, 0, 0, 0, time.UTC), AttentionBucket: 2, UrgencyRank: 2},
	}

	page := supportQueueSlice(items, 2)
	if !page.HasMore {
		t.Fatal("expected has_more true")
	}
	if page.NextCursor == nil {
		t.Fatal("expected next cursor")
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}

	cursor, err := parseSupportQueueCursor(*page.NextCursor)
	if err != nil {
		t.Fatalf("parse next cursor: %v", err)
	}
	if cursor == nil {
		t.Fatal("expected decoded cursor")
	}
	if cursor.ID != secondID || cursor.AttentionBucket != 1 || cursor.UrgencyRank != 1 {
		t.Fatalf("cursor = %#v, want second item values", cursor)
	}
}
