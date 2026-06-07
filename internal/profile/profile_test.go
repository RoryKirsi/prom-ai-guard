package profile

import (
	"reflect"
	"testing"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/tsdb"
)

func TestBuildSortsAndAttachesSignals(t *testing.T) {
	stats := tsdb.Build([]model.MetricSeries{
		{MetricName: "z_metric", Labels: map[string]string{"service": "svc-z"}, Value: 1},
		{MetricName: "a_metric", Labels: map[string]string{"user_id": "u2", "service": "svc-a"}, Value: 1},
		{MetricName: "a_metric", Labels: map[string]string{"user_id": "u1", "service": "svc-a"}, Value: 1},
	})
	analyses := map[string]model.MetricAnalysis{
		"a_metric": {
			MetricName:   "a_metric",
			RuleSignals:  []string{"label:user_id:high_cardinality"},
			InvalidTypes: []string{"high_cardinality"},
		},
	}
	ctx := map[string]model.MetricContext{
		"a_metric": {Owner: "team-a", Service: "svc-a", Namespace: "ns-a", Jobs: []string{"job-a"}},
		"z_metric": {Owner: "team-z", Service: "svc-z"},
	}

	profiles := Build(stats, analyses, ctx, 5)

	// Profiles sorted by metric name.
	if profiles[0].MetricName != "a_metric" || profiles[1].MetricName != "z_metric" {
		t.Fatalf("profiles not sorted: %s, %s", profiles[0].MetricName, profiles[1].MetricName)
	}

	a := profiles[0]
	// label_keys sorted.
	if !reflect.DeepEqual(a.LabelKeys, []string{"service", "user_id"}) {
		t.Errorf("label_keys = %v", a.LabelKeys)
	}
	// sample values sorted ascending.
	if !reflect.DeepEqual(a.SampleLabelValues["user_id"], []string{"u1", "u2"}) {
		t.Errorf("user_id samples = %v, want [u1 u2]", a.SampleLabelValues["user_id"])
	}
	// rule signals and context attached.
	if !reflect.DeepEqual(a.RuleSignals, []string{"label:user_id:high_cardinality"}) {
		t.Errorf("rule_signals = %v", a.RuleSignals)
	}
	if a.Owner != "team-a" || a.Namespace != "ns-a" || !reflect.DeepEqual(a.Jobs, []string{"job-a"}) {
		t.Errorf("context wrong: %+v", a)
	}
	if a.SeriesCount != 2 || a.LabelCardinality["user_id"] != 2 {
		t.Errorf("stats wrong: series=%d card=%v", a.SeriesCount, a.LabelCardinality)
	}
}

func TestBuildValidMetricHasEmptySignals(t *testing.T) {
	stats := tsdb.Build([]model.MetricSeries{
		{MetricName: "ok_metric", Labels: map[string]string{"service": "svc"}, Value: 1},
	})
	profiles := Build(stats, map[string]model.MetricAnalysis{}, map[string]model.MetricContext{}, 5)
	if profiles[0].RuleSignals == nil || len(profiles[0].RuleSignals) != 0 {
		t.Errorf("rule_signals should be empty slice, got %v", profiles[0].RuleSignals)
	}
	if profiles[0].InvalidTypes == nil || len(profiles[0].InvalidTypes) != 0 {
		t.Errorf("invalid_types should be empty slice, got %v", profiles[0].InvalidTypes)
	}
}
