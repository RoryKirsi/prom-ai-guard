package doctor

import (
	"reflect"
	"strings"
	"testing"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/report"
)

func sampleReport() report.Report {
	return report.Report{
		SchemaVersion: "v1",
		ScanID:        "scan-1",
		ScanTime:      "2026-06-08T00:00:00Z",
		Source:        report.Source{SourceType: "file"},
		InvalidMetrics: []model.MetricAnalysis{
			{
				MetricName: "http_user_requests_total", RiskLevel: "severe", RiskScore: 90,
				InvalidTypes: []string{"high_cardinality"}, RuleSignals: []string{"label:user_id:high_cardinality"},
				RiskReason: "unbounded user_id", RootCause: "high cardinality label", Recommendations: []string{"drop user_id"},
				Owner: "platform", Service: "payment-api", Namespace: "payments",
				SeriesCount: 3, LabelCardinality: map[string]int{"user_id": 3, "service": 1}, RelabelCandidate: true,
			},
			{
				MetricName: "order_legacy_total", RiskLevel: "warning", RiskScore: 50,
				InvalidTypes: []string{"deprecated_metric"}, RuleSignals: []string{"metric:deprecated"},
				Owner: "orders", Service: "orders-api", Namespace: "orders",
				LabelCardinality: map[string]int{"region": 2}, RelabelCandidate: true,
			},
			{
				MetricName: "dup_metric", RiskLevel: "warning", RiskScore: 40,
				InvalidTypes: []string{"duplicate_metric"}, RuleSignals: []string{"metric:duplicate_series"},
				Service: "orders-api", LabelCardinality: map[string]int{}, RelabelCandidate: false,
			},
			{
				MetricName: "ghost_up", RiskLevel: "minor", RiskScore: 20,
				InvalidTypes: []string{"orphan_metric"}, RuleSignals: []string{"service:orphan"},
				Service: "unknown", LabelCardinality: map[string]int{}, RelabelCandidate: false,
			},
			{
				MetricName: "empty_only", RiskLevel: "minor", RiskScore: 20,
				InvalidTypes: []string{"empty_label_value"}, RuleSignals: []string{"label:env:empty_value"},
				Service: "web", LabelCardinality: map[string]int{"env": 1}, RelabelCandidate: true,
			},
		},
	}
}

func names(ds []Diagnosis) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.MetricName
	}
	return out
}

func TestDiagnoseMetricSelector(t *testing.T) {
	d := Diagnose(sampleReport(), Query{Metric: "http_user_requests_total"})
	if d.MatchCount != 1 {
		t.Fatalf("match_count = %d, want 1", d.MatchCount)
	}
	m := d.Matches[0]
	if m.RiskLevel != "severe" || m.RiskScore != 90 {
		t.Errorf("risk = %s/%d", m.RiskLevel, m.RiskScore)
	}
	if !reflect.DeepEqual(m.InvalidTypes, []string{"high_cardinality"}) || len(m.RuleSignals) != 1 {
		t.Errorf("types/signals wrong: %+v", m)
	}
	if m.Owner != "platform" || m.Service != "payment-api" || m.Namespace != "payments" {
		t.Errorf("ownership wrong: %+v", m)
	}
	if !m.RelabelCandidate || !m.RelabelProposalPossible {
		t.Errorf("high_cardinality candidate should have proposal_possible=true: %+v", m)
	}
}

func TestDiagnoseLabelSelector(t *testing.T) {
	d := Diagnose(sampleReport(), Query{Label: "user_id"})
	if !reflect.DeepEqual(names(d.Matches), []string{"http_user_requests_total"}) {
		t.Fatalf("label match = %v, want [http_user_requests_total]", names(d.Matches))
	}
	if !reflect.DeepEqual(d.Matches[0].MatchedLabels, []string{"user_id"}) {
		t.Errorf("matched_labels = %v", d.Matches[0].MatchedLabels)
	}
}

func TestDiagnoseServiceSelector(t *testing.T) {
	d := Diagnose(sampleReport(), Query{Service: "orders-api"})
	if !reflect.DeepEqual(names(d.Matches), []string{"order_legacy_total", "dup_metric"}) {
		t.Errorf("service match = %v (risk desc), want [order_legacy_total dup_metric]", names(d.Matches))
	}
}

func TestDiagnoseAndSelectors(t *testing.T) {
	// metric AND label -> only when both hold.
	d := Diagnose(sampleReport(), Query{Metric: "http_user_requests_total", Label: "user_id"})
	if d.MatchCount != 1 {
		t.Errorf("metric+label AND = %v, want 1", names(d.Matches))
	}
	// metric present but label absent -> 0 matches, no absence note (it IS invalid).
	d2 := Diagnose(sampleReport(), Query{Metric: "http_user_requests_total", Label: "nope"})
	if d2.MatchCount != 0 {
		t.Errorf("metric+absent-label = %v, want 0", names(d2.Matches))
	}
	for _, n := range d2.Notes {
		if strings.Contains(n, "not found in invalid_metrics") {
			t.Errorf("must not emit absence note when metric IS in invalid_metrics")
		}
	}
}

func TestDiagnoseAbsentMetricNote(t *testing.T) {
	d := Diagnose(sampleReport(), Query{Metric: "totally_absent"})
	if d.MatchCount != 0 {
		t.Fatalf("want 0 matches")
	}
	found := false
	for _, n := range d.Notes {
		if strings.Contains(n, "not found in invalid_metrics") && strings.Contains(n, "cannot confirm healthy") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing absence/cannot-confirm-healthy note: %v", d.Notes)
	}
}

func TestRelabelProposalPossibleConservative(t *testing.T) {
	cases := map[string]bool{
		"http_user_requests_total": true,  // high_cardinality + candidate
		"order_legacy_total":       true,  // deprecated_metric + candidate
		"dup_metric":               false, // duplicate + not candidate
		"ghost_up":                 false, // orphan + not candidate
		"empty_only":               false, // empty_label_value + candidate, but not an actionable type
	}
	rep := sampleReport()
	for name, want := range cases {
		d := Diagnose(rep, Query{Metric: name})
		if d.MatchCount != 1 {
			t.Fatalf("%s: want 1 match", name)
		}
		if got := d.Matches[0].RelabelProposalPossible; got != want {
			t.Errorf("%s: relabel_proposal_possible = %v, want %v", name, got, want)
		}
	}
}

func TestDiagnoseDeterministicOrder(t *testing.T) {
	d := Diagnose(sampleReport(), Query{Service: "orders-api"})
	a := names(d.Matches)
	d2 := Diagnose(sampleReport(), Query{Service: "orders-api"})
	if !reflect.DeepEqual(a, names(d2.Matches)) {
		t.Errorf("non-deterministic order")
	}
}
