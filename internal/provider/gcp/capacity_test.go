package gcp

import "testing"

func TestFamilyOf(t *testing.T) {
	cases := map[string]string{
		"n2d-highmem-8":     "n2d",
		"e2-custom-4-8192":  "e2",
		"n1-standard-4":     "n1",
		"c3-standard-88":    "c3",
		"unknowntype":       "unknowntype",
	}
	for in, want := range cases {
		if got := familyOf(in); got != want {
			t.Errorf("familyOf(%q)=%q want %q", in, got, want)
		}
	}
}

func TestNICGbps(t *testing.T) {
	// e2 caps at 16 even though 16*2=32.
	if got := nicGbps("e2-standard-16", 16); got != 16 {
		t.Errorf("e2-standard-16 nic = %v want 16", got)
	}
	// small n2 scales at 2/vCPU below the cap.
	if got := nicGbps("n2-standard-4", 4); got != 8 {
		t.Errorf("n2-standard-4 nic = %v want 8", got)
	}
	// unknown family uses the default 32 cap.
	if got := nicGbps("zz-standard-64", 64); got != 32 {
		t.Errorf("unknown family nic = %v want 32", got)
	}
	if got := nicGbps("n2-standard-4", 0); got != 0 {
		t.Errorf("zero vcpu nic = %v want 0", got)
	}
}

func TestDiskIOPSCeiling(t *testing.T) {
	// provisioned IOPS wins.
	if got := diskIOPSCeiling("pd-extreme", 500, 25000); got != 25000 {
		t.Errorf("provisioned = %v want 25000", got)
	}
	// pd-ssd scales at 30/GB.
	if got := diskIOPSCeiling("pd-ssd", 100, 0); got != 3000 {
		t.Errorf("pd-ssd 100GB = %v want 3000", got)
	}
	// pd-standard caps at 7500.
	if got := diskIOPSCeiling("pd-standard", 100000, 0); got != 7500 {
		t.Errorf("pd-standard cap = %v want 7500", got)
	}
	// unknown type -> 0 (unknown).
	if got := diskIOPSCeiling("local-ssd", 375, 0); got != 0 {
		t.Errorf("unknown type = %v want 0", got)
	}
}
