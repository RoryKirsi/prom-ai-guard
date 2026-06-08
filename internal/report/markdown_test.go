package report

import (
	"strings"
	"testing"

	"prom-ai-guard/internal/ai"
	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/scan"
)

// sampleReport is a deterministic report.Report used by the markdown and excel
// writer tests. It is built directly (no analysis re-run).
func sampleReport() Report {
	return Report{
		SchemaVersion: "v1",
		ScanID:        "20260608T000000Z-scan",
		ScanTime:      "2026-06-08T00:00:00Z",
		ToolVersion:   "0.1.0",
		ConfigHash:    "sha256:cfg",
		Source: Source{
			SourceType: "file", InputRef: "fixtures/demo_metrics.prom",
			ScanScope: "all", SeriesCount: 16, MetricNameCount: 11,
		},
		AI: &ai.Info{
			Provider: "deepseek", Model: "deepseek-v4-flash", BaseURL: "https://api.deepseek.com",
			AIMode: "llm_fullscan", AIScope: "all", Enabled: true, Status: "success",
			AnalysisMethod: "llm_fullscan", AttemptCount: 1, AnalyzedMetricCount: 11,
			Summary: "AI advisory summary text.", ConfigHash: "sha256:ai",
		},
		Summary: scan.Summary{
			TotalSeries: 16, TotalMetricNames: 11, ValidMetricNames: 4, InvalidMetricNames: 7,
			InvalidRatio:     0.6364,
			RiskDistribution: map[string]int{"severe": 1, "warning": 4, "minor": 2},
			InvalidTypeCounts: map[string]int{
				"deprecated_metric": 1, "duplicate_metric": 1, "empty_label_value": 1,
				"invalid_label_name": 1, "meaningless_metric": 1, "orphan_metric": 1, "high_cardinality": 1,
			},
		},
		InvalidMetrics: []model.MetricAnalysis{{
			MetricName: "http_user_requests_total", IsInvalid: true,
			InvalidTypes: []string{"high_cardinality"}, RiskLevel: "severe", RiskScore: 90, Confidence: 1.0,
			RuleSignals: []string{"label:user_id:high_cardinality"},
			RiskReason:  "unbounded user_id", RootCause: "high cardinality label",
			Recommendations: []string{"drop user_id"},
			Owner:           "platform", Service: "payment-api", Namespace: "payments",
			SeriesCount: 3, LabelCardinality: map[string]int{"user_id": 3, "service": 1},
			RelabelCandidate: true, AnalysisSources: []string{"local_rules", "llm"},
		}},
		TopRiskMetrics: []model.RiskRef{{
			MetricName: "http_user_requests_total", RiskLevel: "severe", RiskScore: 90,
			InvalidTypes: []string{"high_cardinality"},
		}},
		TopViolationLabels: []model.LabelViolation{{
			LabelKey: "user_id", InvalidType: "high_cardinality", RiskLevel: "severe", RiskScore: 90,
			MetricCount: 1, SeriesCount: 3, SampleMetricNames: []string{"http_user_requests_total"},
		}},
		Warnings: []model.ParseWarning{{Line: 39, Raw: "this is not a metric line", Reason: `invalid value "is"`}},
	}
}

func TestRenderMarkdownHasRequiredSections(t *testing.T) {
	md := RenderMarkdown(sampleReport())
	for _, want := range []string{
		"# prom-ai-guard analysis report",
		"## Scan",
		"## Source",
		"## AI analysis",
		"## Summary",
		"## Risk distribution",
		"## Invalid type counts",
		"## Top risk metrics",
		"## Top violation labels",
		"## Invalid metric details",
		"## Parse warnings",
		"## Report files",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing section %q", want)
		}
	}
}

func TestRenderMarkdownAdvisoryNote(t *testing.T) {
	md := RenderMarkdown(sampleReport())
	if !strings.Contains(md, "advisory") || !strings.Contains(md, "authoritative") {
		t.Errorf("markdown must note AI summary is advisory and counts are authoritative")
	}
}

func TestRenderMarkdownContent(t *testing.T) {
	md := RenderMarkdown(sampleReport())
	for _, want := range []string{
		"llm_fullscan",              // AI mode
		"deepseek-v4-flash",         // model
		"AI advisory summary text.", // AI summary surfaced
		"http_user_requests_total",  // top-risk + invalid metric
		"high_cardinality",          // invalid type
		"local_rules", "llm",        // analysis_sources
		"unbounded user_id", // risk_reason
		"line 39",           // parse warning
		"analysis_report.json", "analysis_report.md", "analysis_report.xlsx", "ai_input_preview.json",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing content %q", want)
		}
	}
}

func TestRenderMarkdownStableForMaps(t *testing.T) {
	// Map-backed sections must render identically across runs (sorted keys).
	r := sampleReport()
	if RenderMarkdown(r) != RenderMarkdown(r) {
		t.Errorf("markdown rendering is not deterministic")
	}
}
