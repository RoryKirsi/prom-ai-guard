package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readScanLog(t *testing.T, outDir string) ([]map[string]any, []byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, "scan.log.jsonl"))
	if err != nil {
		t.Fatalf("scan.log.jsonl missing: %v", err)
	}
	var evs []map[string]any
	for i, line := range bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("scan.log.jsonl line %d is not valid JSON: %v\n%s", i, err, line)
		}
		evs = append(evs, m)
	}
	return evs, data
}

func countEvent(evs []map[string]any, name string) int {
	n := 0
	for _, m := range evs {
		if m["event"] == name {
			n++
		}
	}
	return n
}

func TestScanLogSuccessPath(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	cfg := setupConfigDir(t)
	outDir := t.TempDir()
	if _, err := runScanCmd(t, "--input", "../../fixtures/demo_metrics.prom", "--config", cfg, "--out", outDir, "--ai-mode", "local_rules"); err != nil {
		t.Fatal(err)
	}
	evs, _ := readScanLog(t, outDir)
	for _, want := range []string{
		"scan_started", "source_read_started", "source_read_completed",
		"parse_warnings_summary", "local_rules_completed", "ai_completed",
		"report_written", "scan_completed",
	} {
		if countEvent(evs, want) == 0 {
			t.Errorf("missing required event %q", want)
		}
	}
	if n := countEvent(evs, "report_written"); n != 4 {
		t.Errorf("report_written count = %d, want 4 (json/md/xlsx/preview)", n)
	}
	// scan_completed carries exit_code 0; no scan_failed on success.
	if countEvent(evs, "scan_failed") != 0 {
		t.Errorf("scan_failed must not appear on success")
	}
}

func TestScanLogAIBatchFailureEvents(t *testing.T) {
	t.Setenv("LLM_API_KEY", "k")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"not analyzer json"}}]}`) // unparseable -> invalid_response
	}))
	defer srv.Close()
	cfg := setupConfigDir(t)
	outDir := t.TempDir()
	if _, err := runScanCmd(t, "--input", "../../fixtures/demo_metrics.prom", "--config", cfg, "--out", outDir,
		"--ai-mode", "llm_fullscan", "--ai-scope", "all", "--base-url", srv.URL, "--model", "x", "--ai-batch-size", "5"); err != nil {
		t.Fatal(err)
	}
	evs, _ := readScanLog(t, outDir)
	if countEvent(evs, "ai_batch_failure") == 0 {
		t.Fatalf("expected ai_batch_failure events")
	}
	for _, m := range evs {
		if m["event"] != "ai_batch_failure" {
			continue
		}
		if _, ok := m["batch_index"]; !ok {
			t.Errorf("ai_batch_failure missing batch_index: %v", m)
		}
		if _, ok := m["metric_count"]; !ok {
			t.Errorf("ai_batch_failure missing metric_count: %v", m)
		}
		if m["reason"] != "invalid_response" {
			t.Errorf("ai_batch_failure reason = %v, want safe category", m["reason"])
		}
	}
	// ai_batch_summary should report the failures.
	for _, m := range evs {
		if m["event"] == "ai_batch_summary" && m["failed_batches"].(float64) == 0 {
			t.Errorf("ai_batch_summary failed_batches should be > 0")
		}
	}
}

func TestScanLogParseWarningsNoRawLines(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	cfg := setupConfigDir(t)
	mp := filepath.Join(t.TempDir(), "m.prom")
	const badLine = "this_is_not_a_valid metric line ::"
	if err := os.WriteFile(mp, []byte("good_metric{service=\"payment-api\"} 1\n"+badLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	if _, err := runScanCmd(t, "--input", mp, "--config", cfg, "--out", outDir, "--ai-mode", "local_rules"); err != nil {
		t.Fatal(err)
	}
	evs, data := readScanLog(t, outDir)
	found := false
	for _, m := range evs {
		if m["event"] == "parse_warnings_summary" && m["parse_warnings"].(float64) > 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected parse_warnings_summary with count > 0")
	}
	if bytes.Contains(data, []byte("this_is_not_a_valid")) {
		t.Errorf("raw malformed metric line must NOT appear in scan.log.jsonl")
	}
}

func TestScanLogNoSecretLeak(t *testing.T) {
	const secret = "TESTSECRET123"
	t.Setenv("LLM_API_KEY", secret)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"metrics\":[],\"summary\":\"ok\"}"}}]}`)
	}))
	defer srv.Close()
	cfg := setupConfigDir(t)
	outDir := t.TempDir()
	if _, err := runScanCmd(t, "--input", "../../fixtures/demo_metrics.prom", "--config", cfg, "--out", outDir,
		"--ai-mode", "llm_fullscan", "--ai-scope", "all", "--base-url", srv.URL, "--model", "x", "--ai-batch-size", "5"); err != nil {
		t.Fatal(err)
	}
	_, data := readScanLog(t, outDir)
	if bytes.Contains(data, []byte(secret)) {
		t.Errorf("API key leaked into scan.log.jsonl")
	}
}

func TestScanLogScanFailedSanitized(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	cfg := setupConfigDir(t)
	outDir := t.TempDir()
	_, err := runScanCmd(t, "--input", filepath.Join(t.TempDir(), "nope.prom"), "--config", cfg, "--out", outDir, "--ai-mode", "local_rules")
	if err == nil {
		t.Fatal("expected scan to fail on missing input")
	}
	evs, _ := readScanLog(t, outDir)
	if countEvent(evs, "scan_failed") != 1 {
		t.Fatalf("expected exactly one scan_failed event")
	}
	for _, m := range evs {
		if m["event"] != "scan_failed" {
			continue
		}
		reason, _ := m["reason"].(string)
		if strings.ContainsAny(reason, "\n\t") {
			t.Errorf("scan_failed reason must be single-line: %q", reason)
		}
		if len(reason) > 300 {
			t.Errorf("scan_failed reason len = %d, want <= 300", len(reason))
		}
		if m["exit_code"].(float64) != 1 || m["level"] != "error" {
			t.Errorf("scan_failed exit/level = %v/%v", m["exit_code"], m["level"])
		}
	}
}

func TestScanLogOpenFailureWarnsAndContinues(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	cfg := setupConfigDir(t)
	outDir := t.TempDir()
	// Make scan.log.jsonl a directory so opening it for writing fails.
	if err := os.Mkdir(filepath.Join(outDir, "scan.log.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runScanCmd(t, "--input", "../../fixtures/demo_metrics.prom", "--config", cfg, "--out", outDir, "--ai-mode", "local_rules")
	if err != nil {
		t.Fatalf("audit-log open failure must NOT fail the scan: %v", err)
	}
	if !strings.Contains(out, "scan.log.jsonl") || !strings.Contains(out, "continuing without audit log") {
		t.Errorf("expected a single stderr warning about the audit log:\n%s", out)
	}
	// The scan still produced its report.
	if _, statErr := os.Stat(filepath.Join(outDir, "analysis_report.json")); statErr != nil {
		t.Errorf("scan must still write the report despite audit-log failure: %v", statErr)
	}
}
