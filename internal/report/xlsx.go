package report

import (
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"
)

// RenderXLSX writes the report as a multi-sheet Excel workbook:
//   - "Instances": inventory + utilization + bound
//   - "NodePools": GKE/EKS nodepool inventory
//   - "Summary":   roll-up counts
func RenderXLSX(out io.Writer, r Report) error {
	f := excelize.NewFile()
	defer f.Close()

	// Instances sheet (rename default Sheet1).
	const inst = "Instances"
	f.SetSheetName("Sheet1", inst)
	instHeader := []any{"Name", "Instance ID", "Provider", "Project", "Region", "Zone",
		"Machine Type", "vCPU", "Mem (GB)", "Disk (GB)", "NIC (Gbps)",
		"CPU p95", "Mem p95", "Net p95", "Disk p95", "Bound",
		"$/hr", "$/mo", "Price Source", "Notes"}
	writeRow(f, inst, 1, instHeader)
	for i, ir := range r.Instances {
		in := ir.Instance
		a := ir.Analysis
		var hourly, monthly, source any
		if ir.Pricing != nil {
			hourly, monthly, source = ir.Pricing.HourlyUSD, ir.Pricing.MonthlyUSD, ir.Pricing.Source
		}
		writeRow(f, inst, i+2, []any{
			in.Name, in.ID, in.Provider, in.Project, in.Region, in.Zone,
			in.MachineType, in.VCPU, in.MemGB, in.DiskGB(), in.NICGbps,
			cell(a.Norm["cpu"]), cell(a.Norm["mem"]), cell(a.Norm["net"]), cell(a.Norm["disk"]),
			string(a.Bound), hourly, monthly, source, joinNotes(a.Notes),
		})
	}
	boldHeader(f, inst, len(instHeader))

	// NodePools sheet.
	const pools = "NodePools"
	f.NewSheet(pools)
	poolHeader := []any{"Cluster", "Name", "Provider", "Region", "Machine Type", "vCPU", "Mem (GB)", "Node Count"}
	writeRow(f, pools, 1, poolHeader)
	for i, p := range r.NodePools {
		writeRow(f, pools, i+2, []any{p.Cluster, p.Name, p.Provider, p.Region, p.MachineType, p.VCPU, p.MemGB, p.NodeCount})
	}
	boldHeader(f, pools, len(poolHeader))

	// Summary sheet.
	const sum = "Summary"
	f.NewSheet(sum)
	writeRow(f, sum, 1, []any{"Metric", "Value"})
	rows := [][]any{
		{"Provider", r.Provider},
		{"Generated At", r.GeneratedAt.Format("2006-01-02 15:04 MST")},
		{"Lookback (hours)", r.Window.LookbackHours},
		{"Instances", r.Summary.InstanceCount},
		{"Node Pools", r.Summary.NodePoolCount},
		{"Total vCPU", r.Summary.TotalVCPU},
		{"Total Mem (GB)", r.Summary.TotalMemGB},
	}
	if r.Cost != nil {
		rows = append(rows,
			[]any{"On-demand $/hr", r.Cost.TotalHourlyUSD},
			[]any{"On-demand $/mo", r.Cost.TotalMonthlyUSD},
			[]any{"Priced instances", r.Cost.PricedInstances},
			[]any{"Unpriced instances", r.Cost.UnpricedInstance},
		)
	}
	if s := r.SavingsScore; s != nil {
		rows = append(rows, []any{"Savings Score", savingsHeadline(s)})
		if s.RecoverableMonthlyUSD != nil {
			rows = append(rows, []any{"Recoverable $/mo (est)", *s.RecoverableMonthlyUSD})
		}
		if s.UnderutilizedSpendPct != nil {
			rows = append(rows, []any{"Under-utilized spend %", *s.UnderutilizedSpendPct})
		}
		rows = append(rows,
			[]any{"Under-utilized instance %", s.UnderutilizedInstancePct},
			[]any{"Always-on instance %", s.AlwaysOnInstancePct},
		)
	}
	rn := 2
	for _, row := range rows {
		writeRow(f, sum, rn, row)
		rn++
	}
	writeRow(f, sum, rn, []any{"Bound breakdown", boundLine(r.Summary.BoundCounts)})
	boldHeader(f, sum, 2)

	_, err := f.WriteTo(out)
	return err
}

func writeRow(f *excelize.File, sheet string, row int, vals []any) {
	for c, v := range vals {
		cellRef, _ := excelize.CoordinatesToCellName(c+1, row)
		_ = f.SetCellValue(sheet, cellRef, v)
	}
}

func boldHeader(f *excelize.File, sheet string, ncols int) {
	style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return
	}
	last, _ := excelize.CoordinatesToCellName(ncols, 1)
	_ = f.SetCellStyle(sheet, "A1", last, style)
}

// cell converts a normalized fraction to a spreadsheet-friendly value.
func cell(v float64) any {
	if v < 0 {
		return "" // absent metric
	}
	return fmt.Sprintf("%.1f%%", v*100)
}
