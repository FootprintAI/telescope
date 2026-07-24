// Package report defines the shareable Stage-A artifact (report.json) that the
// customer hands over, plus renderers (table / json / markdown).
package report

import (
	"time"

	"github.com/footprintai/telescope/internal/analyze"
	"github.com/footprintai/telescope/internal/model"
	"github.com/footprintai/telescope/internal/pricing"
	"github.com/footprintai/telescope/internal/savings"
	"github.com/footprintai/telescope/internal/version"
)

// SchemaVersion is bumped on changes to the report contract. v2 adds the
// additive savings_score block.
const SchemaVersion = "2"

// Report is the full Stage-A artifact.
type Report struct {
	Schema       string           `json:"schema"`
	Tool         string           `json:"tool"`
	GeneratedAt  time.Time        `json:"generated_at"`
	Provider     string           `json:"provider"`
	Window       WindowInfo       `json:"window"`
	Instances    []InstanceReport `json:"instances"`
	NodePools    []model.NodePool `json:"node_pools"`
	Summary      Summary          `json:"summary"`
	Cost         *CostSummary     `json:"cost,omitempty"`
	SavingsScore *savings.Score   `json:"savings_score,omitempty"`
}

// WindowInfo records the lookback used for metrics.
type WindowInfo struct {
	LookbackHours float64   `json:"lookback_hours"`
	End           time.Time `json:"end"`
}

// InstanceReport is one instance plus its analysis (and price, if collected).
type InstanceReport struct {
	Instance model.Instance     `json:"instance"`
	Analysis analyze.Result     `json:"analysis"`
	Pricing  *pricing.PriceInfo `json:"pricing,omitempty"`
}

// Summary is a quick roll-up for humans.
type Summary struct {
	InstanceCount int            `json:"instance_count"`
	NodePoolCount int            `json:"node_pool_count"`
	BoundCounts   map[string]int `json:"bound_counts"`
	TotalVCPU     float64        `json:"total_vcpu"`
	TotalMemGB    float64        `json:"total_mem_gb"`
}

// CostSummary aggregates on-demand list cost across priced instances.
type CostSummary struct {
	Currency         string  `json:"currency"`
	TotalHourlyUSD   float64 `json:"total_hourly_usd"`
	TotalMonthlyUSD  float64 `json:"total_monthly_usd"`
	PricedInstances  int     `json:"priced_instances"`
	UnpricedInstance int     `json:"unpriced_instances"`
}

// Build assembles a Report from inventory + analysis. When prices is non-nil
// (keyed by instance ID), pricing metadata and a cost summary are attached.
func Build(provider string, w model.Window, insts []model.Instance, pools []model.NodePool, results []analyze.Result, prices map[string]*pricing.PriceInfo) Report {
	byID := make(map[string]analyze.Result, len(results))
	for _, r := range results {
		byID[r.InstanceID] = r
	}
	rep := Report{
		Schema:      SchemaVersion,
		Tool:        version.V,
		GeneratedAt: time.Now().UTC(),
		Provider:    provider,
		Window:      WindowInfo{LookbackHours: w.Lookback.Hours(), End: w.End},
		NodePools:   pools,
		Summary:     Summary{BoundCounts: map[string]int{}},
	}
	var cost *CostSummary
	if prices != nil {
		cost = &CostSummary{Currency: "USD"}
	}
	for _, in := range insts {
		ir := InstanceReport{Instance: in, Analysis: byID[in.ID]}
		if prices != nil {
			if pi := prices[in.ID]; pi != nil {
				ir.Pricing = pi
				cost.TotalHourlyUSD += pi.HourlyUSD
				cost.PricedInstances++
			} else {
				cost.UnpricedInstance++
			}
		}
		rep.Instances = append(rep.Instances, ir)
		rep.Summary.BoundCounts[string(ir.Analysis.Bound)]++
		rep.Summary.TotalVCPU += in.VCPU
		rep.Summary.TotalMemGB += in.MemGB
	}
	rep.Summary.InstanceCount = len(insts)
	rep.Summary.NodePoolCount = len(pools)
	if cost != nil {
		cost.TotalHourlyUSD = round2(cost.TotalHourlyUSD)
		cost.TotalMonthlyUSD = round2(cost.TotalHourlyUSD * pricing.HoursPerMonth)
		rep.Cost = cost
	}
	score := savings.Compute(insts, results, prices, time.Now().UTC(), savings.Defaults())
	rep.SavingsScore = &score
	return rep
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
