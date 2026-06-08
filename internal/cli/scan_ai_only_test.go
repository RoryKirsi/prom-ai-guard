package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"prom-ai-guard/internal/rules"
)

// Slice 5.5: an AI-only identification demo. A metric that local deterministic
// rules accept (http_request_duration_seconds) is flagged invalid by the LLM for
// a semantic reason (name=seconds but unit=bytes). The merged report must include
// it with analysis_sources=["llm"], while the locally-severe metric is unchanged.

const aiOnlyFixture = "../../fixtures/ai_only_semantic_metrics.prom"

type invalidMetricRow struct {
	MetricName      string   `json:"metric_name"`
	InvalidTypes    []string `json:"invalid_types"`
	RiskLevel       string   `json:"risk_level"`
	AnalysisSources []string `json:"analysis_sources"`
}

func readInvalidMetrics(t *testing.T, outDir string) []invalidMetricRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, "analysis_report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		InvalidMetrics []invalidMetricRow `json:"invalid_metrics"`
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	return rep.InvalidMetrics
}

func findInvalidRow(rows []invalidMetricRow, name string) (invalidMetricRow, bool) {
	for _, r := range rows {
		if r.MetricName == name {
			return r, true
		}
	}
	return invalidMetricRow{}, false
}

// mockSemanticLLM returns an OpenAI-compatible completion whose content marks the
// semantically-inconsistent metric invalid (meaningless_metric) and confirms the
// severe metric is_invalid=false (which must be ignored, not a downgrade).
func mockSemanticLLM(t *testing.T) *httptest.Server {
	t.Helper()
	content := `{"metrics":[` +
		`{"metric_name":"http_request_duration_seconds","is_invalid":true,"invalid_types":["meaningless_metric"],` +
		`"risk_level":"minor","risk_reason":"name indicates duration in seconds but label unit=bytes contradicts the metric semantics",` +
		`"root_cause":"unit label inconsistent with metric name","recommendations":["remove or correct the unit label"],"confidence":0.8},` +
		`{"metric_name":"http_user_requests_total","is_invalid":false}` +
		`],"summary":"semantic consistency check"}`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestScanAIOnlySemanticFinding(t *testing.T) {
	t.Setenv("LLM_API_KEY", "x-test-key") // fake; httptest never validates it
	cfg := setupConfigDir(t)

	// Pass 1: local_rules proves the metric is locally VALID (absent from invalids).
	localOut := t.TempDir()
	if _, err := runScanCmd(t, "--source", "file", "--input", aiOnlyFixture,
		"--config", cfg, "--out", localOut, "--ai-mode", "local_rules"); err != nil {
		t.Fatalf("local_rules scan failed: %v", err)
	}
	localInvalids := readInvalidMetrics(t, localOut)
	if _, present := findInvalidRow(localInvalids, "http_request_duration_seconds"); present {
		t.Fatalf("http_request_duration_seconds must be locally VALID under local_rules; invalids=%v", localInvalids)
	}
	if sev, ok := findInvalidRow(localInvalids, "http_user_requests_total"); !ok || sev.RiskLevel != "severe" {
		t.Fatalf("expected http_user_requests_total severe under local_rules, got ok=%v %+v", ok, sev)
	}

	// Pass 2: llm_fullscan adds the AI-only finding.
	srv := mockSemanticLLM(t)
	defer srv.Close()
	llmOut := t.TempDir()
	if _, err := runScanCmd(t, "--source", "file", "--input", aiOnlyFixture,
		"--config", cfg, "--out", llmOut, "--ai-mode", "llm_fullscan", "--ai-scope", "all",
		"--base-url", srv.URL, "--model", "x"); err != nil {
		t.Fatalf("llm_fullscan scan failed: %v", err)
	}
	llmInvalids := readInvalidMetrics(t, llmOut)

	// The AI-only metric now appears with exactly ["llm"].
	row, ok := findInvalidRow(llmInvalids, "http_request_duration_seconds")
	if !ok {
		t.Fatalf("llm_fullscan must add http_request_duration_seconds; invalids=%v", llmInvalids)
	}
	if !reflect.DeepEqual(row.InvalidTypes, []string{"meaningless_metric"}) {
		t.Errorf("invalid_types = %v, want [meaningless_metric]", row.InvalidTypes)
	}
	if !reflect.DeepEqual(row.AnalysisSources, []string{"llm"}) {
		t.Errorf("analysis_sources = %v, want exactly [llm]", row.AnalysisSources)
	}
	wantLevel := rules.RiskLevelFor(rules.RiskScore([]string{rules.TypeMeaningless}))
	if row.RiskLevel != wantLevel {
		t.Errorf("risk_level = %q, want deterministic %q", row.RiskLevel, wantLevel)
	}

	// The locally-severe finding must NOT be downgraded and must keep local_rules.
	sev, ok := findInvalidRow(llmInvalids, "http_user_requests_total")
	if !ok || sev.RiskLevel != "severe" {
		t.Fatalf("severe local finding downgraded/removed: ok=%v %+v", ok, sev)
	}
	if !containsStr(sev.AnalysisSources, "local_rules") {
		t.Errorf("severe finding sources = %v, must include local_rules", sev.AnalysisSources)
	}

	// AI ran successfully (no fallback, no live call).
	if ai := readAIBlock(t, llmOut); ai.Status != "success" {
		t.Errorf("ai.status = %q, want success", ai.Status)
	}
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
