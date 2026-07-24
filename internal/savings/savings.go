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

// alwaysOnMethod documents, in the report's basis, how always-on is derived and
// its limitation, so the number is auditable.
const alwaysOnMethod = "running hours since creation ≥ always_on_hours; instances without a creation timestamp are counted in always_on_unknown and never counted as always-on (sample-density fallback not yet implemented)"

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
	AlwaysOnMethod         string  `json:"always_on_method"`
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
		AlwaysOnMethod:         alwaysOnMethod,
		InstanceCount:          len(insts),
	}

	var totalMonthly, underutilizedMonthly float64
	var idleMonthly, rightsizableMonthly float64
	var withData, underutilizedCount, alwaysOnCount int
	for _, in := range insts {
		// Always-on is over ALL listed instances (not just those with metrics):
		// an unknown-creation instance is counted in AlwaysOnUnknown, never as
		// always-on.
		switch {
		case in.CreatedAt.IsZero():
			basis.AlwaysOnUnknown++
		case in.RunningHours(now) >= cfg.AlwaysOnHours:
			alwaysOnCount++
		}

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
		idle := r.Bound == analyze.BoundIdle
		under := top < cfg.UnderutilizedThreshold
		if idle {
			basis.IdleInstances++
		}
		if under {
			underutilizedCount++
			if priced {
				underutilizedMonthly += monthly
			}
		}
		// Recoverable spend, excluding spot (already cheap/ephemeral) and
		// unpriced instances: idle boxes are fully reclaimable, under-utilized
		// (non-idle) boxes are rightsizable at RightsizingFraction.
		if priced && in.ProvisioningModel != model.ProvisioningSpot {
			switch {
			case idle:
				idleMonthly += monthly
			case under:
				rightsizableMonthly += monthly
			}
		}
	}
	basis.UnderutilizedInstances = underutilizedCount

	s := Score{Basis: basis}
	s.UnderutilizedInstancePct = pct(underutilizedCount, withData)
	s.AlwaysOnInstancePct = pct(alwaysOnCount, len(insts))

	if prices != nil {
		total := round2(totalMonthly)
		s.TotalMonthlyUSD = &total
		var underPct float64
		if totalMonthly > 0 {
			underPct = round2(underutilizedMonthly / totalMonthly * 100)
		}
		s.UnderutilizedSpendPct = &underPct

		recoverable := round2(idleMonthly + cfg.RightsizingFraction*rightsizableMonthly)
		if recoverable > total { // invariant guard; should not trigger
			recoverable = total
		}
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
