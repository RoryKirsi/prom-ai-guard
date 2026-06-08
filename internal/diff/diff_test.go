package diff

import (
	"reflect"
	"testing"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/report"
	"prom-ai-guard/internal/scan"
)

func rep(configHash string, summary scan.Summary, metrics ...model.MetricAnalysis) report.Report {
	return report.Report{
		SchemaVersion:  "v1",
		ScanID:         "s",
		ConfigHash:     configHash,
		Summary:        summary,
		InvalidMetrics: metrics,
	}
}

func sum(invalidNames, total, severe, warning, minor int, ratio float64) scan.Summary {
	return scan.Summary{
		InvalidMetricNames: invalidNames,
		TotalMetricNames:   total,
		InvalidRatio:       ratio,
		RiskDistribution:   map[string]int{"severe": severe, "warning": warning, "minor": minor},
		InvalidTypeCounts:  map[string]int{},
	}
}

func m(name, level string, score int, types ...string) model.MetricAnalysis {
	return model.MetricAnalysis{MetricName: name, RiskLevel: level, RiskScore: score, InvalidTypes: types}
}

func names(ds []MetricDiff) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.MetricName
	}
	return out
}

func TestComputeAddedResolvedStill(t *testing.T) {
	prev := rep("h1", sum(2, 10, 1, 1, 0, 0.2), m("a", "severe", 90, "high_cardinality"), m("b", "warning", 50, "deprecated_metric"))
	curr := rep("h1", sum(2, 10, 1, 0, 1, 0.2), m("a", "severe", 90, "high_cardinality"), m("c", "minor", 30, "meaningless_metric"))
	d := Compute(prev, curr)

	if !reflect.DeepEqual(names(d.AddedInvalid), []string{"c"}) {
		t.Errorf("added = %v, want [c]", names(d.AddedInvalid))
	}
	if !reflect.DeepEqual(names(d.ResolvedInvalid), []string{"b"}) {
		t.Errorf("resolved = %v, want [b]", names(d.ResolvedInvalid))
	}
	if !reflect.DeepEqual(names(d.StillInvalid), []string{"a"}) {
		t.Errorf("still = %v, want [a]", names(d.StillInvalid))
	}
}

func TestComputeAddedOnlyEmptyPrevious(t *testing.T) {
	prev := rep("h", sum(0, 5, 0, 0, 0, 0))
	curr := rep("h", sum(1, 5, 1, 0, 0, 0.2), m("x", "severe", 90, "high_cardinality"))
	d := Compute(prev, curr)
	if len(d.AddedInvalid) != 1 || len(d.ResolvedInvalid) != 0 || len(d.StillInvalid) != 0 {
		t.Fatalf("want 1 added only, got added=%d resolved=%d still=%d", len(d.AddedInvalid), len(d.ResolvedInvalid), len(d.StillInvalid))
	}
	a := d.AddedInvalid[0]
	if a.PreviousRiskScore != 0 || a.CurrentRiskScore != 90 || a.PreviousRiskLevel != "" {
		t.Errorf("added metric previous fields must be empty/zero: %+v", a)
	}
}

func TestComputeRiskIncreaseDecrease(t *testing.T) {
	prev := rep("h", sum(2, 10, 1, 1, 0, 0.2), m("up", "warning", 50, "x"), m("down", "severe", 90, "x"))
	curr := rep("h", sum(2, 10, 1, 1, 0, 0.2), m("up", "severe", 90, "x"), m("down", "warning", 50, "x"))
	d := Compute(prev, curr)
	if !reflect.DeepEqual(names(d.RiskIncreased), []string{"up"}) {
		t.Errorf("risk_increased = %v, want [up]", names(d.RiskIncreased))
	}
	if !reflect.DeepEqual(names(d.RiskDecreased), []string{"down"}) {
		t.Errorf("risk_decreased = %v, want [down]", names(d.RiskDecreased))
	}
	// both are also in still (overlap)
	if len(d.StillInvalid) != 2 {
		t.Errorf("still should contain both, got %v", names(d.StillInvalid))
	}
}

func TestComputeTypeChangeSameScore(t *testing.T) {
	// Same score, different types -> only TypeChanges, not risk buckets.
	prev := rep("h", sum(1, 10, 0, 1, 0, 0.1), m("t", "warning", 50, "deprecated_metric"))
	curr := rep("h", sum(1, 10, 0, 1, 0, 0.1), m("t", "warning", 50, "meaningless_metric"))
	d := Compute(prev, curr)
	if len(d.RiskIncreased) != 0 || len(d.RiskDecreased) != 0 {
		t.Errorf("same score must not be in risk buckets")
	}
	if len(d.TypeChanges) != 1 {
		t.Fatalf("want 1 type change, got %d", len(d.TypeChanges))
	}
	tc := d.TypeChanges[0]
	if !reflect.DeepEqual(tc.AddedTypes, []string{"meaningless_metric"}) || !reflect.DeepEqual(tc.RemovedTypes, []string{"deprecated_metric"}) {
		t.Errorf("type change = %+v", tc)
	}
}

func TestComputeSummaryDelta(t *testing.T) {
	prev := rep("h1", sum(5, 20, 2, 3, 0, 0.25))
	curr := rep("h2", sum(3, 22, 1, 2, 0, 0.1364))
	d := Compute(prev, curr)
	sd := d.SummaryDelta
	if sd.InvalidMetricNames.Change != -2 || sd.TotalMetricNames.Change != 2 {
		t.Errorf("count deltas wrong: %+v", sd)
	}
	if sd.Severe.Change != -1 || sd.Warning.Change != -1 || sd.Minor.Change != 0 {
		t.Errorf("risk deltas wrong: %+v", sd)
	}
	if sd.InvalidRatio.Previous != 0.25 || sd.InvalidRatio.Current != 0.1364 {
		t.Errorf("ratio prev/curr wrong: %+v", sd.InvalidRatio)
	}
	if !d.ConfigChanged {
		t.Errorf("config_changed must be true when config_hash differs")
	}
}

func TestComputeIdenticalNoChanges(t *testing.T) {
	r := rep("h", sum(1, 10, 1, 0, 0, 0.1), m("a", "severe", 90, "high_cardinality"))
	d := Compute(r, r)
	if len(d.AddedInvalid) != 0 || len(d.ResolvedInvalid) != 0 || len(d.RiskIncreased) != 0 ||
		len(d.RiskDecreased) != 0 || len(d.TypeChanges) != 0 {
		t.Errorf("identical reports must show no changes: %+v", d)
	}
	if d.ConfigChanged {
		t.Errorf("config_changed must be false for identical config_hash")
	}
	if len(d.StillInvalid) != 1 {
		t.Errorf("still should list the unchanged metric")
	}
}

func TestComputeDeterministicOrder(t *testing.T) {
	prev := rep("h", sum(0, 9, 0, 0, 0, 0))
	curr := rep("h", sum(3, 9, 1, 1, 1, 0.33),
		m("low", "minor", 30, "x"), m("high", "severe", 90, "x"), m("mid", "warning", 50, "x"))
	d := Compute(prev, curr)
	if !reflect.DeepEqual(names(d.AddedInvalid), []string{"high", "mid", "low"}) {
		t.Errorf("order = %v, want risk_score desc [high mid low]", names(d.AddedInvalid))
	}
}
