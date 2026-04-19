package pagination

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	req := httptest.NewRequest("GET", "/?page=0&limit=999", nil)
	params := Parse(req, 20, 50)

	if params.Page != 1 || params.Limit != 50 || params.Offset != 0 {
		t.Fatalf("unexpected params: %+v", params)
	}
}

func TestSlice(t *testing.T) {
	resp := Slice([]int{1, 2, 3}, Params{Page: 2, Limit: 2})

	if !resp.HasMore {
		t.Fatal("expected HasMore to be true")
	}
	if len(resp.Items) != 2 || resp.Items[0] != 1 || resp.Items[1] != 2 {
		t.Fatalf("unexpected items: %+v", resp.Items)
	}
}

func TestParseCursor(t *testing.T) {
	before := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	req := httptest.NewRequest("GET", "/?limit=999&before="+before+"&after=invalid", nil)
	params := ParseCursor(req, 20, 50)

	if params.Limit != 50 {
		t.Fatalf("Limit = %d, want 50", params.Limit)
	}
	if params.Before == nil || params.Before.Format(time.RFC3339Nano) != before {
		t.Fatalf("unexpected Before: %v", params.Before)
	}
	if params.After != nil {
		t.Fatalf("expected After to be nil, got %v", params.After)
	}
}

func TestCursorSlice(t *testing.T) {
	type item struct {
		CreatedAt time.Time
	}

	items := []item{
		{CreatedAt: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)},
		{CreatedAt: time.Date(2026, 4, 19, 9, 0, 0, 0, time.UTC)},
		{CreatedAt: time.Date(2026, 4, 19, 8, 0, 0, 0, time.UTC)},
	}

	resp := CursorSlice(items, 2, func(v item) time.Time { return v.CreatedAt })

	if !resp.HasMore {
		t.Fatal("expected HasMore to be true")
	}
	if len(resp.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(resp.Items))
	}
	if resp.NextCursor == nil || *resp.NextCursor != items[1].CreatedAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected NextCursor: %v", resp.NextCursor)
	}
}
