package support

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSupportFeedSliceEncodesLastVisibleItemCursor(t *testing.T) {
	firstID := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	secondID := uuid.MustParse("00000000-0000-0000-0000-000000000020")
	thirdID := uuid.MustParse("00000000-0000-0000-0000-000000000030")
	servedAt := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)

	items := []SupportRequest{
		{ID: firstID, CreatedAt: time.Date(2026, 4, 28, 9, 0, 0, 0, time.UTC), FeedScore: 300},
		{ID: secondID, CreatedAt: time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC), FeedScore: 250},
		{ID: thirdID, CreatedAt: time.Date(2026, 4, 28, 11, 0, 0, 0, time.UTC), FeedScore: 200},
	}

	page := supportFeedSlice(items, 2, &SupportFeedCursor{ServedAt: servedAt})
	if !page.HasMore {
		t.Fatal("expected has_more true")
	}
	if page.NextCursor == nil {
		t.Fatal("expected next cursor")
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}

	cursor, err := parseSupportFeedCursor(*page.NextCursor)
	if err != nil {
		t.Fatalf("parse next cursor: %v", err)
	}
	if cursor == nil {
		t.Fatal("expected decoded cursor")
	}
	if cursor.ID != secondID || cursor.Score != 250 || !cursor.ServedAt.Equal(servedAt) {
		t.Fatalf("cursor = %#v, want second item values", cursor)
	}
}
