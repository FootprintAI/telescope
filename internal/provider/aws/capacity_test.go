package aws

import "testing"

func TestParseNetworkPerformance(t *testing.T) {
	cases := map[string]float64{
		"Up to 25 Gigabit": 25,
		"10 Gigabit":       10,
		"100 Gigabit":      100,
		"High":             5,
		"Moderate":         1,
		"Low":              0.5,
		"":                 0,
	}
	for in, want := range cases {
		if got := parseNetworkPerformance(in); got != want {
			t.Errorf("parseNetworkPerformance(%q)=%v want %v", in, got, want)
		}
	}
}

func TestEBSIOPSCeiling(t *testing.T) {
	if got := ebsIOPSCeiling("gp3", 100, 3000); got != 3000 {
		t.Errorf("gp3 = %v want 3000 (provisioned)", got)
	}
	if got := ebsIOPSCeiling("io2", 500, 20000); got != 20000 {
		t.Errorf("io2 = %v want 20000", got)
	}
	if got := ebsIOPSCeiling("gp2", 100, 0); got != 300 {
		t.Errorf("gp2 100GB = %v want 300", got)
	}
	if got := ebsIOPSCeiling("gp2", 10, 0); got != 100 {
		t.Errorf("gp2 10GB = %v want 100 (floor)", got)
	}
	if got := ebsIOPSCeiling("gp2", 100000, 0); got != 16000 {
		t.Errorf("gp2 cap = %v want 16000", got)
	}
	if got := ebsIOPSCeiling("st1", 1000, 0); got != 0 {
		t.Errorf("st1 = %v want 0 (throughput type)", got)
	}
}
