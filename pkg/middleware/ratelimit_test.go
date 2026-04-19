package middleware

import (
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestLimiterStoreReturnsSameLimiterForSameKey(t *testing.T) {
	store := newLimiterStore(rate.Every(time.Second), 1)

	first := store.get("same")
	second := store.get("same")
	other := store.get("other")

	if first != second {
		t.Fatal("expected same key to return same limiter instance")
	}
	if first == other {
		t.Fatal("expected different keys to return different limiter instances")
	}
}

func TestLimiterStoreRespectsBurst(t *testing.T) {
	store := newLimiterStore(rate.Every(time.Hour), 1)
	limiter := store.get("same")

	if !limiter.Allow() {
		t.Fatal("expected first request to be allowed")
	}
	if limiter.Allow() {
		t.Fatal("expected second immediate request to be rejected")
	}
}
