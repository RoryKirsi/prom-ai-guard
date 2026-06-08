package diff

import (
	"strings"
	"testing"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/report"
	"prom-ai-guard/internal/scan"
)

func TestRenderMarkdownSectionsAndNote(t *testing.T) {
	prev := rep("h1", sum(2, 10, 1, 1, 0, 0.2), m("a", "warning", 50, "deprecated_metric"), m("b", "warning", 50, "x"))
	curr := rep("h2", sum(2, 10, 1, 0, 1, 0.2), m("a", "severe", 90, "high_cardinality"), m("c", "minor", 30, "meaningless_metric"))
	md := RenderMarkdown(Compute(prev, curr))

	for _, want := range []string{
		"# prom-ai-guard diff report",
		"## Reports compared",
		"## Summary delta",
		"## Added invalid metrics",
		"## Resolved invalid metrics",
		"## Still invalid metrics",
		"## Risk increased",
		"## Risk decreased",
		"## Invalid type changes",
		"subsets of **Still invalid metrics** and may overlap", // explicit overlap note
		"config_hash changed",                                  // config warning (h1 != h2)
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
}

func TestRenderMarkdownEmptyShowsNone(t *testing.T) {
	r := rep("h", sum(0, 5, 0, 0, 0, 0))
	md := RenderMarkdown(Compute(r, r))
	if !strings.Contains(md, "## Added invalid metrics\n\nnone") {
		t.Errorf("empty added section must render 'none':\n%s", md)
	}
	if strings.Contains(md, "config_hash changed") {
		t.Errorf("no config warning when hashes match")
	}
}

func TestRenderMarkdownDeterministic(t *testing.T) {
	prev := report.Report{SchemaVersion: "v1", Summary: scan.Summary{RiskDistribution: map[string]int{}}}
	curr := report.Report{
		SchemaVersion: "v1",
		Summary:       scan.Summary{RiskDistribution: map[string]int{}},
		InvalidMetrics: []model.MetricAnalysis{
			m("z", "severe", 90, "high_cardinality"), m("a", "minor", 30, "meaningless_metric"),
		},
	}
	if RenderMarkdown(Compute(prev, curr)) != RenderMarkdown(Compute(prev, curr)) {
		t.Errorf("markdown rendering is not deterministic")
	}
}
