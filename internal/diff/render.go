package diff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const overlapNote = "> Note: **Risk increased**, **Risk decreased**, and **Invalid type changes** are subsets of **Still invalid metrics** and may overlap (a metric can appear in several)."

// RenderMarkdown renders the diff as a deterministic human report.
func RenderMarkdown(d DiffResult) string {
	var b strings.Builder
	b.WriteString("# prom-ai-guard diff report\n\n")

	b.WriteString("## Reports compared\n\n")
	fmt.Fprintf(&b, "- previous: scan_id=%s scan_time=%s tool_version=%s source=%s\n",
		orDash(d.Previous.ScanID), orDash(d.Previous.ScanTime), orDash(d.Previous.ToolVersion), orDash(d.Previous.SourceType))
	fmt.Fprintf(&b, "- current:  scan_id=%s scan_time=%s tool_version=%s source=%s\n",
		orDash(d.Current.ScanID), orDash(d.Current.ScanTime), orDash(d.Current.ToolVersion), orDash(d.Current.SourceType))
	if d.ConfigChanged {
		fmt.Fprintf(&b, "\n⚠ **config_hash changed** between reports (%s → %s): differences may reflect rule/config changes, not metric drift.\n",
			orDash(d.Previous.ConfigHash), orDash(d.Current.ConfigHash))
	}
	b.WriteString("\n" + overlapNote + "\n\n")

	b.WriteString("## Summary delta\n\n")
	b.WriteString("| metric | previous | current | change |\n|---|---|---|---|\n")
	writeIntRow(&b, "invalid_metric_names", d.SummaryDelta.InvalidMetricNames)
	writeIntRow(&b, "total_metric_names", d.SummaryDelta.TotalMetricNames)
	writeIntRow(&b, "severe", d.SummaryDelta.Severe)
	writeIntRow(&b, "warning", d.SummaryDelta.Warning)
	writeIntRow(&b, "minor", d.SummaryDelta.Minor)
	r := d.SummaryDelta.InvalidRatio
	fmt.Fprintf(&b, "| invalid_ratio | %.4f | %.4f | %+.4f |\n\n", r.Previous, r.Current, r.Change)

	writeMetricTable(&b, "Added invalid metrics",
		[]string{"metric_name", "risk_level", "risk_score", "invalid_types"}, d.AddedInvalid,
		func(m MetricDiff) []string {
			return []string{m.MetricName, orDash(m.CurrentRiskLevel), fmt.Sprintf("%d", m.CurrentRiskScore), joinTypes(m.CurrentInvalidTypes)}
		})

	writeMetricTable(&b, "Resolved invalid metrics",
		[]string{"metric_name", "previous risk_level", "previous risk_score", "previous invalid_types"}, d.ResolvedInvalid,
		func(m MetricDiff) []string {
			return []string{m.MetricName, orDash(m.PreviousRiskLevel), fmt.Sprintf("%d", m.PreviousRiskScore), joinTypes(m.PreviousInvalidTypes)}
		})

	writeMetricTable(&b, "Still invalid metrics",
		[]string{"metric_name", "risk_level (prev→curr)", "risk_score (prev→curr)"}, d.StillInvalid,
		func(m MetricDiff) []string {
			return []string{m.MetricName,
				fmt.Sprintf("%s→%s", orDash(m.PreviousRiskLevel), orDash(m.CurrentRiskLevel)),
				fmt.Sprintf("%d→%d", m.PreviousRiskScore, m.CurrentRiskScore)}
		})

	writeMetricTable(&b, "Risk increased",
		[]string{"metric_name", "risk_score (prev→curr)", "risk_level (prev→curr)"}, d.RiskIncreased,
		func(m MetricDiff) []string {
			return []string{m.MetricName,
				fmt.Sprintf("%d→%d", m.PreviousRiskScore, m.CurrentRiskScore),
				fmt.Sprintf("%s→%s", orDash(m.PreviousRiskLevel), orDash(m.CurrentRiskLevel))}
		})

	writeMetricTable(&b, "Risk decreased",
		[]string{"metric_name", "risk_score (prev→curr)", "risk_level (prev→curr)"}, d.RiskDecreased,
		func(m MetricDiff) []string {
			return []string{m.MetricName,
				fmt.Sprintf("%d→%d", m.PreviousRiskScore, m.CurrentRiskScore),
				fmt.Sprintf("%s→%s", orDash(m.PreviousRiskLevel), orDash(m.CurrentRiskLevel))}
		})

	b.WriteString("## Invalid type changes\n\n")
	if len(d.TypeChanges) == 0 {
		b.WriteString("none\n")
	} else {
		b.WriteString("| metric_name | added_types | removed_types |\n|---|---|---|\n")
		for _, tc := range d.TypeChanges {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", tc.MetricName, joinTypes(tc.AddedTypes), joinTypes(tc.RemovedTypes))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func writeIntRow(b *strings.Builder, label string, d Delta) {
	fmt.Fprintf(b, "| %s | %d | %d | %+d |\n", label, d.Previous, d.Current, d.Change)
}

func writeMetricTable(b *strings.Builder, title string, cols []string, rows []MetricDiff, cells func(MetricDiff) []string) {
	fmt.Fprintf(b, "## %s\n\n", title)
	if len(rows) == 0 {
		b.WriteString("none\n\n")
		return
	}
	b.WriteString("| " + strings.Join(cols, " | ") + " |\n|" + strings.Repeat("---|", len(cols)) + "\n")
	for _, r := range rows {
		b.WriteString("| " + strings.Join(cells(r), " | ") + " |\n")
	}
	b.WriteString("\n")
}

func joinTypes(types []string) string {
	if len(types) == 0 {
		return "-"
	}
	return strings.Join(types, ", ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// WriteMarkdown writes the rendered diff report to path.
func WriteMarkdown(d DiffResult, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(RenderMarkdown(d)), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// WriteJSON writes the DiffResult as indented JSON to path.
func WriteJSON(d DiffResult, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(d); err != nil {
		return fmt.Errorf("encoding diff result: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
