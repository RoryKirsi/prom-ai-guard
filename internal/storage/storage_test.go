package storage

import (
	"reflect"
	"testing"

	"prom-ai-guard/internal/model"
)

func metric(name string, series int, card map[string]int) model.MetricAnalysis {
	return model.MetricAnalysis{MetricName: name, SeriesCount: series, LabelCardinality: card}
}

func TestComputeFormulaAndFields(t *testing.T) {
	// series=10, labels=2 (a:5, b:3) ->
	//   estimated = 10*(2+1) + (5+3) = 30 + 8 = 38
	si := Compute(metric("m", 10, map[string]int{"a": 5, "b": 3}), DefaultThresholds())
	if si.EstimatedIndexEntries != 38 {
		t.Errorf("estimated_index_entries = %d, want 38 (series*(labels+1)+sum_card)", si.EstimatedIndexEntries)
	}
	if si.SeriesCount != 10 || si.LabelCount != 2 || si.MaxLabelCardinality != 5 {
		t.Errorf("fields wrong: %+v", si)
	}
	want := []model.LabelCardinalityRef{{LabelKey: "a", Cardinality: 5}, {LabelKey: "b", Cardinality: 3}}
	if !reflect.DeepEqual(si.TopCardinalityLabels, want) {
		t.Errorf("top_cardinality_labels = %v, want %v", si.TopCardinalityLabels, want)
	}
}

func TestImpactLevelByCardinalitySignal(t *testing.T) {
	// Defaults: high_label_cardinality=20, medium=5. A label with 25 distinct
	// values is HIGH even with few series.
	hi := Compute(metric("hi", 25, map[string]int{"user_id": 25}), DefaultThresholds())
	if hi.ImpactLevel != "high" {
		t.Errorf("max_card 25 -> %s, want high", hi.ImpactLevel)
	}
	med := Compute(metric("med", 8, map[string]int{"path": 8}), DefaultThresholds())
	if med.ImpactLevel != "medium" {
		t.Errorf("max_card 8 -> %s, want medium", med.ImpactLevel)
	}
	lo := Compute(metric("lo", 2, map[string]int{"env": 2}), DefaultThresholds())
	if lo.ImpactLevel != "low" {
		t.Errorf("max_card 2 -> %s, want low", lo.ImpactLevel)
	}
}

func TestImpactLevelByIndexEntriesSignal(t *testing.T) {
	// Low max-cardinality but many series+labels -> driven by index entries.
	// series=60, labels=3 (each card 2) -> 60*4 + 6 = 246 >= high(200).
	m := Compute(metric("busy", 60, map[string]int{"a": 2, "b": 2, "c": 2}), DefaultThresholds())
	if m.ImpactLevel != "high" {
		t.Errorf("index-entries-driven level = %s (est=%d), want high", m.ImpactLevel, m.EstimatedIndexEntries)
	}
}

func TestImpactLevelIsMaxOfSignals(t *testing.T) {
	// custom thresholds: index entries say low, cardinality says high -> high.
	th := Thresholds{HighIndexEntries: 100000, MediumIndexEntries: 50000, HighLabelCardinality: 10, MediumLabelCardinality: 5}
	si := Compute(metric("x", 1, map[string]int{"k": 12}), th)
	if si.ImpactLevel != "high" {
		t.Errorf("max(low-entries, high-card) = %s, want high", si.ImpactLevel)
	}
}

func TestEdgeNoLabelsNoSeries(t *testing.T) {
	si := Compute(metric("empty", 0, map[string]int{}), DefaultThresholds())
	if si.ImpactLevel != "low" || si.EstimatedIndexEntries != 0 || si.MaxLabelCardinality != 0 {
		t.Errorf("empty metric should be low/0/0, got %+v", si)
	}
}

func TestAnnotateSummary(t *testing.T) {
	metrics := []model.MetricAnalysis{
		metric("hi", 25, map[string]int{"user_id": 25}), // high
		metric("med", 8, map[string]int{"path": 8}),     // medium
		metric("lo", 2, map[string]int{"env": 2}),       // low
	}
	s := Annotate(metrics, DefaultThresholds())
	if s.HighImpactMetrics != 1 || s.MediumImpactMetrics != 1 || s.LowImpactMetrics != 1 {
		t.Errorf("level counts wrong: %+v", s)
	}
	if s.EstimatedInvalidSeries != 35 {
		t.Errorf("estimated_invalid_series = %d, want 35", s.EstimatedInvalidSeries)
	}
	// each metric got its StorageImpact set
	for _, m := range metrics {
		if m.StorageImpact == nil {
			t.Errorf("Annotate must set StorageImpact on %s", m.MetricName)
		}
	}
	// top list sorted by estimated index entries desc
	if len(s.TopStorageImpactMetrics) != 3 || s.TopStorageImpactMetrics[0].MetricName != "hi" {
		t.Errorf("top list = %+v", s.TopStorageImpactMetrics)
	}
	if s.Heuristic == "" || s.ScopeNote == "" {
		t.Errorf("heuristic/scope disclaimers must be set")
	}
}

func TestAnnotateTopFiveCap(t *testing.T) {
	var metrics []model.MetricAnalysis
	for i := 0; i < 8; i++ {
		metrics = append(metrics, metric(string(rune('a'+i)), i+1, map[string]int{"k": i + 1}))
	}
	s := Annotate(metrics, DefaultThresholds())
	if len(s.TopStorageImpactMetrics) != 5 {
		t.Errorf("top list capped at 5, got %d", len(s.TopStorageImpactMetrics))
	}
}

func TestResolveAppliesDefaults(t *testing.T) {
	got := Resolve(Thresholds{}) // all zero -> all defaults
	if got != DefaultThresholds() {
		t.Errorf("Resolve(zero) = %+v, want defaults", got)
	}
	got2 := Resolve(Thresholds{HighIndexEntries: 9999})
	if got2.HighIndexEntries != 9999 || got2.MediumIndexEntries != DefaultThresholds().MediumIndexEntries {
		t.Errorf("Resolve partial override wrong: %+v", got2)
	}
}
