package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// scan.log.jsonl records one metric_classified event per invalid metric, so the
// log itself shows how each metric was labelled (for review/replay).
func TestScanLogMetricClassifiedEvents(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	cfg := setupConfigDir(t)
	outDir := t.TempDir()
	if _, err := runScanCmd(t, "--input", "../../fixtures/demo_metrics.prom", "--config", cfg, "--out", outDir, "--ai-mode", "local_rules"); err != nil {
		t.Fatal(err)
	}
	evs, _ := readScanLog(t, outDir)

	// One metric_classified per invalid metric (cross-check local_rules_completed).
	invalidCount := -1
	for _, m := range evs {
		if m["event"] == "local_rules_completed" {
			invalidCount = int(m["invalid_count"].(float64))
		}
	}
	got := countEvent(evs, "metric_classified")
	if got == 0 || got != invalidCount {
		t.Fatalf("metric_classified events = %d, want %d (= invalid_count)", got, invalidCount)
	}

	// The severe high-cardinality metric must be logged with its labelling detail.
	found := false
	for _, m := range evs {
		if m["event"] != "metric_classified" {
			continue
		}
		for _, k := range []string{"metric_name", "invalid_types", "risk_level", "risk_score", "rule_signals"} {
			if _, ok := m[k]; !ok {
				t.Errorf("metric_classified missing %q: %v", k, m)
			}
		}
		if m["metric_name"] == "http_user_requests_total" {
			found = true
			if m["risk_level"] != "severe" {
				t.Errorf("http_user_requests_total risk_level = %v, want severe", m["risk_level"])
			}
		}
	}
	if !found {
		t.Errorf("expected a metric_classified event for http_user_requests_total")
	}
}

// End-to-end redaction: a metric carrying sensitive label VALUES is flagged
// (forbidden user_id -> high_cardinality), so a metric_classified event is
// emitted — but the raw label values must NEVER reach scan.log.jsonl.
func TestScanLogMetricClassifiedNoLabelValueLeak(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	cfg := setupConfigDir(t)
	mp := filepath.Join(t.TempDir(), "sensitive.prom")
	const secretVal = "SECRETVAL"
	body := "" +
		"sensitive_metric{user_id=\"" + secretVal + "_alice@example.com\"} 1\n" +
		"sensitive_metric{user_id=\"" + secretVal + "_bob@example.com\"} 1\n" +
		"sensitive_metric{user_id=\"" + secretVal + "_carol@example.com\"} 1\n"
	if err := os.WriteFile(mp, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	if _, err := runScanCmd(t, "--input", mp, "--config", cfg, "--out", outDir, "--ai-mode", "local_rules"); err != nil {
		t.Fatal(err)
	}
	evs, data := readScanLog(t, outDir)

	// The metric must have been flagged + logged (so we actually exercised the path).
	flagged := false
	for _, m := range evs {
		if m["event"] == "metric_classified" && m["metric_name"] == "sensitive_metric" {
			flagged = true
		}
	}
	if !flagged {
		t.Fatal("sensitive_metric should be flagged (forbidden user_id) and logged via metric_classified")
	}
	// The raw sensitive label values must NOT appear anywhere in the audit log.
	if bytes.Contains(data, []byte(secretVal)) {
		t.Errorf("raw label value leaked into scan.log.jsonl (metric_classified): found %q", secretVal)
	}
}
