package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"prom-ai-guard/internal/doctor"
	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/report"
	"prom-ai-guard/internal/scan"
)

func doctorReport() report.Report {
	return report.Report{
		SchemaVersion: "v1",
		ScanID:        "sid-doc",
		Source:        report.Source{SourceType: "file"},
		Summary:       scan.Summary{RiskDistribution: map[string]int{}, InvalidTypeCounts: map[string]int{}},
		InvalidMetrics: []model.MetricAnalysis{
			{
				MetricName: "http_user_requests_total", RiskLevel: "severe", RiskScore: 90,
				InvalidTypes: []string{"high_cardinality"}, RuleSignals: []string{"label:user_id:high_cardinality"},
				Owner: "platform", Service: "payment-api", Namespace: "payments",
				LabelCardinality: map[string]int{"user_id": 3, "service": 1}, RelabelCandidate: true,
			},
		},
		TopRiskMetrics:     []model.RiskRef{},
		TopViolationLabels: []model.LabelViolation{},
		Warnings:           []model.ParseWarning{},
	}
}

func runDoctorCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestDoctorNoSelectorExit1(t *testing.T) {
	rp := writeReportFile(t, doctorReport())
	code, _, errOut := runDoctorCLI(t, "doctor", "--report", rp)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "at least one selector") {
		t.Errorf("expected helpful selector error, got %q", errOut)
	}
}

func TestDoctorMissingReportExit1(t *testing.T) {
	code, _, _ := runDoctorCLI(t, "doctor", "--metric", "x", "--report", filepath.Join(t.TempDir(), "nope.json"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestDoctorBadSchemaExit1(t *testing.T) {
	rp := writeText(t, "analysis_report.json", `{"schema_version":"v2","invalid_metrics":[]}`)
	code, _, _ := runDoctorCLI(t, "doctor", "--metric", "x", "--report", rp)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestDoctorMetricMatchExit0(t *testing.T) {
	rp := writeReportFile(t, doctorReport())
	code, out, _ := runDoctorCLI(t, "doctor", "--metric", "http_user_requests_total", "--report", rp)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "### http_user_requests_total") || !strings.Contains(out, "## Matches (1)") {
		t.Errorf("console missing diagnosis: %s", out)
	}
}

func TestDoctorConsoleOnlyByDefault(t *testing.T) {
	rp := writeReportFile(t, doctorReport())
	outDir := t.TempDir()
	// No --out/--json: nothing should be written.
	runDoctorCLI(t, "doctor", "--metric", "http_user_requests_total", "--report", rp)
	if entries, _ := os.ReadDir(outDir); len(entries) != 0 {
		t.Errorf("default run must not write files, found %d", len(entries))
	}

	// With --out/--json: both written.
	md := filepath.Join(outDir, "doctor_report.md")
	js := filepath.Join(outDir, "doctor_report.json")
	code, _, _ := runDoctorCLI(t, "doctor", "--metric", "http_user_requests_total", "--report", rp, "--out", md, "--json", js)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if _, err := os.Stat(md); err != nil {
		t.Errorf("missing markdown: %v", err)
	}
	data, err := os.ReadFile(js)
	if err != nil {
		t.Fatalf("missing json: %v", err)
	}
	var d doctor.DoctorResult
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("json not a DoctorResult: %v", err)
	}
	if d.MatchCount != 1 || d.Matches[0].MetricName != "http_user_requests_total" {
		t.Errorf("unexpected DoctorResult: %+v", d)
	}
}

func TestDoctorAbsentMetricExit0WithNote(t *testing.T) {
	rp := writeReportFile(t, doctorReport())
	code, out, _ := runDoctorCLI(t, "doctor", "--metric", "definitely_absent", "--report", rp)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (diagnosis ran)", code)
	}
	if !strings.Contains(out, "not found in invalid_metrics") || !strings.Contains(out, "cannot confirm healthy") {
		t.Errorf("absence must not be reported as healthy: %s", out)
	}
}

func TestDoctorInspectAlias(t *testing.T) {
	rp := writeReportFile(t, doctorReport())
	code, out, _ := runDoctorCLI(t, "inspect", "--metric", "http_user_requests_total", "--report", rp)
	if code != 0 {
		t.Fatalf("inspect alias exit = %d, want 0", code)
	}
	if !strings.Contains(out, "### http_user_requests_total") {
		t.Errorf("inspect alias produced no diagnosis: %s", out)
	}
}

func TestDoctorLabelSelector(t *testing.T) {
	rp := writeReportFile(t, doctorReport())
	code, out, _ := runDoctorCLI(t, "doctor", "--label", "user_id", "--report", rp)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "matched_labels: user_id") {
		t.Errorf("label selector should record matched_labels: %s", out)
	}
}
