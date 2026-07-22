// Package cli implements the telescope command-line interface.
//
// Commands:
//
//	telescope scan   Read topology + metrics -> shareable report
//
// telescope is Stage A only: it produces a report (CSV/Excel/Markdown/JSON)
// that the customer hands over. The Containarium recommendation is generated
// separately by the cloud service from that report.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

// Run dispatches a subcommand. Returns a process exit code.
func Run(args []string) int {
	if len(args) < 1 {
		usage(os.Stderr)
		return 2
	}
	ctx := context.Background()
	switch args[0] {
	case "scan":
		return runScan(ctx, args[1:])
	case "-h", "--help", "help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		usage(os.Stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `telescope - cloud workload inventory & utilization reporter

USAGE:
  telescope scan [flags]   Inventory + utilization -> shareable report

The report (JSON) is the handoff to the cloud service, which generates the
Containarium consolidation recommendation.

Run 'telescope scan -h' for flags.
`)
}

// parseLookback accepts values like "14d", "24h", "90m".
func parseLookback(s string) (time.Duration, error) {
	if len(s) > 1 && s[len(s)-1] == 'd' {
		days, err := time.ParseDuration(s[:len(s)-1] + "h")
		if err != nil {
			return 0, err
		}
		return days * 24, nil
	}
	return time.ParseDuration(s)
}
