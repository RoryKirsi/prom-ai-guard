package storage

import (
	"strings"
	"testing"

	"prom-ai-guard/internal/model"
)

func hasStorageRec(recs []string) bool {
	for _, r := range recs {
		if strings.Contains(r, "TSDB storage optimization") {
			return true
		}
	}
	return false
}

func TestAnnotateAppendsStorageRecommendationWhenWarranted(t *testing.T) {
	// hi: high label cardinality (>= demo high threshold 20) -> high impact.
	hi := model.MetricAnalysis{
		MetricName: "hc", SeriesCount: 50, InvalidTypes: []string{"high_cardinality"},
		LabelCardinality: map[string]int{"user_id": 30}, Recommendations: []string{"existing rec"},
	}
	// lo: tiny, low impact, not high-cardinality -> no storage rec.
	lo := model.MetricAnalysis{
		MetricName: "lo", SeriesCount: 1, InvalidTypes: []string{"orphan_metric"},
		LabelCardinality: map[string]int{"service": 1}, Recommendations: []string{"keep"},
	}
	metrics := []model.MetricAnalysis{hi, lo}
	Annotate(metrics, Thresholds{})

	if metrics[0].StorageImpact == nil || metrics[0].StorageImpact.ImpactLevel != "high" {
		t.Fatalf("hi impact level = %v", metrics[0].StorageImpact)
	}
	if !hasStorageRec(metrics[0].Recommendations) {
		t.Errorf("high-impact metric should gain a TSDB storage-optimization recommendation: %v", metrics[0].Recommendations)
	}
	// existing recommendation preserved (appended, not replaced).
	if metrics[0].Recommendations[0] != "existing rec" {
		t.Errorf("existing recommendation must be preserved: %v", metrics[0].Recommendations)
	}
	// low-impact, non-high-cardinality metric: unchanged.
	if hasStorageRec(metrics[1].Recommendations) {
		t.Errorf("low-impact metric should NOT gain a storage recommendation: %v", metrics[1].Recommendations)
	}
	if len(metrics[1].Recommendations) != 1 || metrics[1].Recommendations[0] != "keep" {
		t.Errorf("low-impact recommendations changed: %v", metrics[1].Recommendations)
	}
}
