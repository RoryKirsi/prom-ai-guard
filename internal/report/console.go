package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// consoleRiskListN caps how many invalid metrics the per-metric risk list prints.
const consoleRiskListN = 10

// PrintConsole writes a structured, human-readable summary of the scan: counts,
// invalid totals, risk distribution, the invalid-metric risk list, and warnings.
func PrintConsole(w io.Writer, r Report) {
	s := r.Summary
	fmt.Fprintln(w, "prom-ai-guard scan")
	fmt.Fprintf(w, "  scan_id:            %s\n", r.ScanID)
	if r.Source.SourceType == "prometheus_api" {
		fmt.Fprintf(w, "  source:             prometheus_api %s (metadata-oriented; series values are 0 placeholders)\n", r.Source.PromURL)
	} else {
		fmt.Fprintf(w, "  source:             %s (%s)\n", r.Source.SourceType, r.Source.InputRef)
	}
	fmt.Fprintf(w, "  total_series:       %d\n", s.TotalSeries)
	fmt.Fprintf(w, "  total_metric_names: %d\n", s.TotalMetricNames)
	fmt.Fprintf(w, "  valid / invalid:    %d / %d (ratio %.4f)\n", s.ValidMetricNames, s.InvalidMetricNames, s.InvalidRatio)
	fmt.Fprintf(w, "  risk_distribution:  severe=%d warning=%d minor=%d\n",
		s.RiskDistribution["severe"], s.RiskDistribution["warning"], s.RiskDistribution["minor"])

	fmt.Fprintln(w, "  invalid_type_counts:")
	for _, t := range sortedKeys(s.InvalidTypeCounts) {
		fmt.Fprintf(w, "    - %-20s %d\n", t, s.InvalidTypeCounts[t])
	}

	// Per-metric risk list (无效指标风险列表): the top invalid metrics ranked by
	// risk. r.TopRiskMetrics is already sorted by score desc; show up to N.
	if n := len(r.TopRiskMetrics); n > 0 {
		shown := n
		if shown > consoleRiskListN {
			shown = consoleRiskListN
		}
		fmt.Fprintf(w, "  invalid_metrics (risk list, top %d of %d):\n", shown, n)
		for _, m := range r.TopRiskMetrics[:shown] {
			fmt.Fprintf(w, "    - %-34s %-8s (%d)  %s\n",
				m.MetricName, m.RiskLevel, m.RiskScore, strings.Join(m.InvalidTypes, ","))
		}
	}

	if si := s.StorageImpact; si != nil {
		fmt.Fprintf(w, "  storage_impact:     high=%d medium=%d low=%d est_invalid_index_entries=%d (heuristic)\n",
			si.HighImpactMetrics, si.MediumImpactMetrics, si.LowImpactMetrics, si.EstimatedInvalidIndexEntries)
	}

	if ga := s.GovernanceAssessment; ga != nil {
		top := "none"
		if len(ga.TopSystemicIssues) > 0 {
			t := ga.TopSystemicIssues[0]
			top = fmt.Sprintf("%s(%s×%d)", t.InvalidType, t.MaxRisk, t.MetricCount)
		}
		fmt.Fprintf(w, "  governance:         grade %s (score %d, heuristic) top_issue=%s\n",
			ga.MaturityGrade, ga.MaturityScore, top)
	}

	if r.AI != nil {
		fmt.Fprintf(w, "  ai:                 %s (mode=%s model=%s analyzed=%d)\n",
			r.AI.Status, r.AI.AIMode, r.AI.Model, r.AI.AnalyzedMetricCount)
		if r.AI.FallbackReason != "" {
			fmt.Fprintf(w, "    fallback_reason:  %s\n", r.AI.FallbackReason)
		}
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
