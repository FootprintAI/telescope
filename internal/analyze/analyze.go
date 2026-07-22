// Package analyze classifies each instance's workload character (cpu-, memory-,
// network-, or disk-bound, or idle/over-provisioned) from its metrics.
//
// This is Stage A (client-facing): it depends only on model data and contains
// no Containarium/pricing logic.
package analyze

import "github.com/footprintai/telescope/internal/model"

// Bound is the dominant resource pressure on a workload.
type Bound string

const (
	BoundCPU          Bound = "cpu-bound"
	BoundMemory       Bound = "memory-bound"
	BoundNetwork      Bound = "network-bound"
	BoundDisk         Bound = "disk-bound"
	BoundIdle         Bound = "idle"     // all dimensions under the idle floor
	BoundBalanced     Bound = "balanced" // no single dimension dominates
	BoundInsufficient Bound = "insufficient-data"
)

// Thresholds tune the classifier. Zero value -> Defaults().
type Thresholds struct {
	IdleFloor float64 // below this normalized p95 on every dim => idle
	Dominance float64 // top dim must exceed 2nd by this margin to be "bound"
}

// Defaults returns sensible thresholds.
func Defaults() Thresholds {
	return Thresholds{IdleFloor: 0.15, Dominance: 0.15}
}

// Result is the per-instance analysis outcome (part of the shareable report).
type Result struct {
	InstanceID string             `json:"instance_id"`
	Bound      Bound              `json:"bound"`
	Norm       map[string]float64 `json:"normalized_p95"` // dim -> 0..1 (or -1 if absent)
	Notes      []string           `json:"notes,omitempty"`
}

// netCeilingBytesPerSec converts a NIC Gbps rating to bytes/sec.
func netCeilingBytesPerSec(gbps float64) float64 {
	if gbps <= 0 {
		return 0
	}
	return gbps * 1e9 / 8.0
}

// Instance classifies a single instance.
func Instance(inst model.Instance, t Thresholds) Result {
	if t.IdleFloor == 0 && t.Dominance == 0 {
		t = Defaults()
	}
	res := Result{InstanceID: inst.ID, Norm: map[string]float64{}}

	if inst.Metrics == nil || inst.Metrics.Samples == 0 {
		res.Bound = BoundInsufficient
		res.Notes = append(res.Notes, "no metrics available for window")
		return res
	}
	m := inst.Metrics

	// Normalize each dimension to [0,1] against its capacity. -1 => absent.
	norm := func(s model.Stat, cap float64) float64 {
		if !s.Present || cap <= 0 {
			return -1
		}
		v := s.P95 / cap
		if v < 0 {
			v = 0
		}
		return v
	}

	res.Norm["cpu"] = normFrac(m.CPUUtil) // util is already a fraction
	res.Norm["mem"] = normFrac(m.MemUtil)
	res.Norm["net"] = norm(m.NetBytesPerSec, netCeilingBytesPerSec(inst.NICGbps))
	res.Norm["disk"] = norm(m.DiskIOPS, inst.DiskIOPSCap())

	if !m.MemUtil.Present {
		res.Notes = append(res.Notes, "memory metric missing (monitoring agent not installed?)")
	}

	// Find the top present dimension and the runner-up.
	top, topVal := "", -1.0
	secondVal := -1.0
	anyPresent := false
	for dim, v := range res.Norm {
		if v < 0 {
			continue
		}
		anyPresent = true
		if v > topVal {
			secondVal = topVal
			top, topVal = dim, v
		} else if v > secondVal {
			secondVal = v
		}
	}

	if !anyPresent {
		res.Bound = BoundInsufficient
		return res
	}
	if topVal < t.IdleFloor {
		res.Bound = BoundIdle
		return res
	}
	if secondVal >= 0 && (topVal-secondVal) < t.Dominance {
		res.Bound = BoundBalanced
		return res
	}
	switch top {
	case "cpu":
		res.Bound = BoundCPU
	case "mem":
		res.Bound = BoundMemory
	case "net":
		res.Bound = BoundNetwork
	case "disk":
		res.Bound = BoundDisk
	}
	return res
}

// normFrac clamps an already-fractional utilization stat.
func normFrac(s model.Stat) float64 {
	if !s.Present {
		return -1
	}
	v := s.P95
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return v
}

// All classifies every instance.
func All(insts []model.Instance, t Thresholds) []Result {
	out := make([]Result, 0, len(insts))
	for _, in := range insts {
		out = append(out, Instance(in, t))
	}
	return out
}
