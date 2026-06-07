// Package profile builds MetricProfile, the per-metric AI input unit. A profile
// aggregates the TSDB label model (series count, cardinality, sample values),
// the inventory context (owner/service/namespace/jobs) and the rule signals.
// It carries raw values; redaction happens in a separate pass before the
// profile is written to the AI input preview.
package profile

import (
	"sort"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/tsdb"
)

// MetricProfile is the AI analysis input unit for one metric name.
type MetricProfile struct {
	MetricName        string              `json:"metric_name"`
	SeriesCount       int                 `json:"series_count"`
	LabelKeys         []string            `json:"label_keys"`
	LabelCardinality  map[string]int      `json:"label_cardinality"`
	SampleLabelValues map[string][]string `json:"sample_label_values"`
	Owner             string              `json:"owner"`
	Service           string              `json:"service"`
	Namespace         string              `json:"namespace"`
	Jobs              []string            `json:"jobs"`
	RuleSignals       []string            `json:"rule_signals"`
	InvalidTypes      []string            `json:"invalid_types"`
}

// Build assembles one profile per metric name, sorted ascending. analyses maps
// metric name to its rule analysis (present only for invalid metrics); ctx maps
// metric name to its ownership context. sampleLimit bounds sample values per
// label key.
func Build(stats map[string]*tsdb.MetricStat, analyses map[string]model.MetricAnalysis, ctx map[string]model.MetricContext, sampleLimit int) []MetricProfile {
	names := make([]string, 0, len(stats))
	for n := range stats {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]MetricProfile, 0, len(names))
	for _, name := range names {
		st := stats[name]
		keys := st.LabelKeys()
		samples := make(map[string][]string, len(keys))
		for _, k := range keys {
			samples[k] = st.SampleValues(k, sampleLimit)
		}
		c := ctx[name]
		a := analyses[name]
		out = append(out, MetricProfile{
			MetricName:        name,
			SeriesCount:       st.SeriesCount,
			LabelKeys:         keys,
			LabelCardinality:  st.LabelCardinality(),
			SampleLabelValues: samples,
			Owner:             c.Owner,
			Service:           c.Service,
			Namespace:         c.Namespace,
			Jobs:              c.Jobs,
			RuleSignals:       orEmpty(a.RuleSignals),
			InvalidTypes:      orEmpty(a.InvalidTypes),
		})
	}
	return out
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
