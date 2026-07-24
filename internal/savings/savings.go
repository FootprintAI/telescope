// Package savings computes the Savings Score — a top-line waste summary attached
// to the report: total spend, under-utilized share, always-on share, and an
// estimated recoverable-dollars figure.
//
// Compute is pure (no I/O, injected clock) so the formula is unit-testable in
// isolation. This file (issue #5) establishes the schema, the pricing-presence
// gating, and the basis accounting; the field-level formulas are filled in by
// #6 (under-utilized), #7 (always-on), and #8 (recoverable).
package savings

import (
	"github.com/footprintai/telescope/internal/analyze"
	"github.com/footprintai/telescope/internal/model"
	"github.com/footprintai/telescope/internal/pricing"

	"time"
)

// recoverableFormula documents, in the report's basis, how RecoverableMonthlyUSD
// is derived so the number is auditable by whoever reads the report.
const recoverableFormula = "100% of monthly spend on idle instances + rightsizing_fraction × monthly spend on under-utilized (non-idle) instances; spot and unpriced instances contribute $0"

// Config tunes the score. Zero value is not valid; use Defaults().
type Config struct {
	// UnderutilizedThreshold: an instance whose top normalized p95 utilization
	// is below this is "under-utilized" (distinct from analyze's stricter idle
	// floor).
	UnderutilizedThreshold float64
	// AlwaysOnHours: an instance running longer than this is "always-on".
	AlwaysOnHours float64
	// RightsizingFraction: recoverable share of spend on under-utilized but
	// not idle instances.
	RightsizingFraction float64
}

// Defaults returns the documented starting thresholds.
func Defaults() Config {
	return Config{
		UnderutilizedThreshold: 0.30,
		AlwaysOnHours:          720,
		RightsizingFraction:    0.5,
	}
}

// Score is the Savings Score block embedded in the report. Dollar fields are
// pointers so they can be omitted (not zero-filled) when pricing was not
// collected; percentage fields are count-weighted and always present.
type Score struct {
	TotalMonthlyUSD          *float64 `json:"total_monthly_usd,omitempty"`
	UnderutilizedSpendPct    *float64 `json:"underutilized_spend_pct,omitempty"`
	UnderutilizedInstancePct float64  `json:"underutilized_instance_pct"`
	AlwaysOnInstancePct      float64  `json:"always_on_instance_pct"`
	RecoverableMonthlyUSD    *float64 `json:"recoverable_monthly_usd,omitempty"`
	Basis                    Basis    `json:"basis"`
}

// Basis records the thresholds and counts behind the score so every number is
// auditable from the report alone.
type Basis struct {
	UnderutilizedThreshold float64 `json:"underutilized_utilization_threshold"`
	IdleFloor              float64 `json:"idle_floor"`
	AlwaysOnHours          float64 `json:"always_on_hours"`
	RightsizingFraction    float64 `json:"rightsizing_fraction"`
	RecoverableFormula     string  `json:"recoverable_formula"`
	InstanceCount          int     `json:"instance_count"`
	PricedInstances        int     `json:"priced_instances"`
	IdleInstances          int     `json:"idle_instances"`
	UnderutilizedInstances int     `json:"underutilized_instances"`
	ExcludedNoData         int     `json:"excluded_no_data"`
	AlwaysOnUnknown        int     `json:"always_on_unknown"`
}

// Compute assembles the Savings Score. prices may be nil (pricing disabled), in
// which case the dollar-denominated fields are left nil. now is injected for
// deterministic always-on math.
func Compute(insts []model.Instance, results []analyze.Result, prices map[string]*pricing.PriceInfo, now time.Time, cfg Config) Score {
	byID := make(map[string]analyze.Result, len(results))
	for _, r := range results {
		byID[r.InstanceID] = r
	}

	basis := Basis{
		UnderutilizedThreshold: cfg.UnderutilizedThreshold,
		IdleFloor:              analyze.Defaults().IdleFloor,
		AlwaysOnHours:          cfg.AlwaysOnHours,
		RightsizingFraction:    cfg.RightsizingFraction,
		RecoverableFormula:     recoverableFormula,
		InstanceCount:          len(insts),
	}

	var totalMonthly, underutilizedMonthly float64
	var withData, underutilizedCount int
	for _, in := range insts {
		var monthly float64
		var priced bool
		if prices != nil {
			if pi := prices[in.ID]; pi != nil {
				priced = true
				monthly = pi.MonthlyUSD
				basis.PricedInstances++
				totalMonthly += monthly
			}
		}

		// Instances with no present utilization dimension (insufficient-data) are
		// excluded from the util numerator and denominator, but a priced one still
		// counts toward total spend — we just don't claim it as waste.
		r, hasResult := byID[in.ID]
		top, hasData := topNorm(r)
		if !hasResult || !hasData {
			basis.ExcludedNoData++
			continue
		}
		withData++
		if r.Bound == analyze.BoundIdle {
			basis.IdleInstances++
		}
		if top < cfg.UnderutilizedThreshold {
			underutilizedCount++
			if priced {
				underutilizedMonthly += monthly
			}
		}
	}
	basis.UnderutilizedInstances = underutilizedCount

	s := Score{Basis: basis}
	s.UnderutilizedInstancePct = pct(underutilizedCount, withData)

	// TODO(#7): compute AlwaysOnInstancePct from model.CreatedAt (+ sample-density fallback).
	// TODO(#8): compute RecoverableMonthlyUSD from idle + rightsized under-utilized spend.

	if prices != nil {
		total := round2(totalMonthly)
		s.TotalMonthlyUSD = &total
		var underPct float64
		if totalMonthly > 0 {
			underPct = round2(underutilizedMonthly / totalMonthly * 100)
		}
		s.UnderutilizedSpendPct = &underPct
		var recoverable float64 // TODO(#8)
		s.RecoverableMonthlyUSD = &recoverable
	}

	return s
}

// topNorm returns the highest present normalized-p95 utilization across an
// instance's dimensions; ok is false when no dimension was present (the source
// metrics were unavailable → insufficient-data).
func topNorm(r analyze.Result) (top float64, ok bool) {
	top = -1
	for _, v := range r.Norm {
		if v < 0 {
			continue
		}
		if v > top {
			top = v
		}
		ok = true
	}
	return top, ok
}

// pct returns 100*n/d rounded, or 0 when d is 0.
func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return round2(float64(n) / float64(d) * 100)
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
