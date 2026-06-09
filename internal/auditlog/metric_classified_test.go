package auditlog

import (
	"bytes"
	"strings"
	"testing"
)

func TestMetricClassifiedSafeFields(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, nil, "scan-1").MetricClassified(
		"http_user_requests_total",
		[]string{"high_cardinality"}, "severe", 90,
		[]string{"label:user_id:high_cardinality"},
		[]string{"local_rules", "llm"},
	)
	m := decodeLines(t, buf.Bytes())[0]

	if m["event"] != "metric_classified" {
		t.Fatalf("event = %v", m["event"])
	}
	if m["metric_name"] != "http_user_requests_total" || m["risk_level"] != "severe" || m["risk_score"].(float64) != 90 {
		t.Errorf("core fields = %v", m)
	}
	for _, k := range []string{"invalid_types", "rule_signals", "analysis_sources"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing field %q: %v", k, m)
		}
	}
	// common audit fields still present.
	for _, k := range []string{"timestamp", "scan_id", "level"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing common field %q", k)
		}
	}
}

// rule_signals may carry label KEYS (metadata), but never a raw label VALUE.
func TestMetricClassifiedNoRawLabelValue(t *testing.T) {
	var buf bytes.Buffer
	const sensitiveValue = "alice@example.com"
	New(&buf, nil, "s").MetricClassified(
		"http_requests_total", []string{"high_cardinality"}, "severe", 90,
		[]string{"label:user_id:high_cardinality"}, []string{"local_rules"},
	)
	if strings.Contains(buf.String(), sensitiveValue) {
		t.Errorf("metric_classified must not contain raw label values")
	}
}
