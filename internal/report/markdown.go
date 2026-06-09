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

	// Storage impact (heuristic)
	renderStorageImpact(&b, r)

	// Governance assessment (deterministic, heuristic)
	renderGovernance(&b, r)

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

func renderGovernance(b *strings.Builder, r Report) {
	b.WriteString("## Governance assessment\n\n")
	ga := r.Summary.GovernanceAssessment
	if ga == nil {
		b.WriteString("_none_\n\n")
		return
	}
	fmt.Fprintf(b, "- maturity: grade %s (score %d) — heuristic\n", ga.MaturityGrade, ga.MaturityScore)
	fmt.Fprintf(b, "- invalid_ratio: %.4f (total_invalid %d)\n", ga.InvalidRatio, ga.TotalInvalid)
	fmt.Fprintf(b, "- risk_distribution: severe=%d warning=%d minor=%d\n",
		ga.RiskDistribution["severe"], ga.RiskDistribution["warning"], ga.RiskDistribution["minor"])
	fmt.Fprintf(b, "\n> Note: %s\n\n", ga.MaturityHeuristic)

	if len(ga.TopSystemicIssues) > 0 {
		b.WriteString("### Top systemic issues\n\n")
		b.WriteString("| invalid_type | metric_count | max_risk | max_score |\n")
		b.WriteString("|---|---|---|---|\n")
		for _, s := range ga.TopSystemicIssues {
			fmt.Fprintf(b, "| %s | %d | %s | %d |\n", s.InvalidType, s.MetricCount, s.MaxRisk, s.MaxScore)
		}
		b.WriteString("\n")
	}
	if len(ga.PrioritizedActions) > 0 {
		b.WriteString("### Prioritized actions\n\n")
		for _, a := range ga.PrioritizedActions {
			fmt.Fprintf(b, "1. %s\n", a)
		}
		b.WriteString("\n")
	}
	if len(ga.RecommendedNorms) > 0 {
		b.WriteString("### Recommended governance norms\n\n")
		for _, n := range ga.RecommendedNorms {
			fmt.Fprintf(b, "- %s\n", n)
		}
		b.WriteString("\n")
	}
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

// renderStorageImpact renders the deterministic TSDB-index storage-impact
// simulation. estimated_index_entries is heuristic, not real TSDB bytes.
func renderStorageImpact(b *strings.Builder, r Report) {
	b.WriteString("## Storage impact (heuristic)\n\n")
	si := r.Summary.StorageImpact
	if si == nil {
		b.WriteString("_none_\n\n")
		return
	}
	fmt.Fprintf(b, "- impact metrics: high=%d medium=%d low=%d\n", si.HighImpactMetrics, si.MediumImpactMetrics, si.LowImpactMetrics)
	fmt.Fprintf(b, "- estimated_invalid_series: %d\n", si.EstimatedInvalidSeries)
	fmt.Fprintf(b, "- estimated_invalid_index_entries: %d\n", si.EstimatedInvalidIndexEntries)
	fmt.Fprintf(b, "\n> Note: %s\n", si.Heuristic)
	fmt.Fprintf(b, "> Scope: %s\n\n", si.ScopeNote)

	if len(r.InvalidMetrics) == 0 {
		b.WriteString("_no invalid metrics_\n\n")
		return
	}
	b.WriteString("| metric_name | series_count | label_count | max_label_cardinality | estimated_index_entries | impact_level |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, m := range r.InvalidMetrics {
		s := m.StorageImpact
		if s == nil {
			continue
		}
		fmt.Fprintf(b, "| %s | %d | %d | %d | %d | %s |\n",
			m.MetricName, s.SeriesCount, s.LabelCount, s.MaxLabelCardinality, s.EstimatedIndexEntries, s.ImpactLevel)
	}
	b.WriteString("\n")
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
