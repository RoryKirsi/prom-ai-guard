package governance

import (
	"fmt"
	"strings"
	"testing"

	"prom-ai-guard/internal/model"
)

func ma(name, invalidType, level string, score int) model.MetricAnalysis {
	return model.MetricAnalysis{
		MetricName: name, IsInvalid: true, InvalidTypes: []string{invalidType},
		RiskLevel: level, RiskScore: score,
	}
}

// n metrics of one (type, level, score), uniquely named.
func many(n int, invalidType, level string, score int) []model.MetricAnalysis {
	out := make([]model.MetricAnalysis, n)
	for i := 0; i < n; i++ {
		out[i] = ma(fmt.Sprintf("%s_%d", invalidType, i), invalidType, level, score)
	}
	return out
}

func TestAssessZeroStateClean(t *testing.T) {
	g := Assess(nil, 0, nil)
	if g.MaturityScore != 100 || g.MaturityGrade != "A" {
		t.Errorf("clean -> score/grade = %d/%q, want 100/A", g.MaturityScore, g.MaturityGrade)
	}
	if g.TotalInvalid != 0 || len(g.TopSystemicIssues) != 0 || len(g.PrioritizedActions) != 0 || len(g.RecommendedNorms) != 0 {
		t.Errorf("clean should have empty issues/actions/norms, got %+v", g)
	}
	if g.MaturityHeuristic == "" || !strings.Contains(strings.ToLower(g.MaturityHeuristic), "heuristic") {
		t.Errorf("maturity_heuristic must state it is a heuristic: %q", g.MaturityHeuristic)
	}
}

func TestAssessTopSystemicIssueOrdering(t *testing.T) {
	var inv []model.MetricAnalysis
	inv = append(inv, ma("hc_severe", "high_cardinality", "severe", 90))
	inv = append(inv, ma("hc_warn", "high_cardinality", "warning", 60)) // type max=90, count=2
	inv = append(inv, ma("dep", "deprecated_metric", "warning", 50))    // count=1, max=50
	inv = append(inv, many(3, "orphan_metric", "minor", 35)...)         // count=3, max=35

	g := Assess(inv, 0.5, nil)
	got := []string{}
	for _, s := range g.TopSystemicIssues {
		got = append(got, s.InvalidType)
	}
	want := []string{"high_cardinality", "deprecated_metric", "orphan_metric"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("systemic order = %v, want %v (by max_score desc)", got, want)
	}
	if g.TopSystemicIssues[0].MetricCount != 2 || g.TopSystemicIssues[0].MaxScore != 90 || g.TopSystemicIssues[0].MaxRisk != "severe" {
		t.Errorf("high_cardinality issue = %+v", g.TopSystemicIssues[0])
	}
	if g.TopSystemicIssues[2].MetricCount != 3 {
		t.Errorf("orphan count = %d, want 3", g.TopSystemicIssues[2].MetricCount)
	}
}

func TestAssessMaturityBands(t *testing.T) {
	cases := []struct {
		name                string
		ratio               float64
		severe, warn, minor int
		storage             *model.StorageImpactSummary
		wantScore           int
		wantGrade           string
	}{
		{"clean", 0, 0, 0, 0, nil, 100, "A"},
		{"one-severe-low-ratio", 0.1, 1, 0, 0, nil, 86, "B"},                                       // 100-4-10
		{"demo-mix", 0.6364, 1, 4, 2, nil, 51, "D"},                                                // 100-25-10-12-2
		{"high-storage", 0.5, 1, 0, 0, &model.StorageImpactSummary{HighImpactMetrics: 1}, 65, "C"}, // 100-20-10-5
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var inv []model.MetricAnalysis
			inv = append(inv, many(c.severe, "high_cardinality", "severe", 90)...)
			inv = append(inv, many(c.warn, "deprecated_metric", "warning", 50)...)
			inv = append(inv, many(c.minor, "orphan_metric", "minor", 35)...)
			g := Assess(inv, c.ratio, c.storage)
			if g.MaturityScore != c.wantScore || g.MaturityGrade != c.wantGrade {
				t.Errorf("%s: score/grade = %d/%q, want %d/%q", c.name, g.MaturityScore, g.MaturityGrade, c.wantScore, c.wantGrade)
			}
		})
	}
}

func TestAssessPrioritizedActionsOrdering(t *testing.T) {
	inv := []model.MetricAnalysis{
		ma("orphan", "orphan_metric", "minor", 35),
		ma("hc", "high_cardinality", "severe", 90),
		ma("dep", "deprecated_metric", "warning", 50),
	}
	g := Assess(inv, 0.5, nil)
	if len(g.PrioritizedActions) != 3 {
		t.Fatalf("actions = %v, want 3", g.PrioritizedActions)
	}
	// Highest risk first -> the first action must reference high_cardinality.
	if !strings.Contains(g.PrioritizedActions[0], "high-cardinality") && !strings.Contains(g.PrioritizedActions[0], "cardinality") {
		t.Errorf("first action should target the severe high_cardinality issue: %q", g.PrioritizedActions[0])
	}
	// Orphan (minor) must come last.
	if !strings.Contains(g.PrioritizedActions[2], "service_inventory") {
		t.Errorf("last action should be the minor orphan issue: %q", g.PrioritizedActions[2])
	}
}

func TestAssessRecommendedNormsByMix(t *testing.T) {
	tests := []struct {
		invalidType string
		wantSubstr  string
	}{
		{"high_cardinality", "cardinality"},
		{"orphan_metric", "service_inventory"},
		{"deprecated_metric", "naming"},
		{"invalid_label_name", "label name"},
		{"duplicate_metric", "duplicate"},
	}
	for _, tc := range tests {
		g := Assess([]model.MetricAnalysis{ma("m", tc.invalidType, "warning", 50)}, 0.5, nil)
		joined := strings.ToLower(strings.Join(g.RecommendedNorms, " | "))
		if !strings.Contains(joined, strings.ToLower(tc.wantSubstr)) {
			t.Errorf("type %s: norms %v missing %q", tc.invalidType, g.RecommendedNorms, tc.wantSubstr)
		}
		// Any invalid run should also recommend GitOps review of relabel rules.
		if !strings.Contains(joined, "relabel_rules.yaml") {
			t.Errorf("type %s: expected a relabel_rules.yaml GitOps norm; got %v", tc.invalidType, g.RecommendedNorms)
		}
	}
	// Clean run -> no norms.
	if g := Assess(nil, 0, nil); len(g.RecommendedNorms) != 0 {
		t.Errorf("clean run should have no norms, got %v", g.RecommendedNorms)
	}
}

func TestAssessStoragePressureAndTSDBNorm(t *testing.T) {
	st := &model.StorageImpactSummary{HighImpactMetrics: 1, MediumImpactMetrics: 2, LowImpactMetrics: 3, EstimatedInvalidIndexEntries: 999}
	g := Assess([]model.MetricAnalysis{ma("hc", "high_cardinality", "severe", 90)}, 0.5, st)
	if g.StoragePressure == nil || g.StoragePressure.HighImpactMetrics != 1 || g.StoragePressure.EstimatedInvalidIndexEntries != 999 {
		t.Errorf("storage_pressure = %+v", g.StoragePressure)
	}
	// High storage pressure -> a TSDB storage-optimization norm (recording rules).
	if !strings.Contains(strings.ToLower(strings.Join(g.RecommendedNorms, " ")), "recording rules") {
		t.Errorf("expected a TSDB storage-optimization norm; got %v", g.RecommendedNorms)
	}
}
