package pricing

import (
	"context"
	"encoding/json"
	"strconv"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awspricing "github.com/aws/aws-sdk-go-v2/service/pricing"
	awspricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"

	"github.com/footprintai/telescope/internal/model"
)

// awsPricer resolves EC2 on-demand prices via the AWS Price List Query API.
// The API is served only from a few regions; we pin us-east-1.
type awsPricer struct {
	cli   *awspricing.Client
	cache map[string]PriceInfo // "region/instanceType" -> price (present even if empty => "looked up, none")
}

func newAWSPricer(ctx context.Context, credentialsFile string) (*awsPricer, error) {
	opts := []func(*config.LoadOptions) error{config.WithRegion("us-east-1")}
	if credentialsFile != "" {
		opts = append(opts, config.WithSharedCredentialsFiles([]string{credentialsFile}))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return &awsPricer{cli: awspricing.NewFromConfig(cfg), cache: map[string]PriceInfo{}}, nil
}

func filter(field, value string) awspricingtypes.Filter {
	return awspricingtypes.Filter{
		Type:  awspricingtypes.FilterTypeTermMatch,
		Field: awssdk.String(field),
		Value: awssdk.String(value),
	}
}

// Price implements Pricer.
func (p *awsPricer) Price(ctx context.Context, in model.Instance) (PriceInfo, bool) {
	// The Price List API serves on-demand terms only; Spot prices are dynamic
	// (DescribeSpotPriceHistory). Leave Spot instances unpriced rather than
	// overstating them at on-demand rates.
	if in.ProvisioningModel == model.ProvisioningSpot {
		return PriceInfo{}, false
	}
	key := in.Region + "/" + in.MachineType
	if v, ok := p.cache[key]; ok {
		return v, v.HourlyUSD > 0
	}
	out, err := p.cli.GetProducts(ctx, &awspricing.GetProductsInput{
		ServiceCode: awssdk.String("AmazonEC2"),
		Filters: []awspricingtypes.Filter{
			filter("instanceType", in.MachineType),
			filter("regionCode", in.Region),
			filter("operatingSystem", "Linux"),
			filter("tenancy", "Shared"),
			filter("preInstalledSw", "NA"),
			filter("capacitystatus", "Used"),
		},
	})
	if err != nil || len(out.PriceList) == 0 {
		p.cache[key] = PriceInfo{}
		return PriceInfo{}, false
	}
	hourly, ok := parseAWSPrice(out.PriceList[0])
	if !ok {
		p.cache[key] = PriceInfo{}
		return PriceInfo{}, false
	}
	info := mk(hourly, "aws-pricing-api")
	p.cache[key] = info
	return info, true
}

// parseAWSPrice extracts the on-demand USD/hour from a Price List JSON document.
func parseAWSPrice(doc string) (float64, bool) {
	var d struct {
		Terms struct {
			OnDemand map[string]struct {
				PriceDimensions map[string]struct {
					PricePerUnit struct {
						USD string `json:"USD"`
					} `json:"pricePerUnit"`
				} `json:"priceDimensions"`
			} `json:"OnDemand"`
		} `json:"terms"`
	}
	if err := json.Unmarshal([]byte(doc), &d); err != nil {
		return 0, false
	}
	for _, term := range d.Terms.OnDemand {
		for _, dim := range term.PriceDimensions {
			if v, err := strconv.ParseFloat(dim.PricePerUnit.USD, 64); err == nil && v > 0 {
				return v, true
			}
		}
	}
	return 0, false
}
