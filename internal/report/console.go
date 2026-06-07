package report

import (
	"fmt"
	"io"
	"sort"
)

// PrintConsole writes a structured, human-readable summary of the scan: counts,
// invalid totals, risk distribution, invalid-type breakdown and parse warnings.
func PrintConsole(w io.Writer, r Report) {
	s := r.Summary
	fmt.Fprintln(w, "prom-ai-guard scan")
	fmt.Fprintf(w, "  scan_id:            %s\n", r.ScanID)
	fmt.Fprintf(w, "  source:             %s (%s)\n", r.Source.SourceType, r.Source.InputRef)
	fmt.Fprintf(w, "  total_series:       %d\n", s.TotalSeries)
	fmt.Fprintf(w, "  total_metric_names: %d\n", s.TotalMetricNames)
	fmt.Fprintf(w, "  valid / invalid:    %d / %d (ratio %.4f)\n", s.ValidMetricNames, s.InvalidMetricNames, s.InvalidRatio)
	fmt.Fprintf(w, "  risk_distribution:  severe=%d warning=%d minor=%d\n",
		s.RiskDistribution["severe"], s.RiskDistribution["warning"], s.RiskDistribution["minor"])

	fmt.Fprintln(w, "  invalid_type_counts:")
	for _, t := range sortedKeys(s.InvalidTypeCounts) {
		fmt.Fprintf(w, "    - %-20s %d\n", t, s.InvalidTypeCounts[t])
	}

	fmt.Fprintf(w, "  parse_warnings:     %d\n", len(r.Warnings))
	for _, warn := range r.Warnings {
		fmt.Fprintf(w, "    - line %d: %s\n", warn.Line, warn.Reason)
	}
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
