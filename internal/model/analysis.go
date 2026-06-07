package model

// MetricAnalysis is the per-metric invalid-metric result (contract §5.4). In
// Slice 2 it is produced entirely by local rules; later slices let DeepSeek
// refine root_cause, recommendations and confidence. Slice 2 always reports
// confidence=1.0 because rule hits are deterministic.
type MetricAnalysis struct {
	MetricName       string         `json:"metric_name"`
	IsInvalid        bool           `json:"is_invalid"`
	InvalidTypes     []string       `json:"invalid_types"`
	RiskLevel        string         `json:"risk_level"`
	RiskScore        int            `json:"risk_score"`
	Confidence       float64        `json:"confidence"`
	RuleSignals      []string       `json:"rule_signals"`
	RootCause        string         `json:"root_cause"`
	Recommendations  []string       `json:"recommendations"`
	Owner            string         `json:"owner"`
	Service          string         `json:"service"`
	Namespace        string         `json:"namespace"`
	SeriesCount      int            `json:"series_count"`
	LabelCardinality map[string]int `json:"label_cardinality"`
	RelabelCandidate bool           `json:"relabel_candidate"`
}

// RiskRef is a slim entry for the top_risk_metrics list.
type RiskRef struct {
	MetricName   string   `json:"metric_name"`
	RiskLevel    string   `json:"risk_level"`
	RiskScore    int      `json:"risk_score"`
	InvalidTypes []string `json:"invalid_types"`
}

// LabelViolation is an aggregated entry for top_violation_labels: one
// (label_key, invalid_type) pair rolled up across the metrics it affects.
type LabelViolation struct {
	LabelKey          string   `json:"label_key"`
	InvalidType       string   `json:"invalid_type"`
	RiskLevel         string   `json:"risk_level"`
	RiskScore         int      `json:"risk_score"`
	MetricCount       int      `json:"metric_count"`
	SeriesCount       int      `json:"series_count"`
	SampleMetricNames []string `json:"sample_metric_names"`
}

// LabelContribution is a single label-scoped rule hit emitted by the engine,
// later aggregated into LabelViolation entries. It is not serialized directly.
type LabelContribution struct {
	MetricName  string
	LabelKey    string
	InvalidType string
	RiskScore   int
	SeriesCount int
}
