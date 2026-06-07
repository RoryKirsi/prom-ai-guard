// Package rules implements the deterministic local rule engine: the seven
// invalid-metric classes, a risk score and a risk level. It is the evidence and
// fallback layer; later slices let DeepSeek refine the textual fields.
package rules

import (
	"fmt"
	"regexp"
	"sort"

	"prom-ai-guard/internal/config"
	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/tsdb"
)

// Invalid-metric type identifiers (contract §5.3).
const (
	TypeDeprecated       = "deprecated_metric"
	TypeDuplicate        = "duplicate_metric"
	TypeEmptyLabelValue  = "empty_label_value"
	TypeInvalidLabelName = "invalid_label_name"
	TypeMeaningless      = "meaningless_metric"
	TypeOrphan           = "orphan_metric"
	TypeHighCardinality  = "high_cardinality"
)

// Risk levels (the only allowed values).
const (
	RiskSevere  = "severe"
	RiskWarning = "warning"
	RiskMinor   = "minor"
)

// baseScore is the per-type base risk contribution. empty_label_value is 55 so
// it maps to warning.
var baseScore = map[string]int{
	TypeHighCardinality:  90,
	TypeInvalidLabelName: 60,
	TypeDuplicate:        55,
	TypeEmptyLabelValue:  55,
	TypeDeprecated:       50,
	TypeOrphan:           35,
	TypeMeaningless:      30,
}

// relabelCandidateTypes are the invalid types that can be remediated by a
// metric_relabel drop/labeldrop rule.
var relabelCandidateTypes = map[string]bool{
	TypeHighCardinality:  true,
	TypeInvalidLabelName: true,
	TypeEmptyLabelValue:  true,
	TypeDeprecated:       true,
	TypeMeaningless:      true,
}

// validLabelName is the strict Prometheus label-name convention. The parser
// also accepts ':' in label keys, so non-conforming keys reach this rule.
var validLabelName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// Evaluate runs the rule engine over the aggregated stats and returns the
// invalid metrics (sorted by name) plus the label-scoped contributions used to
// build top_violation_labels. Valid metrics are omitted from the result.
func Evaluate(stats map[string]*tsdb.MetricStat, cfg config.Rules, inv config.Inventory) ([]model.MetricAnalysis, []model.LabelContribution) {
	deprecated := compile(cfg.Patterns.DeprecatedMetricNames)
	debug := compile(cfg.Patterns.DebugMetricNames)
	forbidden := toSet(cfg.Patterns.ForbiddenLabelKeys)
	index := buildInventoryIndex(inv)

	names := make([]string, 0, len(stats))
	for n := range stats {
		names = append(names, n)
	}
	sort.Strings(names)

	var (
		analyses []model.MetricAnalysis
		contribs []model.LabelContribution
	)
	for _, name := range names {
		a, cs := evaluateMetric(stats[name], cfg, deprecated, debug, forbidden, index)
		if a.IsInvalid {
			analyses = append(analyses, a)
			contribs = append(contribs, cs...)
		}
	}
	return analyses, contribs
}

func evaluateMetric(st *tsdb.MetricStat, cfg config.Rules, deprecated, debug []*regexp.Regexp, forbidden map[string]bool, index map[string]config.Service) (model.MetricAnalysis, []model.LabelContribution) {
	var (
		types    []string
		signals  []string
		contribs []model.LabelContribution
	)
	addLabelContrib := func(typ, key string) {
		contribs = append(contribs, model.LabelContribution{
			MetricName: st.MetricName, LabelKey: key, InvalidType: typ, SeriesCount: st.SeriesCount,
		})
	}

	// deprecated_metric
	if matchesAny(deprecated, st.MetricName) {
		types = append(types, TypeDeprecated)
		signals = append(signals, "metric:deprecated")
	}
	// meaningless_metric
	if matchesAny(debug, st.MetricName) {
		types = append(types, TypeMeaningless)
		signals = append(signals, "metric:meaningless")
	}
	// empty_label_value
	if keys := st.EmptyValueKeys(); len(keys) > 0 {
		types = append(types, TypeEmptyLabelValue)
		for _, k := range keys {
			signals = append(signals, fmt.Sprintf("label:%s:empty_value", k))
			addLabelContrib(TypeEmptyLabelValue, k)
		}
	}
	// invalid_label_name
	if keys := invalidLabelKeys(st.LabelKeys()); len(keys) > 0 {
		types = append(types, TypeInvalidLabelName)
		for _, k := range keys {
			signals = append(signals, fmt.Sprintf("label:%s:invalid_name", k))
			addLabelContrib(TypeInvalidLabelName, k)
		}
	}
	// duplicate_metric (fingerprint collision)
	if st.DuplicateFingerprint {
		types = append(types, TypeDuplicate)
		signals = append(signals, "metric:duplicate_series")
	}
	// high_cardinality
	if hcKeys, hcSeries := highCardinality(st, cfg, forbidden); len(hcKeys) > 0 || hcSeries {
		types = append(types, TypeHighCardinality)
		for _, k := range hcKeys {
			signals = append(signals, fmt.Sprintf("label:%s:high_cardinality", k))
			addLabelContrib(TypeHighCardinality, k)
		}
		if hcSeries {
			signals = append(signals, fmt.Sprintf("metric:series=%d", st.SeriesCount))
		}
	}
	// orphan_metric. The signal intentionally omits the raw service value so no
	// label value is embedded in rule_signals (the metric_name identifies it).
	matched, orphanVal := resolveOwner(st, index)
	if orphanVal != "" {
		types = append(types, TypeOrphan)
		signals = append(signals, "service:orphan")
	}

	a := model.MetricAnalysis{
		MetricName:       st.MetricName,
		SeriesCount:      st.SeriesCount,
		LabelCardinality: st.LabelCardinality(),
		Confidence:       1.0,
	}
	if len(types) == 0 {
		// Valid metric: still fill owner context, but mark not invalid.
		a.Owner, a.Service, a.Namespace = ownerContext(st, matched)
		return a, nil
	}

	score := riskScore(types)
	a.IsInvalid = true
	a.InvalidTypes = types
	a.RuleSignals = signals
	a.RiskScore = score
	a.RiskLevel = RiskLevelFor(score)
	a.RootCause = rootCause(types)
	a.Recommendations = recommendations(types)
	a.RelabelCandidate = isRelabelCandidate(types)
	a.Owner, a.Service, a.Namespace = ownerContext(st, matched)
	// Stamp the score onto each contribution for downstream aggregation.
	for i := range contribs {
		contribs[i].RiskScore = score
	}
	return a, contribs
}

// riskScore = max(base) + 5*(extraTypes), clamped to [0,100].
func riskScore(types []string) int {
	maxBase := 0
	for _, t := range types {
		if baseScore[t] > maxBase {
			maxBase = baseScore[t]
		}
	}
	score := maxBase + 5*(len(types)-1)
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}
	return score
}

// RiskLevelFor maps a risk score to one of the three allowed levels.
func RiskLevelFor(score int) string {
	switch {
	case score >= 80:
		return RiskSevere
	case score >= 50:
		return RiskWarning
	default:
		return RiskMinor
	}
}

func isRelabelCandidate(types []string) bool {
	for _, t := range types {
		if relabelCandidateTypes[t] {
			return true
		}
	}
	return false
}

func invalidLabelKeys(keys []string) []string {
	var out []string
	for _, k := range keys {
		if !validLabelName.MatchString(k) {
			out = append(out, k)
		}
	}
	return out
}

// highCardinality returns the offending label keys (forbidden or over-threshold
// value cardinality) and whether the metric's series count crosses the limit.
//
// The threshold comparison is `>=` by design: a metric that reaches exactly the
// configured limit is treated as high cardinality (the limit is the maximum
// acceptable value, not the first rejected one).
func highCardinality(st *tsdb.MetricStat, cfg config.Rules, forbidden map[string]bool) ([]string, bool) {
	var keys []string
	seen := map[string]bool{}
	for _, k := range st.LabelKeys() {
		over := cfg.Thresholds.HighCardinalityLabelValues > 0 && st.DistinctValues(k) >= cfg.Thresholds.HighCardinalityLabelValues
		if (forbidden[k] || over) && !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	series := cfg.Thresholds.HighCardinalityMetricSeries > 0 && st.SeriesCount >= cfg.Thresholds.HighCardinalityMetricSeries
	return keys, series
}

// resolveOwner finds the owning service. It returns the matched service (if any)
// and, when the metric carries a service/job label that resolves to nothing, the
// unmatched value that makes it an orphan.
func resolveOwner(st *tsdb.MetricStat, index map[string]config.Service) (*config.Service, string) {
	ids := append(append([]string{}, st.ServiceValues()...), st.JobValues()...)
	if len(ids) == 0 {
		return nil, "" // no service/job label: not attributable, not an orphan
	}
	for _, id := range ids {
		if svc, ok := index[id]; ok {
			svc := svc
			return &svc, ""
		}
	}
	return nil, ids[0]
}

// ownerContext returns a consistent owner/service/namespace triple. When the
// metric resolves to an inventory entry, all three come from that same matched
// entry so the service name is never paired with a different service's owner or
// namespace (e.g. for a metric carrying multiple service label values). When it
// does not resolve, the metric's own label values are used and owner is unknown.
func ownerContext(st *tsdb.MetricStat, matched *config.Service) (owner, service, namespace string) {
	if matched != nil {
		return matched.Owner, matched.Service, matched.Namespace
	}
	if svcs := st.ServiceValues(); len(svcs) > 0 {
		service = svcs[0]
	} else if jobs := st.JobValues(); len(jobs) > 0 {
		service = jobs[0]
	}
	if ns := st.NamespaceValues(); len(ns) > 0 {
		namespace = ns[0]
	}
	if service != "" {
		owner = "unknown"
	}
	return owner, service, namespace
}

func compile(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			out = append(out, re)
		}
	}
	return out
}

func matchesAny(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, it := range items {
		set[it] = true
	}
	return set
}

func buildInventoryIndex(inv config.Inventory) map[string]config.Service {
	index := map[string]config.Service{}
	for _, svc := range inv.Services {
		add := func(key string) {
			if key != "" {
				if _, exists := index[key]; !exists {
					index[key] = svc
				}
			}
		}
		add(svc.Service)
		for _, j := range svc.Jobs {
			add(j)
		}
		for _, a := range svc.Aliases {
			add(a)
		}
	}
	return index
}
