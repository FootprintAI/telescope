package gcp

import (
	"testing"
	"time"
)

func TestParseCreationTimestamp(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want time.Time
	}{
		{"empty", "", time.Time{}},
		{"malformed", "not-a-timestamp", time.Time{}},
		{"rfc3339 with offset", "2023-01-15T12:00:00-08:00", time.Date(2023, 1, 15, 12, 0, 0, 0, time.FixedZone("", -8*3600))},
		{"rfc3339 utc", "2024-06-01T00:00:00Z", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseCreationTimestamp(tc.in)
			if !got.Equal(tc.want) {
				t.Errorf("parseCreationTimestamp(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
