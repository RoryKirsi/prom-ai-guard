// Package model holds the core data structures shared across slices.
//
// In Slice 1 only MetricSeries and the parse-level types are used. The structs
// here intentionally mirror the terminology in CONTEXT.md (MetricSeries,
// MetricName, LabelKey, LabelValue) so later slices can extend them without
// renaming.
package model

// MetricSeries is one parsed Prometheus time series sample: a metric name, its
// label set, and a value. It is the atomic unit produced by the parser and
// aggregated into per-metric profiles in later slices.
type MetricSeries struct {
	MetricName string            `json:"metric_name"`
	Labels     map[string]string `json:"labels,omitempty"`
	Value      float64           `json:"value"`
}

// ParseWarning records a non-fatal problem with a single input line. Per the
// contract, malformed lines must be reported but must not abort the scan.
type ParseWarning struct {
	Line   int    `json:"line"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}
