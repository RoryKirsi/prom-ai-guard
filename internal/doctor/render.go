package doctor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Render produces the human diagnosis text (Markdown), used for both console
// output and the optional --out file.
func Render(d DoctorResult) string {
	var b strings.Builder
	b.WriteString("# prom-ai-guard doctor report\n\n")

	b.WriteString("## Query (AND of provided selectors)\n")
	fmt.Fprintf(&b, "- metric:  %s\n", orDash(d.Query.Metric))
	fmt.Fprintf(&b, "- label:   %s\n", orDash(d.Query.Label))
	fmt.Fprintf(&b, "- service: %s\n\n", orDash(d.Query.Service))

	b.WriteString("## Report\n")
	fmt.Fprintf(&b, "- scan_id:   %s\n", orDash(d.Report.ScanID))
	fmt.Fprintf(&b, "- scan_time: %s\n", orDash(d.Report.ScanTime))
	fmt.Fprintf(&b, "- source:    %s\n\n", orDash(d.Report.SourceType))

	fmt.Fprintf(&b, "## Matches (%d)\n\n", d.MatchCount)
	if d.MatchCount == 0 {
		b.WriteString("No invalid metrics matched the selectors.\n\n")
	}
	for _, m := range d.Matches {
		fmt.Fprintf(&b, "### %s\n", m.MetricName)
		fmt.Fprintf(&b, "- risk: %s (%d)\n", orDash(m.RiskLevel), m.RiskScore)
		fmt.Fprintf(&b, "- invalid_types: %s\n", joinOrDash(m.InvalidTypes))
		fmt.Fprintf(&b, "- rule_signals: %s\n", joinOrDash(m.RuleSignals))
		fmt.Fprintf(&b, "- risk_reason: %s\n", orDash(m.RiskReason))
		fmt.Fprintf(&b, "- root_cause: %s\n", orDash(m.RootCause))
		fmt.Fprintf(&b, "- recommendations: %s\n", joinOrDash(m.Recommendations))
		fmt.Fprintf(&b, "- owner / service / namespace: %s / %s / %s\n", orDash(m.Owner), orDash(m.Service), orDash(m.Namespace))
		fmt.Fprintf(&b, "- relabel_candidate: %t   relabel_proposal_possible: %t\n", m.RelabelCandidate, m.RelabelProposalPossible)
		if len(m.MatchedLabels) > 0 {
			fmt.Fprintf(&b, "- matched_labels: %s\n", joinOrDash(m.MatchedLabels))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Notes\n")
	if len(d.Notes) == 0 {
		b.WriteString("- none\n")
	}
	for _, n := range d.Notes {
		fmt.Fprintf(&b, "- %s\n", n)
	}
	return b.String()
}

func joinOrDash(ss []string) string {
	if len(ss) == 0 {
		return "-"
	}
	return strings.Join(ss, ", ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// WriteMarkdown writes the rendered diagnosis to path.
func WriteMarkdown(d DoctorResult, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(Render(d)), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// WriteJSON writes the DoctorResult as indented JSON to path.
func WriteJSON(d DoctorResult, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(d); err != nil {
		return fmt.Errorf("encoding doctor result: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
