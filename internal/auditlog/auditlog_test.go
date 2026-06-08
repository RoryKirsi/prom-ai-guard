package auditlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// decodeLines splits a JSONL buffer into decoded maps, asserting each is valid JSON.
func decodeLines(t *testing.T, b []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	for i, line := range bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("line %d is not valid JSON: %v\n%s", i, err, line)
		}
		out = append(out, m)
	}
	return out
}

func TestEmitValidJSONLAndCommonFields(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, nil, "scan-1")
	l.ScanStarted("file", "", "/data/m.prom", "all", "llm_fullscan", "all", 50)
	l.SourceReadCompleted("file", 0) // 0 must be present (pointer field)
	l.ScanCompleted(0, map[string]int{"severe": 0, "warning": 0, "minor": 0}, 0, "disabled")

	lines := decodeLines(t, buf.Bytes())
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	for _, m := range lines {
		for _, k := range []string{"timestamp", "scan_id", "level", "event"} {
			if _, ok := m[k]; !ok {
				t.Errorf("event %v missing common field %q", m["event"], k)
			}
		}
		if m["scan_id"] != "scan-1" {
			t.Errorf("scan_id = %v", m["scan_id"])
		}
	}
	// 0 series_count must be present, not dropped.
	if v, ok := lines[1]["series_count"]; !ok || v.(float64) != 0 {
		t.Errorf("series_count 0 must be emitted, got %v ok=%v", v, ok)
	}
	if lines[0]["event"] != "scan_started" || lines[0]["input_ref"] != "/data/m.prom" {
		t.Errorf("scan_started fields = %v", lines[0])
	}
}

func TestSanitizeURL(t *testing.T) {
	cases := map[string]string{
		"http://user:pass@h:9090/api/v1?x=1":     "http://h:9090",
		"http://prometheus.monitoring.svc:9090/": "http://prometheus.monitoring.svc:9090",
		"https://prom.internal":                  "https://prom.internal",
		"":                                       "",
		"://broken":                              "",
	}
	for in, want := range cases {
		if got := sanitizeURL(in); got != want {
			t.Errorf("sanitizeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeReasonNoNewlineAndCapped(t *testing.T) {
	r := sanitizeReason("opening input:\n  bad\tfile\nerror")
	if strings.ContainsAny(r, "\n\t") {
		t.Errorf("reason must be single-line: %q", r)
	}
	if r != "opening input: bad file error" {
		t.Errorf("reason = %q", r)
	}
	long := strings.Repeat("x", 1000)
	if got := sanitizeReason(long); len(got) > reasonMaxLen {
		t.Errorf("reason len = %d, want <= %d", len(got), reasonMaxLen)
	}
	if !strings.Contains(sanitizeReason(long), "[truncated]") {
		t.Errorf("over-long reason must be marked truncated")
	}
}

func TestScanFailedReasonSanitized(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, nil, "s").ScanFailed("boom\nwith newline " + strings.Repeat("y", 1000))
	m := decodeLines(t, buf.Bytes())[0]
	reason := m["reason"].(string)
	if strings.Contains(reason, "\n") || len(reason) > reasonMaxLen {
		t.Errorf("scan_failed reason not sanitized: len=%d newline=%v", len(reason), strings.Contains(reason, "\n"))
	}
	if m["exit_code"].(float64) != 1 || m["level"] != "error" {
		t.Errorf("scan_failed exit/level = %v/%v", m["exit_code"], m["level"])
	}
}

// failWriter fails every Write; used to verify the warn-once behavior.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteFailureWarnsOnce(t *testing.T) {
	var stderr bytes.Buffer
	l := New(failWriter{}, &stderr, "s")
	l.ScanStarted("file", "", "/m", "all", "local_rules", "all", 50)
	l.SourceReadCompleted("file", 1)
	l.ScanCompleted(0, nil, 0, "disabled")
	// Exactly one warning despite three failed writes.
	if n := strings.Count(stderr.String(), "scan.log.jsonl write failed"); n != 1 {
		t.Errorf("stderr warnings = %d, want exactly 1:\n%s", n, stderr.String())
	}
}

func TestNilLoggerIsNoOp(t *testing.T) {
	var l *Logger
	// Must not panic.
	l.ScanStarted("file", "", "/m", "all", "local_rules", "all", 50)
	l.ScanFailed("x")
}

func TestAIBatchFailureSafeFields(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, nil, "s").AIBatchFailure(2, 50, "invalid_response")
	m := decodeLines(t, buf.Bytes())[0]
	if m["event"] != "ai_batch_failure" || m["batch_index"].(float64) != 2 ||
		m["metric_count"].(float64) != 50 || m["reason"] != "invalid_response" {
		t.Errorf("ai_batch_failure = %v", m)
	}
}
