package pricing

import (
	"context"
	"testing"

	billingpb "cloud.google.com/go/billing/apiv1/billingpb"
	"google.golang.org/genproto/googleapis/type/money"

	"github.com/footprintai/telescope/internal/model"
)

func sku(desc, usageType string, price float64, regions ...string) *billingpb.Sku {
	units := int64(price)
	nanos := int32((price - float64(units)) * 1e9)
	return &billingpb.Sku{
		Description: desc,
		Category: &billingpb.Category{
			ResourceFamily: "Compute",
			UsageType:      usageType,
		},
		ServiceRegions: regions,
		PricingInfo: []*billingpb.PricingInfo{{
			PricingExpression: &billingpb.PricingExpression{
				TieredRates: []*billingpb.PricingExpression_TierRate{{
					UnitPrice: &money.Money{CurrencyCode: "USD", Units: units, Nanos: nanos},
				}},
			},
		}},
	}
}

func newTestPricer() *gcpPricer {
	p := &gcpPricer{
		core:     map[string]float64{},
		ram:      map[string]float64{},
		spotCore: map[string]float64{},
		spotRam:  map[string]float64{},
	}
	p.once.Do(func() {}) // build already "done"; tests populate the maps directly
	return p
}

func TestIndexSkuSeparatesSpotFromOnDemand(t *testing.T) {
	p := newTestPricer()
	for _, s := range []*billingpb.Sku{
		sku("C3D Instance Core running in Taiwan", "OnDemand", 0.034231, "asia-east1"),
		sku("C3D Instance Ram running in Taiwan", "OnDemand", 0.004584, "asia-east1"),
		sku("Spot Preemptible C3D Instance Core running in Taiwan", "Preemptible", 0.01242, "asia-east1"),
		sku("Spot Preemptible C3D Instance Ram running in Taiwan", "Preemptible", 0.001663, "asia-east1"),
		sku("Commitment v1: C3D Instance Core running in Taiwan", "Commit1Yr", 0.02, "asia-east1"),
	} {
		p.indexSku(s)
	}
	key := "asia-east1/c3d"
	if _, ok := p.core[key]; !ok {
		t.Errorf("on-demand core SKU not indexed under %q", key)
	}
	if _, ok := p.ram[key]; !ok {
		t.Errorf("on-demand ram SKU not indexed under %q", key)
	}
	if _, ok := p.spotCore[key]; !ok {
		t.Errorf("spot core SKU not indexed under %q", key)
	}
	if _, ok := p.spotRam[key]; !ok {
		t.Errorf("spot ram SKU not indexed under %q", key)
	}
}

func TestPricePicksRatesByProvisioningModel(t *testing.T) {
	p := newTestPricer()
	key := "us-west1/c3d"
	p.core[key], p.ram[key] = 0.029563, 0.003959
	p.spotCore[key], p.spotRam[key] = 0.00659, 0.000882

	in := model.Instance{
		Region: "us-west1", MachineType: "c3d-highmem-8",
		VCPU: 8, MemGB: 64,
	}

	in.ProvisioningModel = model.ProvisioningOnDemand
	od, ok := p.Price(context.Background(), in)
	if !ok {
		t.Fatal("on-demand instance not priced")
	}
	if od.Source != "gcp-billing-catalog" {
		t.Errorf("on-demand source = %q", od.Source)
	}

	in.ProvisioningModel = model.ProvisioningSpot
	spot, ok := p.Price(context.Background(), in)
	if !ok {
		t.Fatal("spot instance not priced")
	}
	if spot.Source != "gcp-billing-catalog-spot" {
		t.Errorf("spot source = %q", spot.Source)
	}
	if spot.HourlyUSD >= od.HourlyUSD {
		t.Errorf("spot price %v not below on-demand %v", spot.HourlyUSD, od.HourlyUSD)
	}
}

func TestBilledVCPUSharedCoreFraction(t *testing.T) {
	cases := []struct {
		machineType string
		logicalVCPU float64
		want        float64
	}{
		{"e2-micro", 2, 0.25},
		{"e2-small", 2, 0.5},
		{"e2-medium", 2, 1.0},
		{"E2-MICRO", 2, 0.25}, // case-insensitive
		{"e2-standard-4", 4, 4},
		{"c3d-highmem-8", 8, 8},
	}
	for _, tc := range cases {
		if got := billedVCPU(tc.machineType, tc.logicalVCPU); got != tc.want {
			t.Errorf("billedVCPU(%q, %v) = %v, want %v", tc.machineType, tc.logicalVCPU, got, tc.want)
		}
	}
}

func TestPriceUsesBilledVCPUForSharedCore(t *testing.T) {
	p := newTestPricer()
	key := "us-west1/e2"
	p.core[key], p.ram[key] = 0.02181159, 0.00292353

	micro := model.Instance{
		Region: "us-west1", MachineType: "e2-micro",
		ProvisioningModel: model.ProvisioningOnDemand,
		VCPU:              2, MemGB: 1,
	}
	got, ok := p.Price(context.Background(), micro)
	if !ok {
		t.Fatal("e2-micro not priced")
	}
	want := 0.25*0.02181159 + 1*0.00292353 // billed at 0.25 vCPU, not the logical 2
	if diff := got.HourlyUSD - round4(want); diff > 1e-6 || diff < -1e-6 {
		t.Errorf("e2-micro hourly = %v, want %v", got.HourlyUSD, round4(want))
	}

	standard := model.Instance{
		Region: "us-west1", MachineType: "e2-standard-4",
		ProvisioningModel: model.ProvisioningOnDemand,
		VCPU:              4, MemGB: 16,
	}
	got, ok = p.Price(context.Background(), standard)
	if !ok {
		t.Fatal("e2-standard-4 not priced")
	}
	want = 4*0.02181159 + 16*0.00292353 // unaffected: billed at the full logical count
	if diff := got.HourlyUSD - round4(want); diff > 1e-6 || diff < -1e-6 {
		t.Errorf("e2-standard-4 hourly = %v, want %v", got.HourlyUSD, round4(want))
	}
}

func TestPriceSpotWithoutSpotSkuIsUnpriced(t *testing.T) {
	p := newTestPricer()
	key := "us-west1/c3d"
	p.core[key], p.ram[key] = 0.029563, 0.003959 // on-demand only

	in := model.Instance{
		Region: "us-west1", MachineType: "c3d-highmem-8",
		VCPU: 8, MemGB: 64,
		ProvisioningModel: model.ProvisioningSpot,
	}
	if _, ok := p.Price(context.Background(), in); ok {
		t.Fatal("spot instance was priced from on-demand rates; want unpriced")
	}
}
