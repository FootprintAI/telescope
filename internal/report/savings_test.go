package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/footprintai/telescope/internal/savings"
)

func f64(v float64) *float64 { return &v }

func TestMoney0(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "$0"},
		{489.1, "$489"},
		{1306.7, "$1,307"},
		{1234567, "$1,234,567"},
	}
	for _, tc := range tests {
		if got := money0(tc.in); got != tc.want {
			t.Errorf("money0(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSavingsHeadlineWithPricing(t *testing.T) {
	s := &savings.Score{
		RecoverableMonthlyUSD: f64(489.1),
		UnderutilizedSpendPct: f64(37.0),
		Basis:                 savings.Basis{AlwaysOnInstances: 5, InstanceCount: 6},
	}
	got := savingsHeadline(s)
	for _, want := range []string{"~$489/mo estimated recoverable", "37% of spend under-utilized", "5 of 6 instances always-on"} {
		if !strings.Contains(got, want) {
			t.Errorf("headline %q missing %q", got, want)
		}
	}
}

func TestSavingsHeadlineWithoutPricing(t *testing.T) {
	s := &savings.Score{
		UnderutilizedInstancePct: 33.0,
		Basis:                    savings.Basis{AlwaysOnInstances: 5, InstanceCount: 6},
	}
	got := savingsHeadline(s)
	if !strings.Contains(got, "run with --pricing") {
		t.Errorf("no-pricing headline must hint at --pricing; got %q", got)
	}
	if strings.Contains(got, "$") {
		t.Errorf("no-pricing headline must not show dollars; got %q", got)
	}
}

// The headline must appear in the table output ahead of the instance rows.
func TestRenderTableIncludesHeadline(t *testing.T) {
	rep := buildFixture(nil)
	var buf bytes.Buffer
	RenderTable(&buf, rep)
	out := buf.String()
	if !strings.Contains(out, "Savings Score:") {
		t.Errorf("table output missing Savings Score headline:\n%s", out)
	}
	if idx := strings.Index(out, "Savings Score:"); idx > strings.Index(out, "NAME") {
		t.Error("Savings Score headline must precede the instance table")
	}
}
