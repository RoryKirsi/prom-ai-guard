package diff

import (
	"sort"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/report"
	"prom-ai-guard/internal/scan"
)

// Provenance identifies one of the compared reports.
type Provenance struct {
	ScanID      string `json:"scan_id"`
	ScanTime    string `json:"scan_time"`
	ToolVersion string `json:"tool_version"`
	ConfigHash  string `json:"config_hash"`
	SourceType  string `json:"source_type"`
}

// Delta is an integer before/after pair with its signed change.
type Delta struct {
	Previous int `json:"previous"`
	Current  int `json:"current"`
	Change   int `json:"change"`
}

// RatioDelta is a float before/after pair with its signed change.
type RatioDelta struct {
	Previous float64 `json:"previous"`
	Current  float64 `json:"current"`
	Change   float64 `json:"change"`
}

// SummaryDelta captures the report-summary movement. All fields are strictly
// required by ValidateReport, so none is a misleading 0-default.
type SummaryDelta struct {
	InvalidMetricNames Delta      `json:"invalid_metric_names"`
	TotalMetricNames   Delta      `json:"total_metric_names"`
	Severe             Delta      `json:"severe"`
	Warning            Delta      `json:"warning"`
	Minor              Delta      `json:"minor"`
	InvalidRatio       RatioDelta `json:"invalid_ratio"`
}

// MetricDiff describes one metric across the two reports. Absent-on-a-side fields
// are zero/empty (e.g. previous_* is empty for an added metric).
type MetricDiff struct {
	MetricName           string   `json:"metric_name"`
	PreviousRiskLevel    string   `json:"previous_risk_level"`
	CurrentRiskLevel     string   `json:"current_risk_level"`
	PreviousRiskScore    int      `json:"previous_risk_score"`
	CurrentRiskScore     int      `json:"current_risk_score"`
	PreviousInvalidTypes []string `json:"previous_invalid_types"`
	CurrentInvalidTypes  []string `json:"current_invalid_types"`
	Owner                string   `json:"owner"`
	Service              string   `json:"service"`
	Namespace            string   `json:"namespace"`
}

// TypeChange is the invalid_types set delta for a still-invalid metric.
type TypeChange struct {
	MetricName   string   `json:"metric_name"`
	AddedTypes   []string `json:"added_types"`
	RemovedTypes []string `json:"removed_types"`
}

// DiffResult is the full deterministic diff. RiskIncreased/RiskDecreased and
// TypeChanges are overlapping subsets of StillInvalid.
type DiffResult struct {
	SchemaVersion   string       `json:"schema_version"`
	Previous        Provenance   `json:"previous"`
	Current         Provenance   `json:"current"`
	ConfigChanged   bool         `json:"config_changed"`
	SummaryDelta    SummaryDelta `json:"summary_delta"`
	AddedInvalid    []MetricDiff `json:"added_invalid"`
	ResolvedInvalid []MetricDiff `json:"resolved_invalid"`
	StillInvalid    []MetricDiff `json:"still_invalid"`
	RiskIncreased   []MetricDiff `json:"risk_increased"`
	RiskDecreased   []MetricDiff `json:"risk_decreased"`
	TypeChanges     []TypeChange `json:"type_changes"`
}

// Compute builds the deterministic diff. Metric identity is metric_name, assumed
// unique within each report (guaranteed by ValidateReport before this is called).
func Compute(previous, current report.Report) DiffResult {
	prevByName := indexByName(previous.InvalidMetrics)
	currByName := indexByName(current.InvalidMetrics)

	added := []MetricDiff{}
	resolved := []MetricDiff{}
	still := []MetricDiff{}
	riskUp := []MetricDiff{}
	riskDown := []MetricDiff{}
	typeChanges := []TypeChange{}

	for name, c := range currByName {
		p, inPrev := prevByName[name]
		if !inPrev {
			added = append(added, addedDiff(c))
			continue
		}
		md := bothDiff(p, c)
		still = append(still, md)
		if c.RiskScore > p.RiskScore {
			riskUp = append(riskUp, md)
		} else if c.RiskScore < p.RiskScore {
			riskDown = append(riskDown, md)
		}
		if add, rem := typeDelta(p.InvalidTypes, c.InvalidTypes); len(add) > 0 || len(rem) > 0 {
			typeChanges = append(typeChanges, TypeChange{MetricName: name, AddedTypes: add, RemovedTypes: rem})
		}
	}
	for name, p := range prevByName {
		if _, inCurr := currByName[name]; !inCurr {
			resolved = append(resolved, resolvedDiff(p))
		}
	}

	sortDiffs(added)
	sortDiffs(resolved)
	sortDiffs(still)
	sortDiffs(riskUp)
	sortDiffs(riskDown)
	sort.Slice(typeChanges, func(i, j int) bool { return typeChanges[i].MetricName < typeChanges[j].MetricName })

	return DiffResult{
		SchemaVersion:   "v1",
		Previous:        provenanceOf(previous),
		Current:         provenanceOf(current),
		ConfigChanged:   previous.ConfigHash != current.ConfigHash,
		SummaryDelta:    summaryDelta(previous.Summary, current.Summary),
		AddedInvalid:    added,
		ResolvedInvalid: resolved,
		StillInvalid:    still,
		RiskIncreased:   riskUp,
		RiskDecreased:   riskDown,
		TypeChanges:     typeChanges,
	}
}

func indexByName(metrics []model.MetricAnalysis) map[string]model.MetricAnalysis {
	out := make(map[string]model.MetricAnalysis, len(metrics))
	for _, m := range metrics {
		out[m.MetricName] = m
	}
	return out
}

func addedDiff(c model.MetricAnalysis) MetricDiff {
	return MetricDiff{
		MetricName:          c.MetricName,
		CurrentRiskLevel:    c.RiskLevel,
		CurrentRiskScore:    c.RiskScore,
		CurrentInvalidTypes: c.InvalidTypes,
		Owner:               c.Owner,
		Service:             c.Service,
		Namespace:           c.Namespace,
	}
}

func resolvedDiff(p model.MetricAnalysis) MetricDiff {
	return MetricDiff{
		MetricName:           p.MetricName,
		PreviousRiskLevel:    p.RiskLevel,
		PreviousRiskScore:    p.RiskScore,
		PreviousInvalidTypes: p.InvalidTypes,
		Owner:                p.Owner,
		Service:              p.Service,
		Namespace:            p.Namespace,
	}
}

func bothDiff(p, c model.MetricAnalysis) MetricDiff {
	return MetricDiff{
		MetricName:           c.MetricName,
		PreviousRiskLevel:    p.RiskLevel,
		CurrentRiskLevel:     c.RiskLevel,
		PreviousRiskScore:    p.RiskScore,
		CurrentRiskScore:     c.RiskScore,
		PreviousInvalidTypes: p.InvalidTypes,
		CurrentInvalidTypes:  c.InvalidTypes,
		Owner:                orElse(c.Owner, p.Owner),
		Service:              orElse(c.Service, p.Service),
		Namespace:            orElse(c.Namespace, p.Namespace),
	}
}

func orElse(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

// typeDelta returns the sorted added/removed invalid_types between previous and
// current.
func typeDelta(previous, current []string) (added, removed []string) {
	prev := toSet(previous)
	curr := toSet(current)
	for t := range curr {
		if !prev[t] {
			added = append(added, t)
		}
	}
	for t := range prev {
		if !curr[t] {
			removed = append(removed, t)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func summaryDelta(p, c scan.Summary) SummaryDelta {
	return SummaryDelta{
		InvalidMetricNames: intDelta(p.InvalidMetricNames, c.InvalidMetricNames),
		TotalMetricNames:   intDelta(p.TotalMetricNames, c.TotalMetricNames),
		Severe:             intDelta(p.RiskDistribution["severe"], c.RiskDistribution["severe"]),
		Warning:            intDelta(p.RiskDistribution["warning"], c.RiskDistribution["warning"]),
		Minor:              intDelta(p.RiskDistribution["minor"], c.RiskDistribution["minor"]),
		InvalidRatio:       RatioDelta{Previous: p.InvalidRatio, Current: c.InvalidRatio, Change: c.InvalidRatio - p.InvalidRatio},
	}
}

func intDelta(prev, curr int) Delta {
	return Delta{Previous: prev, Current: curr, Change: curr - prev}
}

func provenanceOf(r report.Report) Provenance {
	return Provenance{
		ScanID:      r.ScanID,
		ScanTime:    r.ScanTime,
		ToolVersion: r.ToolVersion,
		ConfigHash:  r.ConfigHash,
		SourceType:  r.Source.SourceType,
	}
}

// sortDiffs orders by the higher of the two risk scores (desc), then name (asc),
// for a stable, severity-first presentation across every bucket.
func sortDiffs(ds []MetricDiff) {
	sort.Slice(ds, func(i, j int) bool {
		ki, kj := maxInt(ds[i].PreviousRiskScore, ds[i].CurrentRiskScore), maxInt(ds[j].PreviousRiskScore, ds[j].CurrentRiskScore)
		if ki != kj {
			return ki > kj
		}
		return ds[i].MetricName < ds[j].MetricName
	})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
