package report

import (
	"fmt"
	"io"
)

// PrintConsole writes a structured, human-readable summary of the scan. In
// Slice 1 this is the counts plus a parse-warning tally.
func PrintConsole(w io.Writer, r Report) {
	fmt.Fprintln(w, "prom-ai-guard scan")
	fmt.Fprintf(w, "  scan_id:            %s\n", r.ScanID)
	fmt.Fprintf(w, "  source:             %s (%s)\n", r.Source.SourceType, r.Source.InputRef)
	fmt.Fprintf(w, "  total_series:       %d\n", r.Summary.TotalSeries)
	fmt.Fprintf(w, "  total_metric_names: %d\n", r.Summary.TotalMetricNames)
	fmt.Fprintf(w, "  parse_warnings:     %d\n", len(r.Warnings))
	for _, warn := range r.Warnings {
		fmt.Fprintf(w, "    - line %d: %s\n", warn.Line, warn.Reason)
	}
}
