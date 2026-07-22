package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/footprintai/telescope/internal/analyze"
	"github.com/footprintai/telescope/internal/model"
	"github.com/footprintai/telescope/internal/provider"
	"github.com/footprintai/telescope/internal/pricing"
	awsprov "github.com/footprintai/telescope/internal/provider/aws"
	"github.com/footprintai/telescope/internal/provider/gcp"
	"github.com/footprintai/telescope/internal/provider/mock"
	"github.com/footprintai/telescope/internal/report"
)

func runScan(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	var (
		providerName = fs.String("provider", "mock", "cloud provider: gcp|aws|mock")
		projects     = fs.String("projects", "", "comma-separated GCP projects / AWS accounts")
		regions      = fs.String("regions", "", "comma-separated regions (empty = all)")
		credentials  = fs.String("credentials", "", "path to read-only SA JSON / AWS creds (or use env)")
		lookbackStr  = fs.String("lookback", "14d", "metrics lookback window, e.g. 14d, 24h")
		format       = fs.String("output", "table", "output format: table|json|csv|markdown|xlsx")
		outPath      = fs.String("out", "", "write output to file instead of stdout")
		withPricing  = fs.Bool("pricing", false, "attach live on-demand price metadata (best-effort)")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	lookback, err := parseLookback(*lookbackStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid --lookback: %v\n", err)
		return 2
	}
	win := model.Window{Lookback: lookback, End: time.Now().UTC()}

	cfg := provider.Config{
		Projects:        splitCSV(*projects),
		Regions:         splitCSV(*regions),
		CredentialsFile: *credentials,
	}

	prov, err := newProvider(ctx, *providerName, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "provider init: %v\n", err)
		return 1
	}

	insts, err := prov.ListInstances(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list instances: %v\n", err)
		return 1
	}
	pools, err := prov.ListNodePools(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list nodepools: %v\n", err)
		return 1
	}
	if _, err := prov.FetchMetrics(ctx, insts, win); err != nil {
		fmt.Fprintf(os.Stderr, "fetch metrics: %v\n", err)
		return 1
	}

	results := analyze.All(insts, analyze.Defaults())

	var prices map[string]*pricing.PriceInfo
	if *withPricing {
		prices = collectPricing(ctx, prov.Name(), *credentials, insts)
	}

	rep := report.Build(prov.Name(), win, insts, pools, results, prices)

	out := os.Stdout
	if *outPath != "" {
		fh, err := os.Create(*outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "create %s: %v\n", *outPath, err)
			return 1
		}
		defer fh.Close()
		out = fh
	}

	switch strings.ToLower(*format) {
	case "table":
		report.RenderTable(out, rep)
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintf(os.Stderr, "encode json: %v\n", err)
			return 1
		}
	case "csv":
		if err := report.RenderCSV(out, rep); err != nil {
			fmt.Fprintf(os.Stderr, "encode csv: %v\n", err)
			return 1
		}
	case "markdown", "md":
		report.RenderMarkdown(out, rep)
	case "xlsx", "excel":
		if *outPath == "" {
			fmt.Fprintln(os.Stderr, "xlsx output requires --out <file.xlsx>")
			return 2
		}
		if err := report.RenderXLSX(out, rep); err != nil {
			fmt.Fprintf(os.Stderr, "encode xlsx: %v\n", err)
			return 1
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown --output %q\n", *format)
		return 2
	}
	return 0
}

// newProvider builds the requested provider. GCP/AWS are wired in later
// milestones; only mock is available today.
func newProvider(ctx context.Context, name string, cfg provider.Config) (provider.Provider, error) {
	switch name {
	case "mock":
		return mock.New(), nil
	case "gcp":
		return gcp.New(ctx, cfg)
	case "aws":
		return awsprov.New(ctx, cfg)
	default:
		return nil, fmt.Errorf("unknown provider %q", name)
	}
}

// collectPricing best-effort resolves a price for each instance. Failures are
// warned about but never abort the scan; unpriced instances are simply omitted
// from the returned map.
func collectPricing(ctx context.Context, providerName, credentials string, insts []model.Instance) map[string]*pricing.PriceInfo {
	pricer, err := pricing.New(ctx, providerName, credentials)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: pricing disabled: %v\n", err)
		return nil
	}
	out := make(map[string]*pricing.PriceInfo, len(insts))
	for _, in := range insts {
		if pi, ok := pricer.Price(ctx, in); ok {
			p := pi
			out[in.ID] = &p
		}
	}
	return out
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
