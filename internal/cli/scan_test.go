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
mode: deepseek_fullscan
model: deepseek-v4-flash
base_url: https://api.deepseek.com
api_key_env: DEEPSEEK_API_KEY
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

func TestScanDeepSeekSecretNeverLeaks(t *testing.T) {
	const secret = "TESTSECRET123"
	t.Setenv("DEEPSEEK_API_KEY", secret)

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
		"--ai-mode", "deepseek_fullscan", "--base-url", srv.URL)
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
	t.Setenv("DEEPSEEK_API_KEY", "shouldnotbeused")
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

func TestScanFallbackWhenKeyMissing(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	cfg := setupConfigDir(t)
	metrics := setupMetrics(t)
	outDir := t.TempDir()

	if _, err := runScanCmd(t, "--input", metrics, "--config", cfg, "--out", outDir, "--ai-mode", "deepseek_fullscan"); err != nil {
		t.Fatal(err)
	}
	ai := readAIBlock(t, outDir)
	if ai.Status != "fallback" || !strings.Contains(ai.FallbackReason, "DEEPSEEK_API_KEY") {
		t.Errorf("ai = %+v, want fallback/missing-key", ai)
	}
}

func TestScanRejectsUnsupportedMode(t *testing.T) {
	cfg := setupConfigDir(t)
	metrics := setupMetrics(t)
	outDir := t.TempDir()
	for _, mode := range []string{"hybrid", "fallback_only"} {
		if _, err := runScanCmd(t, "--input", metrics, "--config", cfg, "--out", outDir, "--ai-mode", mode); err == nil {
			t.Errorf("--ai-mode %q should be rejected", mode)
		}
	}
}
