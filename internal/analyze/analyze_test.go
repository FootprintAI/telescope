package analyze

import (
	"testing"

	"github.com/footprintai/telescope/internal/model"
)

func inst(cpu, mem, netFrac float64, memPresent bool) model.Instance {
	in := model.Instance{ID: "x", VCPU: 4, MemGB: 16, NICGbps: 10}
	nicBps := in.NICGbps * 1e9 / 8.0
	in.Metrics = &model.Metrics{
		CPUUtil:        model.Stat{P95: cpu, Present: true},
		MemUtil:        model.Stat{P95: mem, Present: memPresent},
		NetBytesPerSec: model.Stat{P95: netFrac * nicBps, Present: true},
		DiskIOPS:       model.Stat{Present: false},
		Samples:        100,
	}
	return in
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		in   model.Instance
		want Bound
	}{
		{"cpu", inst(0.85, 0.20, 0.05, true), BoundCPU},
		{"mem", inst(0.20, 0.88, 0.05, true), BoundMemory},
		{"net", inst(0.20, 0.15, 0.90, true), BoundNetwork},
		{"idle", inst(0.05, 0.06, 0.02, true), BoundIdle},
		{"balanced", inst(0.55, 0.52, 0.30, true), BoundBalanced},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Instance(c.in, Defaults()).Bound
			if got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestInsufficientData(t *testing.T) {
	in := model.Instance{ID: "y", VCPU: 4, MemGB: 16}
	if got := Instance(in, Defaults()).Bound; got != BoundInsufficient {
		t.Fatalf("got %s, want %s", got, BoundInsufficient)
	}
}

func TestMissingMemoryNoted(t *testing.T) {
	r := Instance(inst(0.85, 0, 0.05, false), Defaults())
	if len(r.Notes) == 0 {
		t.Fatalf("expected a note about missing memory metric")
	}
	if r.Norm["mem"] != -1 {
		t.Fatalf("absent memory should normalize to -1, got %v", r.Norm["mem"])
	}
}
