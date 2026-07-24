package savings

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/footprintai/telescope/internal/analyze"
	"github.com/footprintai/telescope/internal/model"
	"github.com/footprintai/telescope/internal/pricing"
)

// fixedNow is a deterministic clock for always-on math.
var fixedNow = time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)

func inst(id string) model.Instance {
	return model.Instance{ID: id, Provider: "mock", VCPU: 4, MemGB: 16}
}

func priceOf(hourly float64) *pricing.PriceInfo {
	p := pricing.PriceInfo{
		HourlyUSD:  hourly,
		MonthlyUSD: hourly * pricing.HoursPerMonth,
		Currency:   "USD",
		Source:     "test",
	}
	return &p
}

// result builds an analyze.Result whose top normalized p95 is `top` on the CPU
// dimension. A negative `top` models insufficient-data (no present dimension).
func result(id string, top float64) analyze.Result {
	if top < 0 {
		return analyze.Result{InstanceID: id, Bound: analyze.BoundInsufficient, Norm: map[string]float64{"cpu": -1}}
	}
	b := analyze.BoundCPU
	if top < analyze.Defaults().IdleFloor {
		b = analyze.BoundIdle
	}
	return analyze.Result{InstanceID: id, Bound: b, Norm: map[string]float64{"cpu": top}}
}

// Acceptance criterion: the Score object carries all headline fields plus a
// basis sub-object, and Defaults() surface the documented thresholds in basis.
func TestComputeExposesBasisThresholds(t *testing.T) {
	insts := []model.Instance{inst("a")}
	results := []analyze.Result{{InstanceID: "a", Bound: analyze.BoundCPU}}
	prices := map[string]*pricing.PriceInfo{"a": priceOf(1.0)}

	s := Compute(insts, results, prices, fixedNow, Defaults())

	if s.Basis.UnderutilizedThreshold != 0.30 {
		t.Errorf("basis UnderutilizedThreshold = %v, want 0.30", s.Basis.UnderutilizedThreshold)
	}
	if s.Basis.AlwaysOnHours != 720 {
		t.Errorf("basis AlwaysOnHours = %v, want 720", s.Basis.AlwaysOnHours)
	}
	if s.Basis.RightsizingFraction != 0.5 {
		t.Errorf("basis RightsizingFraction = %v, want 0.5", s.Basis.RightsizingFraction)
	}
	if s.Basis.RecoverableFormula == "" {
		t.Error("basis RecoverableFormula must document the formula, got empty")
	}
	if s.Basis.InstanceCount != 1 {
		t.Errorf("basis InstanceCount = %d, want 1", s.Basis.InstanceCount)
	}
}

// Acceptance criterion: with pricing, the dollar fields are present (non-nil).
func TestComputeDollarFieldsPresentWithPricing(t *testing.T) {
	insts := []model.Instance{inst("a")}
	results := []analyze.Result{{InstanceID: "a", Bound: analyze.BoundCPU}}
	prices := map[string]*pricing.PriceInfo{"a": priceOf(2.0)}

	s := Compute(insts, results, prices, fixedNow, Defaults())

	if s.TotalMonthlyUSD == nil {
		t.Fatal("TotalMonthlyUSD must be non-nil when pricing is present")
	}
	if s.UnderutilizedSpendPct == nil {
		t.Error("UnderutilizedSpendPct must be non-nil when pricing is present")
	}
	if s.RecoverableMonthlyUSD == nil {
		t.Error("RecoverableMonthlyUSD must be non-nil when pricing is present")
	}
	if got, want := *s.TotalMonthlyUSD, 2.0*pricing.HoursPerMonth; got != want {
		t.Errorf("TotalMonthlyUSD = %v, want %v", got, want)
	}
}

// Acceptance criterion: dollar fields are omitted (nil), not zero-filled, when
// pricing is off; percentage fields still populate.
func TestComputeDollarFieldsOmittedWithoutPricing(t *testing.T) {
	insts := []model.Instance{inst("a")}
	results := []analyze.Result{{InstanceID: "a", Bound: analyze.BoundCPU}}

	s := Compute(insts, results, nil, fixedNow, Defaults())

	if s.TotalMonthlyUSD != nil {
		t.Errorf("TotalMonthlyUSD must be nil without pricing, got %v", *s.TotalMonthlyUSD)
	}
	if s.UnderutilizedSpendPct != nil {
		t.Errorf("UnderutilizedSpendPct must be nil without pricing, got %v", *s.UnderutilizedSpendPct)
	}
	if s.RecoverableMonthlyUSD != nil {
		t.Errorf("RecoverableMonthlyUSD must be nil without pricing, got %v", *s.RecoverableMonthlyUSD)
	}

	// Marshal and confirm the dollar keys are physically absent (omitempty),
	// while the count-weighted percentage keys are present.
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"total_monthly_usd", "underutilized_spend_pct", "recoverable_monthly_usd"} {
		if _, ok := m[k]; ok {
			t.Errorf("key %q must be omitted from JSON without pricing", k)
		}
	}
	for _, k := range []string{"underutilized_instance_pct", "always_on_instance_pct", "basis"} {
		if _, ok := m[k]; !ok {
			t.Errorf("key %q must be present in JSON even without pricing", k)
		}
	}
}

// Acceptance criterion (#6): underutilized_spend_pct is the spend-weighted share
// of instances whose top utilization is below the 30% threshold.
func TestUnderutilizedSpendPct(t *testing.T) {
	insts := []model.Instance{inst("under"), inst("busy")}
	results := []analyze.Result{result("under", 0.10), result("busy", 0.50)}
	// equal price → under-utilized instance is exactly half the spend.
	prices := map[string]*pricing.PriceInfo{"under": priceOf(1.0), "busy": priceOf(1.0)}

	s := Compute(insts, results, prices, fixedNow, Defaults())

	if s.UnderutilizedSpendPct == nil {
		t.Fatal("UnderutilizedSpendPct must be non-nil with pricing")
	}
	if got := *s.UnderutilizedSpendPct; got != 50 {
		t.Errorf("UnderutilizedSpendPct = %v, want 50", got)
	}
	if s.UnderutilizedInstancePct != 50 {
		t.Errorf("UnderutilizedInstancePct = %v, want 50", s.UnderutilizedInstancePct)
	}
	if s.Basis.UnderutilizedInstances != 1 {
		t.Errorf("basis UnderutilizedInstances = %d, want 1", s.Basis.UnderutilizedInstances)
	}
}

// Spend-weighting must differ from count-weighting when prices are unequal: a
// cheap idle box and an expensive busy box is low spend-pct but 50% instance-pct.
func TestUnderutilizedSpendWeightingDiffersFromCount(t *testing.T) {
	insts := []model.Instance{inst("cheap-idle"), inst("pricey-busy")}
	results := []analyze.Result{result("cheap-idle", 0.05), result("pricey-busy", 0.60)}
	prices := map[string]*pricing.PriceInfo{"cheap-idle": priceOf(1.0), "pricey-busy": priceOf(9.0)}

	s := Compute(insts, results, prices, fixedNow, Defaults())

	if got := *s.UnderutilizedSpendPct; got != 10 { // 1 / (1+9)
		t.Errorf("UnderutilizedSpendPct = %v, want 10", got)
	}
	if s.UnderutilizedInstancePct != 50 {
		t.Errorf("UnderutilizedInstancePct = %v, want 50", s.UnderutilizedInstancePct)
	}
}

// The 30% threshold is strict: an instance at exactly 0.30 is NOT under-utilized.
func TestUnderutilizedThresholdBoundary(t *testing.T) {
	insts := []model.Instance{inst("edge")}
	results := []analyze.Result{result("edge", 0.30)}
	s := Compute(insts, results, map[string]*pricing.PriceInfo{"edge": priceOf(1.0)}, fixedNow, Defaults())
	if s.UnderutilizedInstancePct != 0 {
		t.Errorf("top==threshold must not be under-utilized; got pct %v", s.UnderutilizedInstancePct)
	}
}

// Acceptance criterion (#6): insufficient-data instances are excluded from the
// util numerator/denominator and counted in basis.excluded_no_data.
func TestExcludedNoData(t *testing.T) {
	insts := []model.Instance{inst("under"), inst("nodata")}
	results := []analyze.Result{result("under", 0.10), result("nodata", -1)}
	prices := map[string]*pricing.PriceInfo{"under": priceOf(1.0), "nodata": priceOf(1.0)}

	s := Compute(insts, results, prices, fixedNow, Defaults())

	if s.Basis.ExcludedNoData != 1 {
		t.Errorf("basis ExcludedNoData = %d, want 1", s.Basis.ExcludedNoData)
	}
	// denominator is instances-with-data (1), so the one under-utilized box = 100%.
	if s.UnderutilizedInstancePct != 100 {
		t.Errorf("UnderutilizedInstancePct = %v, want 100 (nodata excluded from denominator)", s.UnderutilizedInstancePct)
	}
}

// Acceptance criterion (#7): always_on_instance_pct is the share of listed
// instances whose running-hours-since-creation reach the 720h threshold.
// Instances without a creation timestamp are counted in basis.always_on_unknown
// and never counted as always-on (conservative).
func TestAlwaysOnInstancePct(t *testing.T) {
	old := fixedNow.Add(-1000 * time.Hour)  // > 720h ago -> always-on
	fresh := fixedNow.Add(-100 * time.Hour) // < 720h ago -> not
	insts := []model.Instance{
		{ID: "old", CreatedAt: old},
		{ID: "fresh", CreatedAt: fresh},
		{ID: "unknown"}, // no CreatedAt
	}
	results := []analyze.Result{result("old", 0.5), result("fresh", 0.5), result("unknown", 0.5)}

	s := Compute(insts, results, nil, fixedNow, Defaults())

	// 1 of 3 listed instances is always-on.
	if got := s.AlwaysOnInstancePct; got != round2(100.0/3.0) {
		t.Errorf("AlwaysOnInstancePct = %v, want %v", got, round2(100.0/3.0))
	}
	if s.Basis.AlwaysOnUnknown != 1 {
		t.Errorf("basis AlwaysOnUnknown = %d, want 1", s.Basis.AlwaysOnUnknown)
	}
	if s.Basis.AlwaysOnMethod == "" {
		t.Error("basis AlwaysOnMethod must document the derivation + limitation")
	}
}

// Boundary: exactly 720h of running time counts as always-on (inclusive).
func TestAlwaysOnBoundary(t *testing.T) {
	insts := []model.Instance{{ID: "edge", CreatedAt: fixedNow.Add(-720 * time.Hour)}}
	results := []analyze.Result{result("edge", 0.5)}
	s := Compute(insts, results, nil, fixedNow, Defaults())
	if s.AlwaysOnInstancePct != 100 {
		t.Errorf("720h must count as always-on; got %v", s.AlwaysOnInstancePct)
	}
}

// Empty fleet must not panic or divide by zero.
func TestComputeEmptyFleet(t *testing.T) {
	s := Compute(nil, nil, nil, fixedNow, Defaults())
	if s.AlwaysOnInstancePct != 0 || s.UnderutilizedInstancePct != 0 {
		t.Errorf("empty fleet pcts must be 0, got always-on=%v under=%v",
			s.AlwaysOnInstancePct, s.UnderutilizedInstancePct)
	}
	if s.Basis.InstanceCount != 0 {
		t.Errorf("empty fleet InstanceCount = %d, want 0", s.Basis.InstanceCount)
	}
}
