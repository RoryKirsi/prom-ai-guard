package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const storageFixture = "../../fixtures/storage_impact_demo.prom"

// Slice 12: storage impact is computed for invalid metrics and aggregated under
// summary.storage_impact (NOT a new top-level report key).
func TestScanIncludesStorageImpact(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	cfg := setupConfigDir(t)
	outDir := t.TempDir()

	if _, err := runScanCmd(t, "--input", storageFixture, "--config", cfg, "--out", outDir, "--ai-mode", "local_rules"); err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "analysis_report.json"))
	if err != nil {
		t.Fatal(err)
	}

	var rep struct {
		Summary struct {
			StorageImpact *struct {
				HighImpactMetrics            int    `json:"high_impact_metrics"`
				EstimatedInvalidSeries       int    `json:"estimated_invalid_series"`
				EstimatedInvalidIndexEntries int    `json:"estimated_invalid_index_entries"`
				Heuristic                    string `json:"heuristic"`
				ScopeNote                    string `json:"scope_note"`
				TopStorageImpactMetrics      []struct {
					MetricName string `json:"metric_name"`
				} `json:"top_storage_impact_metrics"`
			} `json:"storage_impact"`
		} `json:"summary"`
		InvalidMetrics []struct {
			MetricName    string `json:"metric_name"`
			StorageImpact *struct {
				ImpactLevel           string `json:"impact_level"`
				EstimatedIndexEntries int    `json:"estimated_index_entries"`
				MaxLabelCardinality   int    `json:"max_label_cardinality"`
			} `json:"storage_impact"`
		} `json:"invalid_metrics"`
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}

	// 1. summary.storage_impact present with disclaimers.
	si := rep.Summary.StorageImpact
	if si == nil {
		t.Fatal("summary.storage_impact must be present")
	}
	if si.Heuristic == "" || si.ScopeNote == "" {
		t.Errorf("summary.storage_impact missing heuristic/scope disclaimers")
	}
	if si.HighImpactMetrics < 1 {
		t.Errorf("expected at least one high-impact metric (25-series user_id), got %d", si.HighImpactMetrics)
	}

	// 2. per-metric storage_impact present on every invalid metric.
	if len(rep.InvalidMetrics) == 0 {
		t.Fatal("expected invalid metrics")
	}
	sawHigh := false
	for _, m := range rep.InvalidMetrics {
		if m.StorageImpact == nil {
			t.Fatalf("invalid metric %s missing storage_impact", m.MetricName)
		}
		if m.StorageImpact.ImpactLevel == "high" {
			sawHigh = true
		}
	}
	if !sawHigh {
		t.Errorf("expected a high impact_level metric in the demo fixture")
	}

	// 3. No new TOP-LEVEL storage key — it is nested under summary.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"storage_impact_summary", "storage_impact"} {
		if _, ok := top[forbidden]; ok {
			t.Errorf("must not add top-level %q key; storage impact belongs under summary", forbidden)
		}
	}
}
