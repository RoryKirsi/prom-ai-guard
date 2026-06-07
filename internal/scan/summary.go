// Package scan aggregates parsed MetricSeries into scan-level results.
//
// Slice 1 produces only the basic counts (total_series, total_metric_names).
// Later slices extend Summary with validity, risk distribution and invalid
// type counts; field names follow outputs/11-implementation-contracts.md §5.3.
package scan

import "prom-ai-guard/internal/model"

// Summary holds the Slice 1 subset of the contract summary block. Additional
// fields (valid/invalid counts, risk_distribution, invalid_type_counts) are
// added in later slices once rules exist.
type Summary struct {
	TotalSeries      int `json:"total_series"`
	TotalMetricNames int `json:"total_metric_names"`
}

// Summarize computes basic counts over the parsed series. total_metric_names is
// the number of distinct metric names; total_series is the sample count.
func Summarize(series []model.MetricSeries) Summary {
	names := make(map[string]struct{}, len(series))
	for _, s := range series {
		names[s.MetricName] = struct{}{}
	}
	return Summary{
		TotalSeries:      len(series),
		TotalMetricNames: len(names),
	}
}
