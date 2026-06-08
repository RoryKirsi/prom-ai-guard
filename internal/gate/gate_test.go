package gate

import (
	"testing"

	"prom-ai-guard/internal/ai"
	"prom-ai-guard/internal/config"
	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/report"
	"prom-ai-guard/internal/scan"
)

func intp(v int) *int           { return &v }
func floatp(v float64) *float64 { return &v }

// baseReport is a clean report (no policy violations under a lenient policy).
func baseReport() report.Report {
	return report.Report{
		SchemaVersion: "v1",
		AI:            &ai.Info{FallbackUsed: false},
		Summary: scan.Summary{
			InvalidRatio:      0.1,
			RiskDistribution:  map[string]int{"severe": 0, "warning": 0, "minor": 0},
			InvalidTypeCounts: map[string]int{"high_cardinality": 0},
		},
		InvalidMetrics:     []model.MetricAnalysis{},
		TopViolationLabels: []model.LabelViolation{},
	}
}

func hitIDs(r GateResult) []string {
	ids := make([]string, 0, len(r.PolicyHits))
	for _, h := range r.PolicyHits {
		ids = append(ids, h.PolicyID)
	}
	return ids
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestEvaluatePass(t *testing.T) {
	pol := config.Policy{Gate: config.GatePolicy{
		MaxSevere: intp(0), MaxWarning: intp(20), MaxInvalidRatio: floatp(0.3),
		MaxHighCardinalityMetrics: intp(5), ForbiddenLabelKeys: []string{"user_id"},
	}}
	r := Evaluate(baseReport(), pol)
	if !r.Passed || r.ExitCode != 0 || len(r.PolicyHits) != 0 {
		t.Fatalf("expected pass, got %+v", r)
	}
	if r.PolicyHits == nil {
		t.Errorf("policy_hits must be a non-nil empty slice")
	}
}

func TestEvaluateMaxSevere(t *testing.T) {
	rep := baseReport()
	rep.Summary.RiskDistribution["severe"] = 1
	r := Evaluate(rep, config.Policy{Gate: config.GatePolicy{MaxSevere: intp(0)}})
	if r.Passed || r.ExitCode != 2 || !contains(hitIDs(r), "max_severe") {
		t.Fatalf("expected max_severe failure (exit 2), got %+v", r)
	}
}

func TestEvaluateMaxWarning(t *testing.T) {
	rep := baseReport()
	rep.Summary.RiskDistribution["warning"] = 5
	r := Evaluate(rep, config.Policy{Gate: config.GatePolicy{MaxWarning: intp(2)}})
	if r.ExitCode != 2 || !contains(hitIDs(r), "max_warning") {
		t.Fatalf("expected max_warning failure, got %+v", r)
	}
}

func TestEvaluateMaxInvalidRatio(t *testing.T) {
	rep := baseReport()
	rep.Summary.InvalidRatio = 0.5
	r := Evaluate(rep, config.Policy{Gate: config.GatePolicy{MaxInvalidRatio: floatp(0.3)}})
	if r.ExitCode != 2 || !contains(hitIDs(r), "max_invalid_ratio") {
		t.Fatalf("expected max_invalid_ratio failure, got %+v", r)
	}
}

func TestEvaluateMaxHighCardinality(t *testing.T) {
	rep := baseReport()
	rep.Summary.InvalidTypeCounts["high_cardinality"] = 3
	r := Evaluate(rep, config.Policy{Gate: config.GatePolicy{MaxHighCardinalityMetrics: intp(1)}})
	if r.ExitCode != 2 || !contains(hitIDs(r), "max_high_cardinality_metrics") {
		t.Fatalf("expected high_cardinality failure, got %+v", r)
	}
}

func TestEvaluateFallbackUsed(t *testing.T) {
	rep := baseReport()
	rep.AI.FallbackUsed = true
	// enabled -> hit
	r := Evaluate(rep, config.Policy{Gate: config.GatePolicy{FailOnFallbackUsed: true}})
	if r.ExitCode != 2 || !contains(hitIDs(r), "fail_on_fallback_used") {
		t.Fatalf("expected fallback failure, got %+v", r)
	}
	// disabled -> no hit
	r = Evaluate(rep, config.Policy{Gate: config.GatePolicy{FailOnFallbackUsed: false}})
	if !r.Passed {
		t.Fatalf("fallback must not fail when disabled, got %+v", r)
	}
}

func TestEvaluateForbiddenFromTopViolation(t *testing.T) {
	rep := baseReport()
	rep.TopViolationLabels = []model.LabelViolation{{LabelKey: "user_id", InvalidType: "high_cardinality"}}
	r := Evaluate(rep, config.Policy{Gate: config.GatePolicy{ForbiddenLabelKeys: []string{"user_id", "trace_id"}}})
	if r.ExitCode != 2 || !contains(hitIDs(r), "forbidden_label_keys") {
		t.Fatalf("expected forbidden via top_violation_labels, got %+v", r)
	}
}

func TestEvaluateForbiddenFromInvalidMetricCardinalityOnly(t *testing.T) {
	// The forbidden key is present ONLY in invalid_metrics label_cardinality,
	// not in top_violation_labels — Gate must still catch it.
	rep := baseReport()
	rep.InvalidMetrics = []model.MetricAnalysis{{
		MetricName:       "m",
		LabelCardinality: map[string]int{"session_id": 50, "service": 1},
	}}
	r := Evaluate(rep, config.Policy{Gate: config.GatePolicy{ForbiddenLabelKeys: []string{"session_id"}}})
	if r.ExitCode != 2 || !contains(hitIDs(r), "forbidden_label_keys") {
		t.Fatalf("expected forbidden via invalid_metrics cardinality, got %+v", r)
	}
}

func TestEvaluateAbsentThresholdsNoCheck(t *testing.T) {
	rep := baseReport()
	rep.Summary.RiskDistribution["severe"] = 99
	rep.Summary.InvalidRatio = 0.99
	rep.Summary.InvalidTypeCounts["high_cardinality"] = 99
	// Empty policy: all thresholds nil -> no checks -> pass.
	r := Evaluate(rep, config.Policy{})
	if !r.Passed || r.ExitCode != 0 {
		t.Fatalf("absent thresholds must not enforce, got %+v", r)
	}
}
