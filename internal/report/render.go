package report

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/footprintai/telescope/internal/pricing"
)

// RenderTable writes a human-readable table of the report to w.
func RenderTable(out io.Writer, r Report) {
	fmt.Fprintf(out, "telescope report  (provider=%s, lookback=%.0fh, generated=%s)\n\n",
		r.Provider, r.Window.LookbackHours, r.GeneratedAt.Format("2006-01-02 15:04 MST"))

	if h := savingsHeadline(r.SavingsScore); h != "" {
		fmt.Fprintf(out, "%s\n\n", h)
	}

	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTYPE\tvCPU\tMEM(GB)\tCPU p95\tMEM p95\tBOUND\t$/HR")
	for _, ir := range r.Instances {
		in := ir.Instance
		cpu := pct(ir.Analysis.Norm["cpu"])
		mem := pct(ir.Analysis.Norm["mem"])
		fmt.Fprintf(tw, "%s\t%s\t%.0f\t%.0f\t%s\t%s\t%s\t%s\n",
			in.Name, in.MachineType, in.VCPU, in.MemGB, cpu, mem, ir.Analysis.Bound, price(ir.Pricing))
	}
	tw.Flush()

	fmt.Fprintf(out, "\nSummary: %d instances, %d nodepools, %.0f vCPU, %.0f GB\n",
		r.Summary.InstanceCount, r.Summary.NodePoolCount, r.Summary.TotalVCPU, r.Summary.TotalMemGB)
	fmt.Fprintf(out, "Bound breakdown: %s\n", boundLine(r.Summary.BoundCounts))
	if r.Cost != nil {
		fmt.Fprintf(out, "On-demand list cost: $%.2f/hr, $%.2f/mo (%d priced, %d unpriced)\n",
			r.Cost.TotalHourlyUSD, r.Cost.TotalMonthlyUSD, r.Cost.PricedInstances, r.Cost.UnpricedInstance)
	}
}

// RenderMarkdown writes a Markdown version of the report.
func RenderMarkdown(out io.Writer, r Report) {
	fmt.Fprintf(out, "# telescope report\n\n")
	fmt.Fprintf(out, "- **Provider:** %s\n- **Lookback:** %.0fh\n- **Generated:** %s\n\n",
		r.Provider, r.Window.LookbackHours, r.GeneratedAt.Format("2006-01-02 15:04 MST"))
	if h := savingsHeadline(r.SavingsScore); h != "" {
		fmt.Fprintf(out, "## Savings Score\n\n%s\n\n", h)
	}
	fmt.Fprintf(out, "## Instances\n\n")
	fmt.Fprintln(out, "| Name | Type | vCPU | Mem (GB) | CPU p95 | Mem p95 | Bound | $/hr |")
	fmt.Fprintln(out, "|------|------|-----:|---------:|--------:|--------:|-------|-----:|")
	for _, ir := range r.Instances {
		in := ir.Instance
		fmt.Fprintf(out, "| %s | %s | %.0f | %.0f | %s | %s | %s | %s |\n",
			in.Name, in.MachineType, in.VCPU, in.MemGB,
			pct(ir.Analysis.Norm["cpu"]), pct(ir.Analysis.Norm["mem"]), ir.Analysis.Bound, price(ir.Pricing))
	}
	fmt.Fprintf(out, "\n## Summary\n\n")
	fmt.Fprintf(out, "- Instances: %d\n- Nodepools: %d\n- Total vCPU: %.0f\n- Total Mem: %.0f GB\n- Bound breakdown: %s\n",
		r.Summary.InstanceCount, r.Summary.NodePoolCount, r.Summary.TotalVCPU, r.Summary.TotalMemGB,
		boundLine(r.Summary.BoundCounts))
	if r.Cost != nil {
		fmt.Fprintf(out, "- On-demand list cost: **$%.2f/hr**, $%.2f/mo (%d priced, %d unpriced)\n",
			r.Cost.TotalHourlyUSD, r.Cost.TotalMonthlyUSD, r.Cost.PricedInstances, r.Cost.UnpricedInstance)
	}
}

func pct(v float64) string {
	if v < 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.0f%%", v*100)
}

func price(p *pricing.PriceInfo) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%.4f", p.HourlyUSD)
}

func boundLine(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := ""
	for i, k := range keys {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("%s=%d", k, m[k])
	}
	return s
}
