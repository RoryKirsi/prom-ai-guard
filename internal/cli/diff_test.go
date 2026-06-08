package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"prom-ai-guard/internal/diff"
	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/report"
	"prom-ai-guard/internal/scan"
)

func diffReport(configHash string, metrics ...model.MetricAnalysis) report.Report {
	return report.Report{
		SchemaVersion: "v1",
		ScanID:        "sid-" + configHash,
		ConfigHash:    configHash,
		Summary: scan.Summary{
			InvalidMetricNames: len(metrics),
			TotalMetricNames:   10,
			InvalidRatio:       0.2,
			RiskDistribution:   map[string]int{"severe": 1, "warning": 0, "minor": 0},
			InvalidTypeCounts:  map[string]int{},
		},
		InvalidMetrics:     metrics,
		TopRiskMetrics:     []model.RiskRef{},
		TopViolationLabels: []model.LabelViolation{},
		Warnings:           []model.ParseWarning{},
	}
}

func dm(name string, score int, types ...string) model.MetricAnalysis {
	if types == nil {
		types = []string{}
	}
	return model.MetricAnalysis{MetricName: name, RiskLevel: "severe", RiskScore: score, InvalidTypes: types}
}

func runDiffCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(append([]string{"diff"}, args...), &out, &errb)
	return code, out.String(), errb.String()
}

func TestDiffWritesMarkdownAndJSON(t *testing.T) {
	prev := writeReportFile(t, diffReport("h1", dm("a", 90, "high_cardinality"), dm("b", 50, "deprecated_metric")))
	curr := writeReportFile(t, diffReport("h2", dm("a", 90, "high_cardinality"), dm("c", 30, "meaningless_metric")))
	outDir := t.TempDir()
	md := filepath.Join(outDir, "diff_report.md")
	js := filepath.Join(outDir, "diff_report.json")

	code, out, _ := runDiffCLI(t, "--previous", prev, "--current", curr, "--out", md, "--json", js)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; out=%s", code, out)
	}
	if _, err := os.Stat(md); err != nil {
		t.Errorf("missing markdown: %v", err)
	}
	data, err := os.ReadFile(js)
	if err != nil {
		t.Fatalf("missing json: %v", err)
	}
	var d diff.DiffResult
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("json not a DiffResult: %v", err)
	}
	if len(d.AddedInvalid) != 1 || d.AddedInvalid[0].MetricName != "c" {
		t.Errorf("added = %+v, want [c]", d.AddedInvalid)
	}
	if len(d.ResolvedInvalid) != 1 || d.ResolvedInvalid[0].MetricName != "b" {
		t.Errorf("resolved = %+v, want [b]", d.ResolvedInvalid)
	}
	if !d.ConfigChanged {
		t.Errorf("config_changed must be true (h1 != h2)")
	}
}

func TestDiffMarkdownOnlyWhenNoJSON(t *testing.T) {
	prev := writeReportFile(t, diffReport("h", dm("a", 90, "high_cardinality")))
	curr := writeReportFile(t, diffReport("h", dm("a", 90, "high_cardinality")))
	outDir := t.TempDir()
	md := filepath.Join(outDir, "diff_report.md")

	code, _, _ := runDiffCLI(t, "--previous", prev, "--current", curr, "--out", md)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if _, err := os.Stat(md); err != nil {
		t.Errorf("markdown should be written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "diff_report.json")); err == nil {
		t.Errorf("json must NOT be written without --json")
	}
}

func TestDiffMissingReportExit1(t *testing.T) {
	curr := writeReportFile(t, diffReport("h", dm("a", 90, "high_cardinality")))
	outDir := t.TempDir()
	code, _, errOut := runDiffCLI(t, "--previous", filepath.Join(t.TempDir(), "nope.json"),
		"--current", curr, "--out", filepath.Join(outDir, "d.md"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if errOut == "" {
		t.Errorf("error should be reported on stderr")
	}
}

func TestDiffDuplicateMetricNameExit1(t *testing.T) {
	dupBody := `{"schema_version":"v1","summary":{"invalid_metric_names":2,"total_metric_names":5,"invalid_ratio":0.4,"risk_distribution":{"severe":2,"warning":0,"minor":0}},"invalid_metrics":[{"metric_name":"dup","risk_score":90,"invalid_types":["high_cardinality"]},{"metric_name":"dup","risk_score":50,"invalid_types":["deprecated_metric"]}]}`
	prev := writeText(t, "analysis_report.json", dupBody)
	curr := writeReportFile(t, diffReport("h", dm("a", 90, "high_cardinality")))
	outDir := t.TempDir()
	code, _, errOut := runDiffCLI(t, "--previous", prev, "--current", curr, "--out", filepath.Join(outDir, "d.md"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(outDir, "d.md")); err == nil {
		t.Errorf("no report should be written on a validation error")
	}
	_ = errOut
}

func TestDiffMissingSummaryFieldExit1(t *testing.T) {
	// missing summary.invalid_ratio -> strict validation must reject (exit 1).
	body := `{"schema_version":"v1","summary":{"invalid_metric_names":1,"total_metric_names":5,"risk_distribution":{"severe":1,"warning":0,"minor":0}},"invalid_metrics":[{"metric_name":"a","risk_score":90,"invalid_types":["high_cardinality"]}]}`
	prev := writeText(t, "analysis_report.json", body)
	curr := writeReportFile(t, diffReport("h", dm("a", 90, "high_cardinality")))
	outDir := t.TempDir()
	code, _, _ := runDiffCLI(t, "--previous", prev, "--current", curr, "--out", filepath.Join(outDir, "d.md"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (strict summary validation)", code)
	}
}
