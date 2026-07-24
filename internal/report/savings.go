package report

import (
	"fmt"
	"strconv"

	"github.com/footprintai/telescope/internal/savings"
)

// savingsHeadline renders the one-line Savings Score headline. With pricing it
// leads with estimated recoverable dollars; without pricing it shows the
// count-weighted signals and points the reader at --pricing. Returns "" when no
// score is present.
func savingsHeadline(s *savings.Score) string {
	if s == nil {
		return ""
	}
	alwaysOn := fmt.Sprintf("%d of %d instances always-on", s.Basis.AlwaysOnInstances, s.Basis.InstanceCount)
	if s.RecoverableMonthlyUSD != nil && s.UnderutilizedSpendPct != nil {
		return fmt.Sprintf("Savings Score: ~%s/mo estimated recoverable (%s of spend under-utilized, %s)",
			money0(*s.RecoverableMonthlyUSD), pct0(*s.UnderutilizedSpendPct), alwaysOn)
	}
	return fmt.Sprintf("Savings Score: %s of instances under-utilized, %s — run with --pricing for dollar estimates",
		pct0(s.UnderutilizedInstancePct), alwaysOn)
}

// money0 formats a dollar amount rounded to the whole dollar with thousands
// separators, e.g. 1306.7 -> "$1,307".
func money0(v float64) string {
	n := int64(v + 0.5)
	neg := ""
	if n < 0 {
		neg, n = "-", -n
	}
	s := strconv.FormatInt(n, 10)
	var out []byte
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return neg + "$" + string(out)
}

func pct0(v float64) string { return fmt.Sprintf("%.0f%%", v) }
