package api

import (
	"testing"
	"time"
)

func TestTokenCache_ReusesUntilExpiry(t *testing.T) {
	calls := 0
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	c := newTokenCache(func() (string, error) { calls++; return "tok", nil }, 50*time.Minute)
	c.now = func() time.Time { return now }

	for i := 0; i < 26; i++ {
		if _, err := c.Get(); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Errorf("expected 1 fetch for 26 requests, got %d", calls)
	}

	now = now.Add(51 * time.Minute)
	_, _ = c.Get()
	if calls != 2 {
		t.Errorf("expected refetch after ttl, got %d fetches", calls)
	}

	c.Invalidate()
	_, _ = c.Get()
	if calls != 3 {
		t.Errorf("expected refetch after invalidate, got %d fetches", calls)
	}
}
