package cli

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/report"
	"prom-ai-guard/internal/scan"
)

func relabelSampleReport() report.Report {
	return report.Report{
		SchemaVersion: "v1",
		ScanID:        "scan-x",
		Summary:       scan.Summary{},
		InvalidMetrics: []model.MetricAnalysis{
			{MetricName: "http_user_requests_total", RuleSignals: []string{"label:user_id:high_cardinality"},
				RiskLevel: "severe", SeriesCount: 3, RelabelCandidate: true},
			{MetricName: "order_legacy_latency_seconds", RuleSignals: []string{"metric:deprecated"},
				RiskLevel: "warning", SeriesCount: 1, RelabelCandidate: true},
			{MetricName: "ghost_exporter_up", RuleSignals: []string{"service:orphan"},
				RiskLevel: "minor", SeriesCount: 1, RelabelCandidate: false},
		},
		TopRiskMetrics:     []model.RiskRef{},
		TopViolationLabels: []model.LabelViolation{},
		Warnings:           []model.ParseWarning{},
	}
}

func writeReportJSON(t *testing.T, rep report.Report) string {
	t.Helper()
	return writeReportFile(t, rep) // reuse helper from gate_test.go
}

func sha(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return string(sum[:])
}

func TestRelabelWritesYAMLAndDoesNotMutateReport(t *testing.T) {
	rp := writeReportJSON(t, relabelSampleReport())
	before := sha(t, rp)

	outDir := t.TempDir()
	out := filepath.Join(outDir, "relabel_rules.yaml")
	var buf bytes.Buffer
	code := run([]string{"relabel", "--report", rp, "--out", out}, &buf, &buf)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; output=%s", code, buf.String())
	}

	// report.json must be byte-identical (no mutation).
	if after := sha(t, rp); after != before {
		t.Errorf("relabel mutated analysis_report.json")
	}

	// relabel_rules.yaml must exist and parse.
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("missing relabel_rules.yaml: %v", err)
	}
	var plan map[string]any
	if err := yaml.Unmarshal(data, &plan); err != nil {
		t.Fatalf("relabel_rules.yaml is not valid YAML: %v", err)
	}

	// Every rule has review_required: true.
	rulesAny, _ := plan["rules"].([]any)
	if len(rulesAny) == 0 {
		t.Fatal("no rules in relabel_rules.yaml")
	}
	for _, r := range rulesAny {
		m, _ := r.(map[string]any)
		if rr, _ := m["review_required"].(bool); !rr {
			t.Errorf("rule %v missing review_required=true", m["rule_id"])
		}
	}
}

func TestRelabelMissingReportExit1(t *testing.T) {
	out := filepath.Join(t.TempDir(), "relabel_rules.yaml")
	var buf bytes.Buffer
	code := run([]string{"relabel", "--report", filepath.Join(t.TempDir(), "nope.json"), "--out", out}, &buf, &buf)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("must not write output on a read error")
	}
}
