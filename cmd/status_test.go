package cmd

import (
	"testing"
	"time"
)

func TestFetchAge(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	for _, tt := range []struct {
		ago   time.Duration
		want  string
		stale bool
	}{
		{-time.Minute, "0m", false}, // clock skew
		{30 * time.Second, "0m", false},
		{23 * time.Minute, "23m", false},
		{59 * time.Minute, "59m", false},
		{time.Hour, "1h", false},
		{7*time.Hour + 50*time.Minute, "7h", false},
		{day, "1d", false},
		{29 * day, "29d", false},
		{30 * day, "30d", true},
		{400 * day, "400d", true},
	} {
		got, stale := fetchAge(now.Add(-tt.ago), now)
		if got != tt.want || stale != tt.stale {
			t.Errorf("fetchAge(%v ago) = %q, %v; want %q, %v", tt.ago, got, stale, tt.want, tt.stale)
		}
	}
	if got, stale := fetchAge(time.Time{}, now); got != "never" || !stale {
		t.Errorf("fetchAge(zero) = %q, %v; want never, true", got, stale)
	}
}
