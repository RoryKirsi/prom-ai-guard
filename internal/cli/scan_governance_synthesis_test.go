package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var mcMetricName = regexp.MustCompile(`"metric_name":"([^"]+)"`)

// mockLLMWithSynthesis routes the two prompt types: metric-analysis batches (echo
// is_invalid:false so batches succeed) vs the governance synthesis call (identified
// by its system prompt). synthStatus<200 makes the synthesis call fail.
func mockLLMWithSynthesis(t *testing.T, synthStatus int, synthNarrative string) *httptest.Server {
	t.Helper()
	writeChat := func(w http.ResponseWriter, content string) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		})
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Role, Content string
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		var system, user string
		for _, m := range req.Messages {
			switch m.Role {
			case "system":
				system = m.Content
			case "user":
				user = m.Content
			}
		}
		if strings.Contains(system, "AGGREGATED") { // governance synthesis
			if synthStatus != 0 && synthStatus < 200 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			writeChat(w, synthNarrative)
			return
		}
		// metric-analysis batch: echo is_invalid:false for each metric in the batch.
		items := []string{}
		for _, m := range mcMetricName.FindAllStringSubmatch(user, -1) {
			items = append(items, `{"metric_name":"`+m[1]+`","is_invalid":false}`)
		}
		writeChat(w, `{"metrics":[`+strings.Join(items, ",")+`],"summary":"batch summary"}`)
	}))
}

func TestScanGovernanceSummaryPresentLLM(t *testing.T) {
	t.Setenv("LLM_API_KEY", "k")
	const narrative = "Overall governance is weak (grade D); high cardinality is the dominant systemic issue across the whole batch."
	srv := mockLLMWithSynthesis(t, 200, narrative)
	defer srv.Close()
	cfg := setupConfigDir(t)
	outDir := t.TempDir()
	if _, err := runScanCmd(t, "--input", "../../fixtures/demo_metrics.prom", "--config", cfg, "--out", outDir,
		"--ai-mode", "llm_fullscan", "--ai-scope", "all", "--base-url", srv.URL, "--model", "x", "--ai-batch-size", "5"); err != nil {
		t.Fatal(err)
	}
	ai := readAIBlock(t, outDir)
	if ai.Status != "success" {
		t.Fatalf("ai.status = %q, want success", ai.Status)
	}
	if ai.GovernanceSummary != narrative {
		t.Errorf("ai.governance_summary = %q, want the synthesized narrative", ai.GovernanceSummary)
	}
}

func TestScanGovernanceSummaryAbsentLocalRules(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	cfg := setupConfigDir(t)
	outDir := t.TempDir()
	if _, err := runScanCmd(t, "--input", "../../fixtures/demo_metrics.prom", "--config", cfg, "--out", outDir, "--ai-mode", "local_rules"); err != nil {
		t.Fatal(err)
	}
	if ai := readAIBlock(t, outDir); ai.GovernanceSummary != "" {
		t.Errorf("local_rules must not produce a governance_summary, got %q", ai.GovernanceSummary)
	}
}

func TestScanGovernanceSynthesisFailureIsSafe(t *testing.T) {
	t.Setenv("LLM_API_KEY", "k")
	// Synthesis call fails (500); metric batches still succeed.
	srv := mockLLMWithSynthesis(t, 1, "")
	defer srv.Close()
	cfg := setupConfigDir(t)
	outDir := t.TempDir()
	if _, err := runScanCmd(t, "--input", "../../fixtures/demo_metrics.prom", "--config", cfg, "--out", outDir,
		"--ai-mode", "llm_fullscan", "--ai-scope", "all", "--base-url", srv.URL, "--model", "x", "--ai-batch-size", "5"); err != nil {
		t.Fatal(err)
	}
	ai := readAIBlock(t, outDir)
	// Synthesis failure must NOT change metric-analysis status/fallback/analyzed.
	if ai.Status != "success" {
		t.Errorf("ai.status = %q, want success (synthesis failure must not change it)", ai.Status)
	}
	if ai.FallbackUsed {
		t.Errorf("fallback_used must stay false on synthesis-only failure")
	}
	if ai.GovernanceSummary != "" {
		t.Errorf("governance_summary must be empty on synthesis failure, got %q", ai.GovernanceSummary)
	}
}

// The synthesis input is aggregate-only: a sensitive label value in the source must
// never reach the report's governance_summary (or anywhere in the report).
func TestScanGovernanceSummaryNoRawValueLeak(t *testing.T) {
	t.Setenv("LLM_API_KEY", "k")
	srv := mockLLMWithSynthesis(t, 200, "Overall: grade D.")
	defer srv.Close()
	cfg := setupConfigDir(t)
	mp := filepath.Join(t.TempDir(), "sensitive.prom")
	const secretVal = "SECRETVAL_alice@example.com"
	_ = os.WriteFile(mp, []byte("sensitive_metric{user_id=\""+secretVal+"\"} 1\n"), 0o644)
	outDir := t.TempDir()
	if _, err := runScanCmd(t, "--input", mp, "--config", cfg, "--out", outDir,
		"--ai-mode", "llm_fullscan", "--ai-scope", "all", "--base-url", srv.URL, "--model", "x", "--ai-batch-size", "5"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "analysis_report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secretVal) {
		t.Errorf("raw label value leaked into analysis_report.json")
	}
}
