package scan

import (
	"reflect"
	"testing"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/rules"
)

// TestAssembleEmptyAlwaysEmitsFixedKeys locks the hardening requirement: even
// with no invalid metrics, risk_distribution has all three keys and
// invalid_type_counts has all seven keys, each zero.
func TestAssembleEmptyAlwaysEmitsFixedKeys(t *testing.T) {
	r := Assemble(0, 5, nil, nil)

	wantRisk := map[string]int{"severe": 0, "warning": 0, "minor": 0}
	if !reflect.DeepEqual(r.Summary.RiskDistribution, wantRisk) {
		t.Errorf("risk_distribution = %v, want %v", r.Summary.RiskDistribution, wantRisk)
	}

	wantTypes := map[string]int{
		"deprecated_metric": 0, "duplicate_metric": 0, "empty_label_value": 0,
		"invalid_label_name": 0, "meaningless_metric": 0, "orphan_metric": 0, "high_cardinality": 0,
	}
	if !reflect.DeepEqual(r.Summary.InvalidTypeCounts, wantTypes) {
		t.Errorf("invalid_type_counts = %v, want %v", r.Summary.InvalidTypeCounts, wantTypes)
	}

	if r.Summary.ValidMetricNames != 5 || r.Summary.InvalidMetricNames != 0 || r.Summary.InvalidRatio != 0 {
		t.Errorf("counts wrong: %+v", r.Summary)
	}
	if r.InvalidMetrics == nil {
		t.Errorf("invalid_metrics should be a non-nil empty slice")
	}
}

func TestAssembleCountsAndDistribution(t *testing.T) {
	invalids := []model.MetricAnalysis{
		{MetricName: "z_metric", IsInvalid: true, InvalidTypes: []string{rules.TypeHighCardinality}, RiskLevel: rules.RiskSevere, RiskScore: 90},
		{MetricName: "a_metric", IsInvalid: true, InvalidTypes: []string{rules.TypeHighCardinality}, RiskLevel: rules.RiskSevere, RiskScore: 90},
		{MetricName: "m_metric", IsInvalid: true, InvalidTypes: []string{rules.TypeDeprecated, rules.TypeEmptyLabelValue}, RiskLevel: rules.RiskWarning, RiskScore: 60},
		{MetricName: "d_metric", IsInvalid: true, InvalidTypes: []string{rules.TypeOrphan}, RiskLevel: rules.RiskMinor, RiskScore: 35},
	}

	r := Assemble(20, 6, invalids, nil)

	if r.Summary.TotalMetricNames != 6 || r.Summary.InvalidMetricNames != 4 || r.Summary.ValidMetricNames != 2 {
		t.Errorf("counts wrong: total=%d invalid=%d valid=%d",
			r.Summary.TotalMetricNames, r.Summary.InvalidMetricNames, r.Summary.ValidMetricNames)
	}
	if got := r.Summary.InvalidRatio; got != 0.6667 {
		t.Errorf("invalid_ratio = %v, want 0.6667", got)
	}

	wantRisk := map[string]int{"severe": 2, "warning": 1, "minor": 1}
	if !reflect.DeepEqual(r.Summary.RiskDistribution, wantRisk) {
		t.Errorf("risk_distribution = %v, want %v", r.Summary.RiskDistribution, wantRisk)
	}

	tc := r.Summary.InvalidTypeCounts
	if tc["high_cardinality"] != 2 || tc["deprecated_metric"] != 1 || tc["empty_label_value"] != 1 || tc["orphan_metric"] != 1 {
		t.Errorf("invalid_type_counts wrong: %v", tc)
	}
	if tc["duplicate_metric"] != 0 || tc["invalid_label_name"] != 0 || tc["meaningless_metric"] != 0 {
		t.Errorf("zero-count types missing or non-zero: %v", tc)
	}
	if len(tc) != 7 {
		t.Errorf("invalid_type_counts must have 7 keys, got %d", len(tc))
	}
}

func TestTopRiskMetricsSorted(t *testing.T) {
	invalids := []model.MetricAnalysis{
		{MetricName: "z_metric", IsInvalid: true, InvalidTypes: []string{rules.TypeHighCardinality}, RiskLevel: rules.RiskSevere, RiskScore: 90},
		{MetricName: "a_metric", IsInvalid: true, InvalidTypes: []string{rules.TypeHighCardinality}, RiskLevel: rules.RiskSevere, RiskScore: 90},
		{MetricName: "m_metric", IsInvalid: true, InvalidTypes: []string{rules.TypeDeprecated}, RiskLevel: rules.RiskWarning, RiskScore: 60},
		{MetricName: "d_metric", IsInvalid: true, InvalidTypes: []string{rules.TypeOrphan}, RiskLevel: rules.RiskMinor, RiskScore: 35},
	}

	r := Assemble(20, 6, invalids, nil)

	// Score desc, then name asc for ties (a_metric before z_metric at 90).
	want := []string{"a_metric", "z_metric", "m_metric", "d_metric"}
	var got []string
	for _, ref := range r.TopRiskMetrics {
		got = append(got, ref.MetricName)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("top_risk order = %v, want %v", got, want)
	}
}

func TestTopViolationLabelsAggregation(t *testing.T) {
	contribs := []model.LabelContribution{
		{MetricName: "z_metric", LabelKey: "user_id", InvalidType: rules.TypeHighCardinality, RiskScore: 90, SeriesCount: 3},
		{MetricName: "a_metric", LabelKey: "user_id", InvalidType: rules.TypeHighCardinality, RiskScore: 90, SeriesCount: 5},
		{MetricName: "m_metric", LabelKey: "env", InvalidType: rules.TypeEmptyLabelValue, RiskScore: 60, SeriesCount: 1},
	}

	r := Assemble(20, 6, nil, contribs)

	if len(r.TopViolationLabels) != 2 {
		t.Fatalf("expected 2 violation labels, got %d: %+v", len(r.TopViolationLabels), r.TopViolationLabels)
	}

	// Highest score first: user_id (90) before env (60).
	top := r.TopViolationLabels[0]
	if top.LabelKey != "user_id" || top.InvalidType != rules.TypeHighCardinality {
		t.Errorf("top label = %q/%q", top.LabelKey, top.InvalidType)
	}
	if top.MetricCount != 2 || top.SeriesCount != 8 || top.RiskScore != 90 || top.RiskLevel != rules.RiskSevere {
		t.Errorf("user_id aggregation wrong: %+v", top)
	}
	wantSamples := []string{"a_metric", "z_metric"} // sorted
	if !reflect.DeepEqual(top.SampleMetricNames, wantSamples) {
		t.Errorf("samples = %v, want %v", top.SampleMetricNames, wantSamples)
	}

	second := r.TopViolationLabels[1]
	if second.LabelKey != "env" || second.MetricCount != 1 || second.SeriesCount != 1 || second.RiskScore != 60 {
		t.Errorf("env aggregation wrong: %+v", second)
	}
}
