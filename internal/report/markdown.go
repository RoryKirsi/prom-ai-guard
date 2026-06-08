package report

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"prom-ai-guard/internal/model"
)

// riskOrder is the fixed display order for risk levels.
var riskOrder = []string{"severe", "warning", "minor"}

// RenderMarkdown renders the report.Report as a human-readable Markdown document.
// It only reads r — no analysis is re-run — and produces deterministic output
// (map-backed sections use sorted keys).
func RenderMarkdown(r Report) string {
	var b strings.Builder
	b.WriteString("# prom-ai-guard analysis report\n\n")

	// Scan
	b.WriteString("## Scan\n\n")
	fmt.Fprintf(&b, "- scan_id: `%s`\n", r.ScanID)
	fmt.Fprintf(&b, "- scan_time: %s\n", r.ScanTime)
	fmt.Fprintf(&b, "- tool_version: %s\n", r.ToolVersion)
	fmt.Fprintf(&b, "- config_hash: `%s`\n\n", r.ConfigHash)

	// Source
	b.WriteString("## Source\n\n")
	fmt.Fprintf(&b, "- source_type: %s\n", r.Source.SourceType)
	fmt.Fprintf(&b, "- input_ref: `%s`\n", r.Source.InputRef)
	fmt.Fprintf(&b, "- scan_scope: %s\n", r.Source.ScanScope)
	fmt.Fprintf(&b, "- series_count: %d\n", r.Source.SeriesCount)
	fmt.Fprintf(&b, "- metric_name_count: %d\n\n", r.Source.MetricNameCount)

	// AI analysis
	b.WriteString("## AI analysis\n\n")
	if r.AI != nil {
		fmt.Fprintf(&b, "- provider: %s\n", r.AI.Provider)
		fmt.Fprintf(&b, "- model: %s\n", r.AI.Model)
		fmt.Fprintf(&b, "- ai_mode: %s (scope: %s)\n", r.AI.AIMode, r.AI.AIScope)
		fmt.Fprintf(&b, "- status: %s\n", r.AI.Status)
		if r.AI.FallbackReason != "" {
			fmt.Fprintf(&b, "- fallback_reason: %s\n", r.AI.FallbackReason)
		}
		fmt.Fprintf(&b, "- analyzed_metric_count: %d\n", r.AI.AnalyzedMetricCount)
		if strings.TrimSpace(r.AI.Summary) != "" {
			fmt.Fprintf(&b, "- ai_summary: %s\n", r.AI.Summary)
		}
	} else {
		b.WriteString("- (no AI block)\n")
	}
	b.WriteString("\n> Note: the AI summary is advisory. The counts below (Summary, ")
	b.WriteString("Risk distribution, Invalid type counts) are the authoritative deterministic results.\n\n")

	// Summary
	s := r.Summary
	b.WriteString("## Summary\n\n")
	fmt.Fprintf(&b, "- total_series: %d\n", s.TotalSeries)
	fmt.Fprintf(&b, "- total_metric_names: %d\n", s.TotalMetricNames)
	fmt.Fprintf(&b, "- valid_metric_names: %d\n", s.ValidMetricNames)
	fmt.Fprintf(&b, "- invalid_metric_names: %d\n", s.InvalidMetricNames)
	fmt.Fprintf(&b, "- invalid_ratio: %.4f\n\n", s.InvalidRatio)

	// Risk distribution (fixed order)
	b.WriteString("## Risk distribution\n\n")
	for _, level := range riskOrder {
		fmt.Fprintf(&b, "- %s: %d\n", level, s.RiskDistribution[level])
	}
	b.WriteString("\n")

	// Invalid type counts (sorted)
	b.WriteString("## Invalid type counts\n\n")
	for _, k := range sortedIntKeys(s.InvalidTypeCounts) {
		fmt.Fprintf(&b, "- %s: %d\n", k, s.InvalidTypeCounts[k])
	}
	b.WriteString("\n")

	// Top risk metrics
	b.WriteString("## Top risk metrics\n\n")
	if len(r.TopRiskMetrics) == 0 {
		b.WriteString("_none_\n\n")
	} else {
		b.WriteString("| metric_name | risk_level | risk_score | invalid_types |\n")
		b.WriteString("|---|---|---|---|\n")
		for _, m := range r.TopRiskMetrics {
			fmt.Fprintf(&b, "| %s | %s | %d | %s |\n", m.MetricName, m.RiskLevel, m.RiskScore, strings.Join(m.InvalidTypes, ", "))
		}
		b.WriteString("\n")
	}

	// Top violation labels
	b.WriteString("## Top violation labels\n\n")
	if len(r.TopViolationLabels) == 0 {
		b.WriteString("_none_\n\n")
	} else {
		b.WriteString("| label_key | invalid_type | risk_level | risk_score | metric_count | series_count | sample_metric_names |\n")
		b.WriteString("|---|---|---|---|---|---|---|\n")
		for _, v := range r.TopViolationLabels {
			fmt.Fprintf(&b, "| %s | %s | %s | %d | %d | %d | %s |\n",
				v.LabelKey, v.InvalidType, v.RiskLevel, v.RiskScore, v.MetricCount, v.SeriesCount, strings.Join(v.SampleMetricNames, ", "))
		}
		b.WriteString("\n")
	}

	// Invalid metric details
	b.WriteString("## Invalid metric details\n\n")
	if len(r.InvalidMetrics) == 0 {
		b.WriteString("_none_\n\n")
	} else {
		for _, m := range r.InvalidMetrics {
			renderInvalidMetric(&b, m)
		}
	}

	// Parse warnings
	b.WriteString("## Parse warnings\n\n")
	if len(r.Warnings) == 0 {
		b.WriteString("_none_\n\n")
	} else {
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "- line %d: %s\n", w.Line, w.Reason)
		}
		b.WriteString("\n")
	}

	// Report files
	b.WriteString("## Report files\n\n")
	b.WriteString("- analysis_report.json (machine contract)\n")
	b.WriteString("- analysis_report.md (this report)\n")
	b.WriteString("- analysis_report.xlsx\n")
	b.WriteString("- ai_input_preview.json (redacted AI input)\n")

	return b.String()
}

func renderInvalidMetric(b *strings.Builder, m model.MetricAnalysis) {
	fmt.Fprintf(b, "### %s\n\n", m.MetricName)
	fmt.Fprintf(b, "- invalid_types: %s\n", strings.Join(m.InvalidTypes, ", "))
	fmt.Fprintf(b, "- risk: %s (score %d)\n", m.RiskLevel, m.RiskScore)
	if m.RiskReason != "" {
		fmt.Fprintf(b, "- risk_reason: %s\n", m.RiskReason)
	}
	if m.RootCause != "" {
		fmt.Fprintf(b, "- root_cause: %s\n", m.RootCause)
	}
	if len(m.Recommendations) > 0 {
		fmt.Fprintf(b, "- recommendations: %s\n", strings.Join(m.Recommendations, "; "))
	}
	fmt.Fprintf(b, "- owner/service/namespace: %s / %s / %s\n", orDash(m.Owner), orDash(m.Service), orDash(m.Namespace))
	fmt.Fprintf(b, "- series_count: %d\n", m.SeriesCount)
	if len(m.AnalysisSources) > 0 {
		fmt.Fprintf(b, "- analysis_sources: %s\n", strings.Join(m.AnalysisSources, ", "))
	}
	fmt.Fprintf(b, "- relabel_candidate: %t\n\n", m.RelabelCandidate)
}

// WriteMarkdown renders and writes the Markdown report to path.
func WriteMarkdown(r Report, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(RenderMarkdown(r)), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func sortedIntKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
