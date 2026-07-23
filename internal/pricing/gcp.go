package pricing

import (
	"context"
	"strings"
	"sync"

	billing "cloud.google.com/go/billing/apiv1"
	billingpb "cloud.google.com/go/billing/apiv1/billingpb"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/footprintai/telescope/internal/model"
)

// computeServiceName is the Cloud Billing Catalog service id for Compute Engine.
const computeServiceName = "services/6F81-5844-456A"

// Cloud Billing Catalog usage types (Sku.Category.UsageType). Spot capacity is
// still cataloged under the legacy "Preemptible" usage type.
const (
	usageOnDemand    = "OnDemand"
	usagePreemptible = "Preemptible"
)

// PriceInfo.Source values emitted by this pricer.
const (
	sourceGCPCatalog     = "gcp-billing-catalog"
	sourceGCPCatalogSpot = "gcp-billing-catalog-spot"
)

// gcpPricer resolves predefined-machine-type prices from the Cloud Billing
// Catalog by decomposing them into per-vCPU ("Core") and per-GB ("Ram") SKUs.
type gcpPricer struct {
	cli  *billing.CloudCatalogClient
	once sync.Once
	err  error
	// keyed by "region/family" -> USD per vCPU-hour / per GB-hour
	core map[string]float64
	ram  map[string]float64
	// same keys, Spot (usage type "Preemptible") rates
	spotCore map[string]float64
	spotRam  map[string]float64
}

// familyDisplay maps a machine-type family prefix to its catalog display token.
// Order matters: longer tokens first so "c2d" wins over "c2".
var familyDisplay = []struct{ key, token string }{
	{"n2d", "N2D"}, {"c2d", "C2D"}, {"c3d", "C3D"},
	{"e2", "E2"}, {"n2", "N2"}, {"n1", "N1"}, {"n4", "N4"},
	{"c2", "C2"}, {"c3", "C3"}, {"c4", "C4"}, {"t2d", "T2D"}, {"t2a", "T2A"},
	{"m1", "M1"}, {"m2", "M2"}, {"m3", "M3"},
	{"a2", "A2"}, {"a3", "A3"}, {"g2", "G2"},
}

var priceExcludeTokens = []string{"Commitment", "Sole Tenancy", "Custom", "Premium"}

func newGCPPricer(ctx context.Context, credentialsFile string) (*gcpPricer, error) {
	var opts []option.ClientOption
	if credentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(credentialsFile))
	}
	cli, err := billing.NewCloudCatalogClient(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return &gcpPricer{
		cli:      cli,
		core:     map[string]float64{},
		ram:      map[string]float64{},
		spotCore: map[string]float64{},
		spotRam:  map[string]float64{},
	}, nil
}

// build lazily indexes all Compute Engine core/ram SKUs once.
func (p *gcpPricer) build(ctx context.Context) error {
	it := p.cli.ListSkus(ctx, &billingpb.ListSkusRequest{Parent: computeServiceName})
	for {
		sku, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		p.indexSku(sku)
	}
	return nil
}

func (p *gcpPricer) indexSku(sku *billingpb.Sku) {
	cat := sku.GetCategory()
	if cat.GetResourceFamily() != "Compute" {
		return
	}
	desc := sku.GetDescription()
	var coreMap, ramMap map[string]float64
	switch cat.GetUsageType() {
	case usageOnDemand:
		// Spot SKUs carry usage type "Preemptible", but guard the description
		// too so a mislabeled catalog entry can't pollute the on-demand table.
		if strings.Contains(desc, "Spot") || strings.Contains(desc, "Preemptible") {
			return
		}
		coreMap, ramMap = p.core, p.ram
	case usagePreemptible:
		coreMap, ramMap = p.spotCore, p.spotRam
	default:
		return
	}
	for _, t := range priceExcludeTokens {
		if strings.Contains(desc, t) {
			return
		}
	}
	fam := familyFromDesc(desc)
	if fam == "" {
		return
	}
	var target map[string]float64
	switch {
	case strings.Contains(desc, "Core"):
		target = coreMap
	case strings.Contains(desc, "Ram"):
		target = ramMap
	default:
		return
	}
	price, ok := skuUnitPrice(sku)
	if !ok {
		return
	}
	for _, region := range sku.GetServiceRegions() {
		target[region+"/"+fam] = price
	}
}

// Price implements Pricer.
func (p *gcpPricer) Price(ctx context.Context, in model.Instance) (PriceInfo, bool) {
	p.once.Do(func() { p.err = p.build(ctx) })
	if p.err != nil {
		return PriceInfo{}, false
	}
	coreMap, ramMap := p.core, p.ram
	source := sourceGCPCatalog
	if in.ProvisioningModel == model.ProvisioningSpot {
		// Never fall back to on-demand rates for a Spot instance: a missing
		// Spot SKU leaves it unpriced rather than overstated ~4x.
		coreMap, ramMap = p.spotCore, p.spotRam
		source = sourceGCPCatalogSpot
	}
	fam := familyOf(in.MachineType)
	key := in.Region + "/" + fam
	corePrice, hasCore := coreMap[key]
	ramPrice, hasRam := ramMap[key]
	if !hasCore || !hasRam || in.VCPU == 0 {
		return PriceInfo{}, false
	}
	hourly := in.VCPU*corePrice + in.MemGB*ramPrice
	if hourly <= 0 {
		return PriceInfo{}, false
	}
	return mk(hourly, source), true
}

// familyFromDesc finds a family token as a whole word in a SKU description,
// e.g. "E2 Instance Core running in Americas" -> "e2".
func familyFromDesc(desc string) string {
	for _, f := range familyDisplay {
		if strings.Contains(desc, f.token+" ") {
			return f.key
		}
	}
	return ""
}

// familyOf extracts the family prefix of a machine type ("n2d-highmem-8" -> "n2d").
func familyOf(machineType string) string {
	if i := strings.Index(machineType, "-"); i >= 0 {
		return strings.ToLower(machineType[:i])
	}
	return strings.ToLower(machineType)
}

// skuUnitPrice returns the first-tier USD unit price of a SKU.
func skuUnitPrice(sku *billingpb.Sku) (float64, bool) {
	infos := sku.GetPricingInfo()
	if len(infos) == 0 {
		return 0, false
	}
	expr := infos[len(infos)-1].GetPricingExpression()
	rates := expr.GetTieredRates()
	if len(rates) == 0 {
		return 0, false
	}
	up := rates[0].GetUnitPrice()
	if up.GetCurrencyCode() != "USD" {
		return 0, false
	}
	return float64(up.GetUnits()) + float64(up.GetNanos())/1e9, true
}
