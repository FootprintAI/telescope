package pricing

import (
	"context"

	"github.com/footprintai/telescope/internal/model"
)

// Static is an embedded price table used for the mock provider and as a
// last-resort fallback. Region is ignored.
type Static struct {
	m map[string]float64
}

// NewStatic returns a Static pricer with approximate us-central1 / us-east-1
// on-demand list prices.
func NewStatic() *Static {
	return &Static{m: map[string]float64{
		// GCP
		"e2-standard-2": 0.067, "e2-standard-4": 0.134, "e2-standard-8": 0.268,
		"e2-standard-16": 0.536, "e2-standard-32": 1.072,
		"n2-standard-4": 0.194, "n2-standard-8": 0.388, "n2-standard-16": 0.777,
		"n2-standard-32": 1.554, "n2-highmem-8": 0.524, "n2-highmem-16": 1.048,
		"n2-highcpu-16": 0.573, "n2-highcpu-32": 1.146,
		// AWS
		"m5.large": 0.096, "m5.xlarge": 0.192, "m5.2xlarge": 0.384,
		"m5.4xlarge": 0.768, "r5.2xlarge": 0.504, "c5.4xlarge": 0.680,
	}}
}

// Price implements Pricer.
func (s *Static) Price(_ context.Context, in model.Instance) (PriceInfo, bool) {
	if p, ok := s.m[in.MachineType]; ok {
		return mk(p, "static"), true
	}
	return PriceInfo{}, false
}
