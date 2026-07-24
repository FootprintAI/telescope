package model

import (
	"testing"
	"time"
)

func TestRunningHours(t *testing.T) {
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		created time.Time
		want    float64
	}{
		{"unknown", time.Time{}, 0},
		{"30 days", now.Add(-720 * time.Hour), 720},
		{"one hour", now.Add(-time.Hour), 1},
		{"future clamps to zero", now.Add(time.Hour), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := Instance{CreatedAt: tc.created}
			if got := in.RunningHours(now); got != tc.want {
				t.Errorf("RunningHours = %v, want %v", got, tc.want)
			}
		})
	}
}
