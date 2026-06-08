package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// Slice 14: --ai-batch-size overrides ai.yaml/default; the report's ai block
// reflects the batch size and resulting batch count end-to-end.
func TestScanAIBatchSizeFlagOverride(t *testing.T) {
	t.Setenv("LLM_API_KEY", "x-test")
	// Mock LLM: a parseable (empty-metrics) completion for every batch request.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"metrics":[],"summary":"ok"}`}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := setupConfigDir(t)
	outDir := t.TempDir()
	// demo_metrics.prom has 11 metric names; --ai-scope all -> 11 in-scope profiles.
	if _, err := runScanCmd(t, "--input", "../../fixtures/demo_metrics.prom", "--config", cfg, "--out", outDir,
		"--ai-mode", "llm_fullscan", "--ai-scope", "all", "--base-url", srv.URL, "--model", "x",
		"--ai-batch-size", "2"); err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "analysis_report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		AI struct {
			BatchSize             int `json:"batch_size"`
			BatchCount            int `json:"batch_count"`
			SuccessfulBatches     int `json:"successful_batches"`
			LLMInScopeMetricCount int `json:"llm_in_scope_metric_count"`
		} `json:"ai"`
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.AI.BatchSize != 2 {
		t.Errorf("ai.batch_size = %d, want 2 (--ai-batch-size override)", rep.AI.BatchSize)
	}
	if rep.AI.LLMInScopeMetricCount != 11 {
		t.Errorf("ai.llm_in_scope_metric_count = %d, want 11", rep.AI.LLMInScopeMetricCount)
	}
	if rep.AI.BatchCount != 6 { // ceil(11/2)
		t.Errorf("ai.batch_count = %d, want 6 (11 metrics / batch_size 2)", rep.AI.BatchCount)
	}
	if rep.AI.SuccessfulBatches != 6 {
		t.Errorf("ai.successful_batches = %d, want 6", rep.AI.SuccessfulBatches)
	}
}
