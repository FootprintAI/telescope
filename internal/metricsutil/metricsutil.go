// Package metricsutil holds provider-agnostic helpers for turning raw metric
// samples into model.Stat summaries (p50/p95/max), shared by all providers.
package metricsutil

import (
	"sort"

	"github.com/footprintai/telescope/internal/model"
)

// Summarize turns a slice of samples into a model.Stat. An empty slice yields
// Present=false.
func Summarize(vals []float64) model.Stat {
	if len(vals) == 0 {
		return model.Stat{Present: false}
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	return model.Stat{
		P50:     Percentile(s, 0.50),
		P95:     Percentile(s, 0.95),
		Max:     s[len(s)-1],
		Present: true,
	}
}

// Percentile returns the p-quantile (0..1) of an already-sorted slice using
// nearest-rank.
func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	rank := int(p*float64(len(sorted)-1) + 0.5)
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

// SumByKey flattens a key->value map into a slice of its values.
func SumByKey(m map[int64]float64) []float64 {
	if len(m) == 0 {
		return nil
	}
	out := make([]float64, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
