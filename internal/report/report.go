// Package report assembles and writes scan results.
//
// The JSON report is the machine contract (analysis_report.json). Slice 1
// emits only the fields it can populate honestly — source, summary, warnings
// and identity metadata — following outputs/11-implementation-contracts.md §5.
// Fields owned by later slices (ai, invalid_metrics, gate_result, ...) are
// deliberately omitted rather than faked.
package report

import (
	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/scan"
)

// ToolVersion is the current tool version stamped into reports.
const ToolVersion = "0.1.0"

// Source describes where the scanned metrics came from (contract §5.1).
type Source struct {
	SourceType      string `json:"source_type"`
	InputRef        string `json:"input_ref"`
	PromURL         string `json:"prom_url"`
	ScanScope       string `json:"scan_scope"`
	SeriesCount     int    `json:"series_count"`
	MetricNameCount int    `json:"metric_name_count"`
}

// Report is the top-level analysis_report.json structure. Slice 2 adds
// config_hash, the rule-derived invalid_metrics and the top lists.
type Report struct {
	SchemaVersion      string                 `json:"schema_version"`
	ScanID             string                 `json:"scan_id"`
	ScanTime           string                 `json:"scan_time"`
	ToolVersion        string                 `json:"tool_version"`
	ConfigHash         string                 `json:"config_hash"`
	Source             Source                 `json:"source"`
	Summary            scan.Summary           `json:"summary"`
	InvalidMetrics     []model.MetricAnalysis `json:"invalid_metrics"`
	TopRiskMetrics     []model.RiskRef        `json:"top_risk_metrics"`
	TopViolationLabels []model.LabelViolation `json:"top_violation_labels"`
	Warnings           []model.ParseWarning   `json:"warnings"`
}
