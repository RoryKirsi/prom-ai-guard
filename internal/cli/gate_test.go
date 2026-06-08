package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"prom-ai-guard/internal/ai"
	"prom-ai-guard/internal/gate"
	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/report"
	"prom-ai-guard/internal/scan"
)

func baseGateReport() report.Report {
	return report.Report{
		SchemaVersion: "v1",
		AI:            &ai.Info{FallbackUsed: false},
		Summary: scan.Summary{
			InvalidRatio:      0.1,
			RiskDistribution:  map[string]int{"severe": 0, "warning": 0, "minor": 0},
			InvalidTypeCounts: map[string]int{"high_cardinality": 0},
		},
		InvalidMetrics:     []model.MetricAnalysis{},
		TopRiskMetrics:     []model.RiskRef{},
		TopViolationLabels: []model.LabelViolation{},
		Warnings:           []model.ParseWarning{},
	}
}

func writeReportFile(t *testing.T, rep report.Report) string {
	t.Helper()
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "analysis_report.json")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeText(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const lenientPolicy = `schema_version: v1
gate:
  fail_on_schema_error: true
  max_severe: 100
  max_warning: 100
  max_invalid_ratio: 1.0
  max_high_cardinality_metrics: 100
`

const strictPolicy = `schema_version: v1
gate:
  fail_on_schema_error: true
  fail_on_fallback_used: true
  max_severe: 0
  max_warning: 0
  max_invalid_ratio: 0.3
  max_high_cardinality_metrics: 0
  forbidden_label_keys:
    - user_id
`

func runGateCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(append([]string{"gate"}, args...), &out, &errb)
	return code, out.String(), errb.String()
}

func TestGatePass(t *testing.T) {
	rp := writeReportFile(t, baseGateReport())
	pp := writeText(t, "policy.yaml", lenientPolicy)
	code, out, _ := runGateCLI(t, "--report", rp, "--policy", pp)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; out=%s", code, out)
	}
	if !strings.Contains(out, "PASS") {
		t.Errorf("text output should say PASS: %s", out)
	}
}

func TestGateSevereFail(t *testing.T) {
	rep := baseGateReport()
	rep.Summary.RiskDistribution["severe"] = 1
	rp := writeReportFile(t, rep)
	pp := writeText(t, "policy.yaml", strictPolicy)
	code, _, _ := runGateCLI(t, "--report", rp, "--policy", pp)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestGateFallbackFail(t *testing.T) {
	rep := baseGateReport()
	rep.AI.FallbackUsed = true
	rp := writeReportFile(t, rep)
	pp := writeText(t, "policy.yaml", strictPolicy)
	code, _, _ := runGateCLI(t, "--report", rp, "--policy", pp)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestGateInvalidRatioFail(t *testing.T) {
	rep := baseGateReport()
	rep.Summary.InvalidRatio = 0.9
	rp := writeReportFile(t, rep)
	pp := writeText(t, "policy.yaml", strictPolicy)
	code, _, _ := runGateCLI(t, "--report", rp, "--policy", pp)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestGateHighCardinalityFail(t *testing.T) {
	rep := baseGateReport()
	rep.Summary.InvalidTypeCounts["high_cardinality"] = 3
	rp := writeReportFile(t, rep)
	pp := writeText(t, "policy.yaml", strictPolicy)
	code, _, _ := runGateCLI(t, "--report", rp, "--policy", pp)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestGateForbiddenLabelFail(t *testing.T) {
	rep := baseGateReport()
	rep.TopViolationLabels = []model.LabelViolation{{LabelKey: "user_id", InvalidType: "high_cardinality"}}
	rp := writeReportFile(t, rep)
	pp := writeText(t, "policy.yaml", strictPolicy)
	code, _, _ := runGateCLI(t, "--report", rp, "--policy", pp)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestGateMissingReport(t *testing.T) {
	pp := writeText(t, "policy.yaml", lenientPolicy)
	code, _, errOut := runGateCLI(t, "--report", filepath.Join(t.TempDir(), "nope.json"), "--policy", pp)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "reading report") {
		t.Errorf("error should go to stderr: %q", errOut)
	}
}

func TestGateMissingPolicy(t *testing.T) {
	rp := writeReportFile(t, baseGateReport())
	code, _, _ := runGateCLI(t, "--report", rp, "--policy", filepath.Join(t.TempDir(), "nope.yaml"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestGateMalformedJSON(t *testing.T) {
	rp := writeText(t, "analysis_report.json", "{ this is not json")
	pp := writeText(t, "policy.yaml", lenientPolicy)
	code, _, _ := runGateCLI(t, "--report", rp, "--policy", pp)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestGateMalformedYAML(t *testing.T) {
	rp := writeReportFile(t, baseGateReport())
	pp := writeText(t, "policy.yaml", "bad: yaml: here")
	code, _, _ := runGateCLI(t, "--report", rp, "--policy", pp)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestGateSchemaErrorExit1(t *testing.T) {
	// Valid JSON but missing summary -> schema error; fail_on_schema_error=true -> exit 1.
	rp := writeText(t, "analysis_report.json", `{"schema_version":"v1","invalid_metrics":[],"top_violation_labels":[]}`)
	pp := writeText(t, "policy.yaml", lenientPolicy)
	code, _, errOut := runGateCLI(t, "--report", rp, "--policy", pp)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr=%s", code, errOut)
	}
}

func TestGateJSONStdoutOnly(t *testing.T) {
	rep := baseGateReport()
	rep.Summary.RiskDistribution["severe"] = 1 // force a policy failure
	rp := writeReportFile(t, rep)
	pp := writeText(t, "policy.yaml", strictPolicy)

	code, out, _ := runGateCLI(t, "--report", rp, "--policy", pp, "--json")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	// stdout must be ONLY the GateResult JSON — no human text.
	if strings.Contains(out, "prom-ai-guard gate") || strings.Contains(out, "result:") {
		t.Errorf("--json stdout contains human text: %q", out)
	}
	var gr gate.GateResult
	if err := json.Unmarshal([]byte(out), &gr); err != nil {
		t.Fatalf("--json stdout is not a single GateResult JSON: %v\n%s", err, out)
	}
	if gr.Passed || gr.ExitCode != 2 || len(gr.PolicyHits) == 0 {
		t.Errorf("unexpected GateResult: %+v", gr)
	}
}

func TestGateJSONStdoutOnlyOnPass(t *testing.T) {
	rp := writeReportFile(t, baseGateReport())
	pp := writeText(t, "policy.yaml", lenientPolicy)
	code, out, _ := runGateCLI(t, "--report", rp, "--policy", pp, "--json")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	var gr gate.GateResult
	if err := json.Unmarshal([]byte(out), &gr); err != nil {
		t.Fatalf("--json pass stdout not clean JSON: %v\n%s", err, out)
	}
	if !gr.Passed {
		t.Errorf("expected passed=true, got %+v", gr)
	}
}
