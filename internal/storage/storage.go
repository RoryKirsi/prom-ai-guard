// Package storage implements a deterministic TSDB-index storage-impact
// simulation for invalid metrics. It works only from the in-memory
// model.MetricAnalysis (series_count + label_cardinality); it NEVER reads real
// Prometheus TSDB blocks/WAL/chunks, calls the Prometheus API, or calls an LLM.
//
// estimated_index_entries is a HEURISTIC proxy for inverted-index postings, not
// real TSDB bytes, and no disk-size reduction is implied.
package storage

import (
	"fmt"
	"sort"

	"prom-ai-guard/internal/model"
)

const (
	heuristicNote = "estimated_index_entries is a heuristic proxy for inverted-index postings, not real TSDB bytes; no disk-size guarantee."
	scopeNote     = "computed for invalid metrics only; valid-metric storage impact is out of scope (analysis_report.json does not retain full valid-metric detail)."

	topLabelsN  = 3
	topMetricsN = 5
)

// Thresholds tune the impact_level classification. Demo-scaled defaults apply to
// any non-positive field via Resolve.
type Thresholds struct {
	HighIndexEntries       int
	MediumIndexEntries     int
	HighLabelCardinality   int
	MediumLabelCardinality int
}

// DefaultThresholds returns the first-version/demo-friendly defaults. They are
// intentionally low so small fixtures can show high/medium/low; raise them for
// production-scale Prometheus.
func DefaultThresholds() Thresholds {
	return Thresholds{
		HighIndexEntries:       200,
		MediumIndexEntries:     50,
		HighLabelCardinality:   20,
		MediumLabelCardinality: 5,
	}
}

// Resolve fills any non-positive threshold with its default.
func Resolve(t Thresholds) Thresholds {
	d := DefaultThresholds()
	if t.HighIndexEntries <= 0 {
		t.HighIndexEntries = d.HighIndexEntries
	}
	if t.MediumIndexEntries <= 0 {
		t.MediumIndexEntries = d.MediumIndexEntries
	}
	if t.HighLabelCardinality <= 0 {
		t.HighLabelCardinality = d.HighLabelCardinality
	}
	if t.MediumLabelCardinality <= 0 {
		t.MediumLabelCardinality = d.MediumLabelCardinality
	}
	return t
}

// Annotate computes the storage impact for each invalid metric (mutating the
// slice in place) and returns the aggregate summary. Invalid metrics only.
func Annotate(metrics []model.MetricAnalysis, t Thresholds) model.StorageImpactSummary {
	t = Resolve(t)
	summary := model.StorageImpactSummary{
		TopStorageImpactMetrics: []model.StorageImpactRef{},
		Heuristic:               heuristicNote,
		ScopeNote:               scopeNote,
	}
	refs := make([]model.StorageImpactRef, 0, len(metrics))
	for i := range metrics {
		si := Compute(metrics[i], t)
		metrics[i].StorageImpact = &si
		// TSDB storage-optimization recommendation: appended (never replacing
		// existing recommendations) only when storage impact or high cardinality
		// warrants it. Heuristic — index entries, not bytes.
		if rec := storageRecommendation(metrics[i], si); rec != "" {
			metrics[i].Recommendations = append(metrics[i].Recommendations, rec)
		}
		switch si.ImpactLevel {
		case "high":
			summary.HighImpactMetrics++
		case "medium":
			summary.MediumImpactMetrics++
		default:
			summary.LowImpactMetrics++
		}
		summary.EstimatedInvalidSeries += si.SeriesCount
		summary.EstimatedInvalidIndexEntries += si.EstimatedIndexEntries
		refs = append(refs, model.StorageImpactRef{
			MetricName:            metrics[i].MetricName,
			EstimatedIndexEntries: si.EstimatedIndexEntries,
			ImpactLevel:           si.ImpactLevel,
		})
	}
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].EstimatedIndexEntries != refs[j].EstimatedIndexEntries {
			return refs[i].EstimatedIndexEntries > refs[j].EstimatedIndexEntries
		}
		return refs[i].MetricName < refs[j].MetricName
	})
	if len(refs) > topMetricsN {
		refs = refs[:topMetricsN]
	}
	summary.TopStorageImpactMetrics = refs
	return summary
}

// storageRecommendation returns a concrete TSDB storage-optimization
// recommendation when the metric's impact is high/medium OR it is flagged
// high_cardinality; otherwise "". Heuristic: index entries, not disk bytes.
func storageRecommendation(m model.MetricAnalysis, si model.StorageImpact) string {
	warranted := si.ImpactLevel == "high" || si.ImpactLevel == "medium"
	for _, t := range m.InvalidTypes {
		if t == "high_cardinality" {
			warranted = true
		}
	}
	if !warranted {
		return ""
	}
	topLabel, topCard := "-", 0
	if len(si.TopCardinalityLabels) > 0 {
		topLabel = si.TopCardinalityLabels[0].LabelKey
		topCard = si.TopCardinalityLabels[0].Cardinality
	}
	return fmt.Sprintf("TSDB storage optimization: reduce label %q (%d distinct) — ~%d estimated index entries (heuristic); use recording rules or drop high-cardinality labels via metric_relabel_configs.", topLabel, topCard, si.EstimatedIndexEntries)
}

// Compute builds the per-metric storage impact.
//
//	estimated_index_entries = series_count*(label_count+1) + sum(label_cardinality)
//
// impact_level = the higher of the index-entries signal and the
// max-label-cardinality signal, so an unbounded label is "high" even before the
// series count explodes.
func Compute(m model.MetricAnalysis, t Thresholds) model.StorageImpact {
	t = Resolve(t)
	labelCount := len(m.LabelCardinality)
	maxCard, sumCard := 0, 0
	for _, c := range m.LabelCardinality {
		if c > maxCard {
			maxCard = c
		}
		sumCard += c
	}
	est := m.SeriesCount*(labelCount+1) + sumCard

	byEntries := levelByEntries(est, t)
	byCard := levelByCard(maxCard, t)
	level := higher(byEntries, byCard)

	return model.StorageImpact{
		SeriesCount:           m.SeriesCount,
		LabelCount:            labelCount,
		MaxLabelCardinality:   maxCard,
		TopCardinalityLabels:  topLabels(m.LabelCardinality, topLabelsN),
		EstimatedIndexEntries: est,
		ImpactLevel:           level,
		Reason:                reason(level, byEntries, byCard, est, maxCard, m.SeriesCount, labelCount, m.LabelCardinality),
	}
}

func levelByEntries(est int, t Thresholds) string {
	switch {
	case est >= t.HighIndexEntries:
		return "high"
	case est >= t.MediumIndexEntries:
		return "medium"
	default:
		return "low"
	}
}

func levelByCard(maxCard int, t Thresholds) string {
	switch {
	case maxCard >= t.HighLabelCardinality:
		return "high"
	case maxCard >= t.MediumLabelCardinality:
		return "medium"
	default:
		return "low"
	}
}

func rank(level string) int {
	switch level {
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}

func higher(a, b string) string {
	if rank(a) >= rank(b) {
		return a
	}
	return b
}

// reason names the deciding factor for the chosen level.
func reason(level, byEntries, byCard string, est, maxCard, series, labelCount int, card map[string]int) string {
	cardDrives := rank(byCard) >= rank(byEntries)
	if level == "low" {
		return fmt.Sprintf("low estimated storage impact (≈%d index entries, max label cardinality %d)", est, maxCard)
	}
	if cardDrives && maxCard > 0 {
		return fmt.Sprintf("%s-cardinality label %q (%d distinct values)", level, topLabelKey(card), maxCard)
	}
	return fmt.Sprintf("%s estimated index entries ≈%d from %d series × %d labels", level, est, series, labelCount)
}

func topLabelKey(card map[string]int) string {
	top := topLabels(card, 1)
	if len(top) == 0 {
		return "-"
	}
	return top[0].LabelKey
}

// topLabels returns the n highest-cardinality labels (cardinality desc, key asc).
func topLabels(card map[string]int, n int) []model.LabelCardinalityRef {
	refs := make([]model.LabelCardinalityRef, 0, len(card))
	for k, c := range card {
		refs = append(refs, model.LabelCardinalityRef{LabelKey: k, Cardinality: c})
	}
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].Cardinality != refs[j].Cardinality {
			return refs[i].Cardinality > refs[j].Cardinality
		}
		return refs[i].LabelKey < refs[j].LabelKey
	})
	if len(refs) > n {
		refs = refs[:n]
	}
	return refs
}
