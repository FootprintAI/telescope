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
