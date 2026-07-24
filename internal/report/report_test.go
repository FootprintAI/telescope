package report

import (
	"testing"
	"time"

	"github.com/footprintai/telescope/internal/analyze"
	"github.com/footprintai/telescope/internal/model"
	"github.com/footprintai/telescope/internal/pricing"
)

// SchemaVersion must be bumped to "2" for the additive savings_score field.
func TestSchemaVersionBumped(t *testing.T) {
	if SchemaVersion != "2" {
		t.Errorf("SchemaVersion = %q, want %q", SchemaVersion, "2")
	}
}

func buildFixture(prices map[string]*pricing.PriceInfo) Report {
	insts := []model.Instance{{ID: "a", Provider: "mock", VCPU: 4, MemGB: 16}}
	results := []analyze.Result{{InstanceID: "a", Bound: analyze.BoundCPU}}
	w := model.Window{Lookback: 14 * 24 * time.Hour, End: time.Now().UTC()}
	return Build("mock", w, insts, nil, results, prices)
}

// Build must attach a SavingsScore whether or not pricing was collected.
func TestBuildAttachesSavingsScore(t *testing.T) {
	rep := buildFixture(nil)
	if rep.SavingsScore == nil {
		t.Fatal("SavingsScore must be attached even without pricing")
	}
	if rep.SavingsScore.TotalMonthlyUSD != nil {
		t.Error("TotalMonthlyUSD must be nil when pricing is off")
	}

	priced := map[string]*pricing.PriceInfo{
		"a": {HourlyUSD: 1.0, MonthlyUSD: 1.0 * pricing.HoursPerMonth, Currency: "USD", Source: "test"},
	}
	rep2 := buildFixture(priced)
	if rep2.SavingsScore == nil || rep2.SavingsScore.TotalMonthlyUSD == nil {
		t.Fatal("TotalMonthlyUSD must be non-nil when pricing is present")
	}
	if rep2.Schema != "2" {
		t.Errorf("report Schema = %q, want %q", rep2.Schema, "2")
	}
}
