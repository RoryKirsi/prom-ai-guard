// Package scan aggregates parsed MetricSeries and rule results into the
// scan-level summary and the report's invalid/top lists.
//
// Field names follow outputs/11-implementation-contracts.md §5.3.
package scan

import (
	"math"
	"sort"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/rules"
)

// Summary holds the contract summary block.
type Summary struct {
	TotalSeries        int            `json:"total_series"`
	TotalMetricNames   int            `json:"total_metric_names"`
	ValidMetricNames   int            `json:"valid_metric_names"`
	InvalidMetricNames int            `json:"invalid_metric_names"`
	InvalidRatio       float64        `json:"invalid_ratio"`
	RiskDistribution   map[string]int `json:"risk_distribution"`
	InvalidTypeCounts  map[string]int `json:"invalid_type_counts"`
	// Slice 12 additive: aggregate TSDB-index storage-impact simulation. Nested
	// here (summary.storage_impact) rather than a new top-level report key.
	StorageImpact *model.StorageImpactSummary `json:"storage_impact,omitempty"`
}

// Result bundles everything the rule pass produces for the report.
type Result struct {
	Summary            Summary
	InvalidMetrics     []model.MetricAnalysis
	TopRiskMetrics     []model.RiskRef
	TopViolationLabels []model.LabelViolation
}

const topN = 20
const sampleMetricsPerLabel = 5

// allInvalidTypes lists every type so invalid_type_counts always contains all
// seven keys (even when zero), matching the contract example.
var allInvalidTypes = []string{
	rules.TypeDeprecated, rules.TypeDuplicate, rules.TypeEmptyLabelValue,
	rules.TypeInvalidLabelName, rules.TypeMeaningless, rules.TypeOrphan, rules.TypeHighCardinality,
}

// Assemble builds the summary and the invalid/top lists from rule output.
// totalSeries and totalMetricNames describe the whole scan; invalids and
// contribs come from rules.Evaluate.
func Assemble(totalSeries, totalMetricNames int, invalids []model.MetricAnalysis, contribs []model.LabelContribution) Result {
	riskDist := map[string]int{rules.RiskSevere: 0, rules.RiskWarning: 0, rules.RiskMinor: 0}
	typeCounts := map[string]int{}
	for _, t := range allInvalidTypes {
		typeCounts[t] = 0
	}
	for _, a := range invalids {
		riskDist[a.RiskLevel]++
		for _, t := range a.InvalidTypes {
			typeCounts[t]++
		}
	}

	invalidCount := len(invalids)
	summary := Summary{
		TotalSeries:        totalSeries,
		TotalMetricNames:   totalMetricNames,
		ValidMetricNames:   totalMetricNames - invalidCount,
		InvalidMetricNames: invalidCount,
		InvalidRatio:       ratio(invalidCount, totalMetricNames),
		RiskDistribution:   riskDist,
		InvalidTypeCounts:  typeCounts,
	}

	return Result{
		Summary:            summary,
		InvalidMetrics:     ensureSlice(invalids),
		TopRiskMetrics:     topRisk(invalids),
		TopViolationLabels: topViolationLabels(contribs),
	}
}

func ratio(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(float64(n)/float64(total)*10000) / 10000
}

// topRisk returns up to topN invalid metrics ordered by score desc, then name.
func topRisk(invalids []model.MetricAnalysis) []model.RiskRef {
	sorted := append([]model.MetricAnalysis{}, invalids...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].RiskScore != sorted[j].RiskScore {
			return sorted[i].RiskScore > sorted[j].RiskScore
		}
		return sorted[i].MetricName < sorted[j].MetricName
	})
	out := make([]model.RiskRef, 0, min(topN, len(sorted)))
	for i := 0; i < len(sorted) && i < topN; i++ {
		out = append(out, model.RiskRef{
			MetricName:   sorted[i].MetricName,
			RiskLevel:    sorted[i].RiskLevel,
			RiskScore:    sorted[i].RiskScore,
			InvalidTypes: sorted[i].InvalidTypes,
		})
	}
	return out
}

// topViolationLabels aggregates contributions by (label_key, invalid_type).
//
// This list is label-scoped by design: only the rule types that implicate a
// specific label (empty_label_value, invalid_label_name, high_cardinality)
// contribute. Whole-metric types (deprecated/duplicate/meaningless/orphan) have
// no offending label and appear in invalid_metrics / top_risk_metrics instead.
func topViolationLabels(contribs []model.LabelContribution) []model.LabelViolation {
	type key struct{ labelKey, invalidType string }
	type acc struct {
		metrics     map[string]struct{}
		seriesCount int
		maxScore    int
		samples     []string
	}
	groups := map[key]*acc{}
	for _, c := range contribs {
		k := key{c.LabelKey, c.InvalidType}
		g := groups[k]
		if g == nil {
			g = &acc{metrics: map[string]struct{}{}}
			groups[k] = g
		}
		if _, seen := g.metrics[c.MetricName]; !seen {
			g.metrics[c.MetricName] = struct{}{}
			g.seriesCount += c.SeriesCount
			if len(g.samples) < sampleMetricsPerLabel {
				g.samples = append(g.samples, c.MetricName)
			}
		}
		if c.RiskScore > g.maxScore {
			g.maxScore = c.RiskScore
		}
	}

	out := make([]model.LabelViolation, 0, len(groups))
	for k, g := range groups {
		sort.Strings(g.samples)
		out = append(out, model.LabelViolation{
			LabelKey:          k.labelKey,
			InvalidType:       k.invalidType,
			RiskLevel:         rules.RiskLevelFor(g.maxScore),
			RiskScore:         g.maxScore,
			MetricCount:       len(g.metrics),
			SeriesCount:       g.seriesCount,
			SampleMetricNames: g.samples,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RiskScore != out[j].RiskScore {
			return out[i].RiskScore > out[j].RiskScore
		}
		if out[i].MetricCount != out[j].MetricCount {
			return out[i].MetricCount > out[j].MetricCount
		}
		return out[i].LabelKey < out[j].LabelKey
	})
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}

func ensureSlice(in []model.MetricAnalysis) []model.MetricAnalysis {
	if in == nil {
		return []model.MetricAnalysis{}
	}
	return in
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
