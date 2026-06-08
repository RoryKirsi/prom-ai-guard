package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "rules.yaml"), `schema_version: v1
thresholds:
  high_cardinality_label_values: 100
  high_cardinality_metric_series: 1000
patterns:
  forbidden_label_keys:
    - user_id
`)
	writeFile(t, filepath.Join(dir, "service_inventory.yaml"), `schema_version: v1
services:
  - namespace: payments
    service: payment-api
    owner: team
`)
	writeFile(t, filepath.Join(dir, "ai.yaml"), `provider: deepseek
mode: llm_fullscan
model: deepseek-v4-flash
base_url: https://api.deepseek.com
api_key_env: LLM_API_KEY
max_attempts: 2
max_payload_bytes: 262144
timeout_seconds: 5
`)
	return dir
}

func setupMetrics(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "m.prom")
	writeFile(t, p, `http_user_requests_total{service="payment-api",user_id="u1"} 1
http_user_requests_total{service="payment-api",user_id="u2"} 1
`)
	return p
}

func runScanCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newScanCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

type aiBlock struct {
	Status         string `json:"status"`
	Enabled        bool   `json:"enabled"`
	FallbackReason string `json:"fallback_reason"`
	ConfigHash     string `json:"config_hash"`
}

func readAIBlock(t *testing.T, outDir string) aiBlock {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, "analysis_report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		AI aiBlock `json:"ai"`
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	return rep.AI
}

func TestScanLLMSecretNeverLeaks(t *testing.T) {
	const secret = "TESTSECRET123"
	t.Setenv("LLM_API_KEY", secret)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+secret {
			t.Errorf("server saw auth header %q", got)
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"{\"metrics\":[],\"summary\":\"ok\"}"}}]}`))
	}))
	defer srv.Close()

	cfg := setupConfigDir(t)
	metrics := setupMetrics(t)
	outDir := t.TempDir()

	out, err := runScanCmd(t, "--input", metrics, "--config", cfg, "--out", outDir,
		"--ai-mode", "llm_fullscan", "--base-url", srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	// The key must never appear in console output, report, or preview.
	report, _ := os.ReadFile(filepath.Join(outDir, "analysis_report.json"))
	preview, _ := os.ReadFile(filepath.Join(outDir, "ai_input_preview.json"))
	for name, blob := range map[string]string{"console": out, "report": string(report), "preview": string(preview)} {
		if strings.Contains(blob, secret) {
			t.Errorf("API key leaked into %s", name)
		}
	}
	// AI ran (server returned no per-metric entries -> partial, not fallback).
	ai := readAIBlock(t, outDir)
	if ai.Status != "partial" {
		t.Errorf("ai.status = %q, want partial", ai.Status)
	}
	if !strings.HasPrefix(ai.ConfigHash, "sha256:") {
		t.Errorf("ai.config_hash = %q", ai.ConfigHash)
	}
}

func TestScanLocalRulesDisabled(t *testing.T) {
	t.Setenv("LLM_API_KEY", "shouldnotbeused")
	cfg := setupConfigDir(t)
	metrics := setupMetrics(t)
	outDir := t.TempDir()

	if _, err := runScanCmd(t, "--input", metrics, "--config", cfg, "--out", outDir, "--ai-mode", "local_rules"); err != nil {
		t.Fatal(err)
	}
	ai := readAIBlock(t, outDir)
	if ai.Status != "disabled" || ai.Enabled {
		t.Errorf("ai = %+v, want disabled", ai)
	}
}

func TestScanWritesAllReportArtifacts(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	cfg := setupConfigDir(t)
	metrics := setupMetrics(t)
	outDir := t.TempDir()

	if _, err := runScanCmd(t, "--input", metrics, "--config", cfg, "--out", outDir, "--ai-mode", "local_rules"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"analysis_report.json", "analysis_report.md", "analysis_report.xlsx", "ai_input_preview.json"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Errorf("missing artifact %s: %v", name, err)
		}
	}
}

func TestScanJSONTopLevelKeysUnchanged(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	cfg := setupConfigDir(t)
	metrics := setupMetrics(t)
	outDir := t.TempDir()

	if _, err := runScanCmd(t, "--input", metrics, "--config", cfg, "--out", outDir, "--ai-mode", "local_rules"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "analysis_report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	// The Slice 4 contract: adding Markdown/Excel must not change the JSON schema.
	want := []string{
		"schema_version", "scan_id", "scan_time", "tool_version", "config_hash",
		"source", "ai", "summary", "invalid_metrics", "top_risk_metrics",
		"top_violation_labels", "warnings",
	}
	if len(top) != len(want) {
		t.Errorf("JSON has %d top-level keys, want %d: %v", len(top), len(want), keysOf(top))
	}
	for _, k := range want {
		if _, ok := top[k]; !ok {
			t.Errorf("JSON missing top-level key %q (schema drift)", k)
		}
	}
	if _, ok := top["report_generation_status"]; ok {
		t.Errorf("report_generation_status must not be added to JSON in Slice 5")
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestScanFallbackWhenKeyMissing(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	cfg := setupConfigDir(t)
	metrics := setupMetrics(t)
	outDir := t.TempDir()

	if _, err := runScanCmd(t, "--input", metrics, "--config", cfg, "--out", outDir, "--ai-mode", "llm_fullscan"); err != nil {
		t.Fatal(err)
	}
	ai := readAIBlock(t, outDir)
	if ai.Status != "fallback" || !strings.Contains(ai.FallbackReason, "LLM_API_KEY") {
		t.Errorf("ai = %+v, want fallback/missing-key", ai)
	}
}

func TestScanRejectsUnsupportedMode(t *testing.T) {
	cfg := setupConfigDir(t)
	metrics := setupMetrics(t)
	outDir := t.TempDir()
	// deepseek_fullscan is no longer accepted (alias support removed).
	for _, mode := range []string{"hybrid", "fallback_only", "deepseek_fullscan"} {
		if _, err := runScanCmd(t, "--input", metrics, "--config", cfg, "--out", outDir, "--ai-mode", mode); err == nil {
			t.Errorf("--ai-mode %q should be rejected", mode)
		}
	}
}
