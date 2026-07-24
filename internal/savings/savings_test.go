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
