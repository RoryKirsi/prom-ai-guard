package cli

import (
	"strings"
	"testing"
)

// The scan console must surface the per-metric invalid risk list (无效指标风险列表)
// and announce the analysis log (scan.log.jsonl) so it is discoverable.
func TestScanConsoleRiskListAndAnalysisLog(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	cfg := setupConfigDir(t)
	outDir := t.TempDir()
	out, err := runScanCmd(t, "--input", "../../fixtures/demo_metrics.prom", "--config", cfg, "--out", outDir, "--ai-mode", "local_rules")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "invalid_metrics (risk list") {
		t.Errorf("console should print the invalid-metric risk list:\n%s", out)
	}
	if !strings.Contains(out, "http_user_requests_total") {
		t.Errorf("risk list should list invalid metrics by name:\n%s", out)
	}
	if !strings.Contains(out, "analysis_log:") || !strings.Contains(out, "scan.log.jsonl") {
		t.Errorf("console should announce the analysis log (scan.log.jsonl) path:\n%s", out)
	}
}
