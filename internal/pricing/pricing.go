// Package pricing enriches the report with on-demand price metadata for each
// instance, fetched live from the provider's pricing API (best-effort).
//
// This runs client-side as part of Stage A: it only annotates the report with
// list prices; it performs no recommendation or bin-packing.
package pricing

import (
	"context"

	"github.com/footprintai/telescope/internal/model"
)

// HoursPerMonth is the convention used to project monthly cost.
const HoursPerMonth = 730.0

// PriceInfo is the on-demand price attached to an instance in the report.
type PriceInfo struct {
	HourlyUSD  float64 `json:"hourly_usd"`
	MonthlyUSD float64 `json:"monthly_usd"`
	Currency   string  `json:"currency"`
	Source     string  `json:"source"` // e.g. aws-pricing-api, gcp-billing-catalog, static
}

// Pricer returns the on-demand price for an instance. ok is false when no price
// could be resolved (unknown type/region, missing permission, API error).
type Pricer interface {
	Price(ctx context.Context, in model.Instance) (PriceInfo, bool)
}

// New returns a live Pricer for the given provider, or a static table for mock
// / unknown providers so the feature is demonstrable offline.
func New(ctx context.Context, providerName, credentialsFile string) (Pricer, error) {
	switch providerName {
	case "aws":
		return newAWSPricer(ctx, credentialsFile)
	case "gcp":
		return newGCPPricer(ctx, credentialsFile)
	default:
		return NewStatic(), nil
	}
}

// mk builds a PriceInfo from an hourly USD figure.
func mk(hourly float64, source string) PriceInfo {
	return PriceInfo{
		HourlyUSD:  round4(hourly),
		MonthlyUSD: round2(hourly * HoursPerMonth),
		Currency:   "USD",
		Source:     source,
	}
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
func round4(f float64) float64 { return float64(int(f*10000+0.5)) / 10000 }
