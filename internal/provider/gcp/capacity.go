package gcp

import "strings"

// This file turns GCE machine/disk shapes into capacity ceilings used to
// normalize the network and disk dimensions during analysis. The numbers are
// GCP's published defaults (standard networking tier, baseline read IOPS) and
// are intentionally conservative; refine per exact SKU as needed.

// nicPerVCPUGbps is GCE's default per-vCPU egress allotment.
const nicPerVCPUGbps = 2.0

// nicFamilyCapGbps is the default max egress bandwidth per family (standard
// tier, i.e. not Tier_1). Unknown families fall back to defaultNICCapGbps.
var nicFamilyCapGbps = map[string]float64{
	"e2":  16,
	"n1":  32,
	"n2":  32,
	"n2d": 32,
	"t2d": 32,
	"t2a": 32,
	"c2":  32,
	"c2d": 32,
	"c3":  100,
	"c3d": 100,
	"c4":  100,
	"n4":  50,
	"m1":  32,
	"m2":  32,
	"m3":  32,
	"a2":  100,
	"a3":  200,
	"g2":  100,
}

const defaultNICCapGbps = 32

// familyOf extracts the family prefix of a machine type, e.g.
// "n2d-highmem-8" -> "n2d", "e2-custom-4-8192" -> "e2".
func familyOf(machineType string) string {
	if i := strings.Index(machineType, "-"); i >= 0 {
		return strings.ToLower(machineType[:i])
	}
	return strings.ToLower(machineType)
}

// nicGbps returns the default egress ceiling for a machine type:
// min(vCPU * 2, family cap).
func nicGbps(machineType string, vCPU float64) float64 {
	if vCPU <= 0 {
		return 0
	}
	cap := defaultNICCapGbps
	if c, ok := nicFamilyCapGbps[familyOf(machineType)]; ok {
		cap = int(c)
	}
	g := vCPU * nicPerVCPUGbps
	if g > float64(cap) {
		g = float64(cap)
	}
	return g
}

// diskIOPSPerGB / diskIOPSMax are GCP baseline READ IOPS scaling per PD type.
// (Per-VM ceilings also apply and are not modeled here.)
var diskIOPSPerGB = map[string]float64{
	"pd-standard": 0.75,
	"pd-balanced": 6,
	"pd-ssd":      30,
}
var diskIOPSMax = map[string]float64{
	"pd-standard": 7500,
	"pd-balanced": 80000,
	"pd-ssd":      100000,
}

// diskIOPSCeiling returns an IOPS ceiling for one disk. Provisioned-IOPS disks
// (pd-extreme, hyperdisk-*) report the value directly; size-scaled types are
// derived from size. Unknown/unscaled types return 0 (treated as "unknown", so
// the disk dimension is skipped rather than guessed).
func diskIOPSCeiling(diskType string, sizeGB, provisionedIOPS float64) float64 {
	if provisionedIOPS > 0 {
		return provisionedIOPS
	}
	t := strings.ToLower(diskType)
	perGB, ok := diskIOPSPerGB[t]
	if !ok {
		return 0
	}
	v := sizeGB * perGB
	if m, ok := diskIOPSMax[t]; ok && v > m {
		v = m
	}
	return v
}
