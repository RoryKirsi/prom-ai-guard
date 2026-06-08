// Package doctor implements a focused, report-only single-metric/label/service
// diagnosis over an existing analysis_report.json. It never re-runs the scan,
// never calls an LLM, and never calls the Prometheus API. Because the report
// lists only invalid metrics, absence from it is never reported as "healthy".
package doctor

import (
	"encoding/json"
	"fmt"
	"sort"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/report"
	"prom-ai-guard/internal/rules"
)

// estimateNote documents that relabel_proposal_possible is derived from the
// report, not looked up in relabel_rules.yaml.
const estimateNote = "relabel_proposal_possible is an estimate derived from analysis_report.json (relabel_candidate + an actionable invalid_type); it is NOT a lookup of relabel_rules.yaml."

// Query holds the AND-combined selectors.
type Query struct {
	Metric  string `json:"metric,omitempty"`
	Label   string `json:"label,omitempty"`
	Service string `json:"service,omitempty"`
}

// Empty reports whether no selector was provided.
func (q Query) Empty() bool { return q.Metric == "" && q.Label == "" && q.Service == "" }

// ReportRef identifies the source report.
type ReportRef struct {
	ScanID     string `json:"scan_id"`
	ScanTime   string `json:"scan_time"`
	SourceType string `json:"source_type"`
}

// Diagnosis is the focused diagnosis for one matched invalid metric.
type Diagnosis struct {
	MetricName              string   `json:"metric_name"`
	RiskLevel               string   `json:"risk_level"`
	RiskScore               int      `json:"risk_score"`
	InvalidTypes            []string `json:"invalid_types"`
	RuleSignals             []string `json:"rule_signals"`
	RiskReason              string   `json:"risk_reason"`
	RootCause               string   `json:"root_cause"`
	Recommendations         []string `json:"recommendations"`
	Owner                   string   `json:"owner"`
	Service                 string   `json:"service"`
	Namespace               string   `json:"namespace"`
	RelabelCandidate        bool     `json:"relabel_candidate"`
	RelabelProposalPossible bool     `json:"relabel_proposal_possible"`
	MatchedLabels           []string `json:"matched_labels"`
}

// DoctorResult is the full diagnosis output.
type DoctorResult struct {
	SchemaVersion string      `json:"schema_version"`
	Query         Query       `json:"query"`
	Report        ReportRef   `json:"report"`
	MatchCount    int         `json:"match_count"`
	Matches       []Diagnosis `json:"matches"`
	Notes         []string    `json:"notes"`
}

// Diagnose filters the report's invalid metrics by the AND of the selectors and
// builds the diagnosis. It always produces a result (0 matches is valid).
func Diagnose(rep report.Report, q Query) DoctorResult {
	res := DoctorResult{
		SchemaVersion: "v1",
		Query:         q,
		Report:        ReportRef{ScanID: rep.ScanID, ScanTime: rep.ScanTime, SourceType: rep.Source.SourceType},
		Matches:       []Diagnosis{},
		Notes:         []string{},
	}
	for _, m := range rep.InvalidMetrics {
		if ok, labels := matchMetric(m, q); ok {
			res.Matches = append(res.Matches, toDiagnosis(m, labels))
		}
	}
	sortDiagnoses(res.Matches)
	res.MatchCount = len(res.Matches)

	if res.MatchCount > 0 {
		res.Notes = append(res.Notes, estimateNote)
	}
	// Absence is never "healthy": a named metric missing from invalid_metrics may
	// be valid OR unscanned — the report cannot tell.
	if q.Metric != "" && !presentInInvalids(rep, q.Metric) {
		res.Notes = append(res.Notes, fmt.Sprintf(
			"metric %q not found in invalid_metrics; cannot confirm healthy from this report — run scan/AI profile for full valid-metric inspection.", q.Metric))
	}
	if res.MatchCount == 0 {
		res.Notes = append(res.Notes, "no invalid metrics matched the selectors.")
	}
	return res
}

func matchMetric(m model.MetricAnalysis, q Query) (bool, []string) {
	if q.Metric != "" && m.MetricName != q.Metric {
		return false, nil
	}
	if q.Service != "" && m.Service != q.Service {
		return false, nil
	}
	var matched []string
	if q.Label != "" {
		if _, ok := m.LabelCardinality[q.Label]; !ok {
			return false, nil
		}
		matched = []string{q.Label}
	}
	return true, matched
}

func toDiagnosis(m model.MetricAnalysis, matchedLabels []string) Diagnosis {
	return Diagnosis{
		MetricName:              m.MetricName,
		RiskLevel:               m.RiskLevel,
		RiskScore:               m.RiskScore,
		InvalidTypes:            m.InvalidTypes,
		RuleSignals:             m.RuleSignals,
		RiskReason:              m.RiskReason,
		RootCause:               m.RootCause,
		Recommendations:         m.Recommendations,
		Owner:                   m.Owner,
		Service:                 m.Service,
		Namespace:               m.Namespace,
		RelabelCandidate:        m.RelabelCandidate,
		RelabelProposalPossible: relabelProposalPossible(m),
		MatchedLabels:           matchedLabels,
	}
}

// relabelProposalPossible is conservative: true only when the metric is a
// relabel_candidate AND carries an actionable invalid_type. It is false for
// duplicate_metric and orphan-only (and empty_label_value-only), mirroring the
// Slice 7 gating without claiming a rule actually exists.
func relabelProposalPossible(m model.MetricAnalysis) bool {
	if !m.RelabelCandidate {
		return false
	}
	actionable := map[string]bool{
		rules.TypeHighCardinality:  true,
		rules.TypeInvalidLabelName: true,
		rules.TypeDeprecated:       true,
		rules.TypeMeaningless:      true,
	}
	for _, t := range m.InvalidTypes {
		if actionable[t] {
			return true
		}
	}
	return false
}

func presentInInvalids(rep report.Report, name string) bool {
	for _, m := range rep.InvalidMetrics {
		if m.MetricName == name {
			return true
		}
	}
	return false
}

func sortDiagnoses(ds []Diagnosis) {
	sort.SliceStable(ds, func(i, j int) bool {
		if ds[i].RiskScore != ds[j].RiskScore {
			return ds[i].RiskScore > ds[j].RiskScore
		}
		return ds[i].MetricName < ds[j].MetricName
	})
}

// ValidateReport performs light, doctor-specific validation: schema_version must
// be v1, invalid_metrics must be a present array, and every invalid metric must
// have a non-empty metric_name. Summary fields are NOT required.
func ValidateReport(raw []byte) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return fmt.Errorf("report is not a JSON object: %w", err)
	}
	var sv string
	if r, ok := top["schema_version"]; !ok {
		return fmt.Errorf("missing schema_version")
	} else if err := json.Unmarshal(r, &sv); err != nil || sv != "v1" {
		return fmt.Errorf("unsupported schema_version %q (want v1)", sv)
	}
	imRaw, ok := top["invalid_metrics"]
	if !ok {
		return fmt.Errorf("missing invalid_metrics")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(imRaw, &items); err != nil {
		return fmt.Errorf("invalid_metrics is not an array")
	}
	for i, it := range items {
		var m struct {
			MetricName string `json:"metric_name"`
		}
		if err := json.Unmarshal(it, &m); err != nil {
			return fmt.Errorf("invalid_metrics[%d] is not an object", i)
		}
		if m.MetricName == "" {
			return fmt.Errorf("invalid_metrics[%d] has an empty metric_name", i)
		}
	}
	return nil
}
