// Package mock is a deterministic Provider with synthetic fixtures. It lets the
// full pipeline run (and be tested) without any cloud credentials.
package mock

import (
	"context"
	"math"

	"github.com/footprintai/telescope/internal/model"
)

// Provider implements provider.Provider with fixed data.
type Provider struct{}

// New returns a mock provider.
func New() *Provider { return &Provider{} }

// Name implements provider.Provider.
func (p *Provider) Name() string { return "mock" }

func disk(name string, gb, iops float64, t string) model.Disk {
	return model.Disk{Name: name, SizeGB: gb, IOPSCap: iops, Type: t}
}

// ListInstances returns a representative mix of workload shapes.
func (p *Provider) ListInstances(ctx context.Context) ([]model.Instance, error) {
	base := func(id, name, mt string, vcpu, mem, nic float64) model.Instance {
		return model.Instance{
			ID: id, Name: name, Provider: "mock", Project: "demo",
			Region: "us-central1", Zone: "us-central1-a",
			MachineType: mt, VCPU: vcpu, MemGB: mem, NICGbps: nic,
			Disks:  []model.Disk{disk(name+"-boot", 100, 3000, "pd-ssd")},
			Labels: map[string]string{"env": "prod"},
		}
	}
	return []model.Instance{
		base("i-001", "web-1", "e2-standard-4", 4, 16, 10),   // cpu-bound, low mem
		base("i-002", "web-2", "e2-standard-4", 4, 16, 10),   // idle
		base("i-003", "cache-1", "n2-highmem-8", 8, 64, 16),  // memory-bound
		base("i-004", "api-1", "e2-standard-8", 8, 32, 16),   // balanced
		base("i-005", "edge-1", "n2-standard-4", 4, 16, 32),  // network-bound
		base("i-006", "batch-1", "e2-standard-16", 16, 64, 32), // idle/over-provisioned
	}, nil
}

// ListNodePools returns one demo nodepool.
func (p *Provider) ListNodePools(ctx context.Context) ([]model.NodePool, error) {
	return []model.NodePool{
		{Cluster: "demo-gke", Name: "default-pool", Provider: "mock",
			Region: "us-central1", MachineType: "e2-standard-4", VCPU: 4, MemGB: 16, NodeCount: 3},
	}, nil
}

// FetchMetrics attaches deterministic synthetic utilization to each instance.
func (p *Provider) FetchMetrics(ctx context.Context, insts []model.Instance, w model.Window) ([]model.Instance, error) {
	type profile struct {
		cpu, mem, netFrac, diskFrac float64
		memPresent                  bool
	}
	// cpu p95, mem p95, net fraction of NIC, disk fraction of IOPS cap
	profiles := map[string]profile{
		"i-001": {0.82, 0.20, 0.05, 0.10, true},  // cpu-bound
		"i-002": {0.06, 0.08, 0.02, 0.05, true},  // idle
		"i-003": {0.25, 0.88, 0.10, 0.30, true},  // memory-bound
		"i-004": {0.55, 0.52, 0.30, 0.35, true},  // balanced
		"i-005": {0.30, 0.25, 0.85, 0.15, true},  // network-bound
		"i-006": {0.05, 0.10, 0.03, 0.04, false}, // idle, mem metric missing
	}
	for idx := range insts {
		in := &insts[idx]
		pr, ok := profiles[in.ID]
		if !ok {
			pr = profile{0.4, 0.4, 0.2, 0.2, true}
		}
		nicBps := in.NICGbps * 1e9 / 8.0
		in.Metrics = &model.Metrics{
			CPUUtil:        stat(pr.cpu, true),
			MemUtil:        stat(pr.mem, pr.memPresent),
			NetBytesPerSec: stat(pr.netFrac*nicBps, true),
			DiskIOPS:       stat(pr.diskFrac*in.DiskIOPSCap(), true),
			Samples:        int(w.Lookback.Hours() * 12), // ~5-min cadence
		}
	}
	return insts, nil
}

// stat builds a Stat with p50 slightly under p95 and max slightly above.
func stat(p95 float64, present bool) model.Stat {
	if !present {
		return model.Stat{Present: false}
	}
	return model.Stat{
		P50:     math.Max(0, p95*0.7),
		P95:     p95,
		Max:     math.Min(p95*1.15, p95+0.05),
		Present: true,
	}
}
