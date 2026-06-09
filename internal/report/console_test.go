package report

import (
	"bytes"
	"strings"
	"testing"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/scan"
)

func TestPrintConsoleInvalidRiskList(t *testing.T) {
	r := Report{
		Source: Source{SourceType: "file", InputRef: "fixtures/x.prom"},
		Summary: scan.Summary{
			TotalMetricNames: 2, InvalidMetricNames: 2,
			RiskDistribution:  map[string]int{"severe": 1, "warning": 1, "minor": 0},
			InvalidTypeCounts: map[string]int{"high_cardinality": 1, "invalid_label_name": 1},
		},
		TopRiskMetrics: []model.RiskRef{
			{MetricName: "http_user_requests_total", RiskLevel: "severe", RiskScore: 90, InvalidTypes: []string{"high_cardinality"}},
			{MetricName: "cache_hits_total", RiskLevel: "warning", RiskScore: 60, InvalidTypes: []string{"invalid_label_name"}},
		},
	}
	var buf bytes.Buffer
	PrintConsole(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "invalid_metrics (risk list") {
		t.Errorf("console must print the invalid-metric risk list header:\n%s", out)
	}
	for _, want := range []string{"http_user_requests_total", "severe", "90", "high_cardinality", "cache_hits_total", "invalid_label_name"} {
		if !strings.Contains(out, want) {
			t.Errorf("risk list missing %q:\n%s", want, out)
		}
	}
	// the severe metric must be listed before the warning one (sorted by score).
	if strings.Index(out, "http_user_requests_total") > strings.Index(out, "cache_hits_total") {
		t.Errorf("risk list not ordered by risk:\n%s", out)
	}
}

func TestPrintConsoleNoInvalidsOmitsRiskList(t *testing.T) {
	r := Report{
		Source: Source{SourceType: "file", InputRef: "x"},
		Summary: scan.Summary{
			TotalMetricNames: 5, ValidMetricNames: 5,
			RiskDistribution:  map[string]int{"severe": 0, "warning": 0, "minor": 0},
			InvalidTypeCounts: map[string]int{},
		},
		TopRiskMetrics: nil,
	}
	var buf bytes.Buffer
	PrintConsole(&buf, r)
	if strings.Contains(buf.String(), "invalid_metrics (risk list") {
		t.Errorf("no risk list should be printed when there are no invalid metrics:\n%s", buf.String())
	}
}
