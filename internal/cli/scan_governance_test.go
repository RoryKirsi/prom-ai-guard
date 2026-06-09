package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type govReport struct {
	AI struct {
		Status string `json:"status"`
	} `json:"ai"`
	Summary struct {
		Governance *struct {
			MaturityScore      int      `json:"maturity_score"`
			MaturityGrade      string   `json:"maturity_grade"`
			MaturityHeuristic  string   `json:"maturity_heuristic"`
			TotalInvalid       int      `json:"total_invalid"`
			TopSystemicIssues  []any    `json:"top_systemic_issues"`
			PrioritizedActions []string `json:"prioritized_actions"`
			RecommendedNorms   []string `json:"recommended_norms"`
		} `json:"governance_assessment"`
	} `json:"summary"`
}

func readGovReport(t *testing.T, outDir string) govReport {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, "analysis_report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r govReport
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestScanGovernanceAssessmentLocalRules(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	cfg := setupConfigDir(t)
	outDir := t.TempDir()
	if _, err := runScanCmd(t, "--input", "../../fixtures/demo_metrics.prom", "--config", cfg, "--out", outDir, "--ai-mode", "local_rules"); err != nil {
		t.Fatal(err)
	}
	r := readGovReport(t, outDir)
	if r.Summary.Governance == nil {
		t.Fatal("summary.governance_assessment must exist in local_rules runs")
	}
	g := r.Summary.Governance
	if g.MaturityGrade == "" || g.MaturityHeuristic == "" {
		t.Errorf("maturity grade/heuristic must be set: %+v", g)
	}
	if g.TotalInvalid == 0 || len(g.TopSystemicIssues) == 0 || len(g.PrioritizedActions) == 0 || len(g.RecommendedNorms) == 0 {
		t.Errorf("governance assessment should be populated for the demo fixture: %+v", g)
	}
}

func TestScanGovernanceAssessmentSurvivesFallback(t *testing.T) {
	t.Setenv("LLM_API_KEY", "k")
	// Mock LLM returns unparseable content -> every batch fails -> ai.status=fallback.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"not analyzer json"}}]}`)
	}))
	defer srv.Close()
	cfg := setupConfigDir(t)
	outDir := t.TempDir()
	if _, err := runScanCmd(t, "--input", "../../fixtures/demo_metrics.prom", "--config", cfg, "--out", outDir,
		"--ai-mode", "llm_fullscan", "--ai-scope", "all", "--base-url", srv.URL, "--model", "x", "--ai-batch-size", "5"); err != nil {
		t.Fatal(err)
	}
	r := readGovReport(t, outDir)
	if r.AI.Status != "fallback" {
		t.Fatalf("expected ai.status=fallback, got %q", r.AI.Status)
	}
	// Deterministic governance must still be present despite the LLM fallback.
	if r.Summary.Governance == nil || r.Summary.Governance.MaturityGrade == "" {
		t.Errorf("governance_assessment must survive LLM fallback (deterministic): %+v", r.Summary.Governance)
	}
}
