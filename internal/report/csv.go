package report

import (
	"encoding/csv"
	"io"
	"strconv"
)

// RenderCSV writes one row per instance with inventory + analysis columns.
// This is a primary shareable deliverable for the customer.
func RenderCSV(out io.Writer, r Report) error {
	w := csv.NewWriter(out)
	defer w.Flush()

	header := []string{
		"name", "instance_id", "provider", "project", "region", "zone",
		"machine_type", "vcpu", "mem_gb", "disk_gb", "nic_gbps",
		"cpu_p95", "mem_p95", "net_p95", "disk_p95", "bound",
		"hourly_usd", "monthly_usd", "price_source", "notes",
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, ir := range r.Instances {
		in := ir.Instance
		a := ir.Analysis
		hourly, monthly, source := "", "", ""
		if ir.Pricing != nil {
			hourly = f(ir.Pricing.HourlyUSD)
			monthly = f(ir.Pricing.MonthlyUSD)
			source = ir.Pricing.Source
		}
		row := []string{
			in.Name, in.ID, in.Provider, in.Project, in.Region, in.Zone,
			in.MachineType, f(in.VCPU), f(in.MemGB), f(in.DiskGB()), f(in.NICGbps),
			frac(a.Norm["cpu"]), frac(a.Norm["mem"]), frac(a.Norm["net"]), frac(a.Norm["disk"]),
			string(a.Bound), hourly, monthly, source, joinNotes(a.Notes),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

func f(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func frac(v float64) string {
	if v < 0 {
		return "" // absent
	}
	return strconv.FormatFloat(v, 'f', 4, 64)
}

func joinNotes(ns []string) string {
	s := ""
	for i, n := range ns {
		if i > 0 {
			s += "; "
		}
		s += n
	}
	return s
}
