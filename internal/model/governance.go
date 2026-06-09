package model

// GovernanceAssessment is a deterministic, batch-level monitoring-governance
// assessment. It is nested under summary.governance_assessment (NOT a top-level
// report key) and is computed entirely from the scan result (invalid metrics +
// risk + storage impact). It NEVER depends on the LLM: it must be present in
// local_rules and fallback runs. ai.summary may add an advisory narrative on top.
//
// MaturityScore / MaturityGrade are a HEURISTIC governance-prioritization signal
// only — NOT an SLO, compliance score, or production-maturity certification. The
// formula is recorded in MaturityHeuristic (and in governance.score).
type GovernanceAssessment struct {
	InvalidRatio      float64          `json:"invalid_ratio"`
	TotalInvalid      int              `json:"total_invalid"`
	RiskDistribution  map[string]int   `json:"risk_distribution"`
	TopSystemicIssues []SystemicIssue  `json:"top_systemic_issues"`
	StoragePressure   *StoragePressure `json:"storage_pressure,omitempty"`

	MaturityScore     int    `json:"maturity_score"`
	MaturityGrade     string `json:"maturity_grade"`
	MaturityHeuristic string `json:"maturity_heuristic"`

	PrioritizedActions []string `json:"prioritized_actions"`
	RecommendedNorms   []string `json:"recommended_norms"`
}

// SystemicIssue rolls up one invalid_type across the batch: how many metrics it
// affects and the worst risk it reached.
type SystemicIssue struct {
	InvalidType string `json:"invalid_type"`
	MetricCount int    `json:"metric_count"`
	MaxRisk     string `json:"max_risk"`
	MaxScore    int    `json:"max_score"`
}

// StoragePressure is the governance-level rollup of summary.storage_impact (kept
// heuristic: estimated index entries, not TSDB bytes).
type StoragePressure struct {
	HighImpactMetrics            int `json:"high_impact_metrics"`
	MediumImpactMetrics          int `json:"medium_impact_metrics"`
	LowImpactMetrics             int `json:"low_impact_metrics"`
	EstimatedInvalidIndexEntries int `json:"estimated_invalid_index_entries"`
}
