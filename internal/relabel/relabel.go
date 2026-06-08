// Package relabel generates a Prometheus relabel-rule proposal from an existing
// analysis_report.json. It NEVER applies rules to Prometheus/Kubernetes/Helm or
// any live environment, never re-runs analysis, never calls an LLM, and never
// mutates the report — it only reads the report and writes relabel_rules.yaml.
//
// labeldrop is scope-wide: within a metric_relabel_configs block it drops every
// label whose name matches the regex across the whole scrape/relabel scope, not
// only the metrics our report flagged. So labeldrop rules are grouped per label
// key and carry explicit scope warnings; impact is a lower bound estimated from
// the metrics present in the report. Metric-level `drop` via __name__ is truly
// metric-scoped and reports an exact affected_series.
package relabel

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/report"
	"prom-ai-guard/internal/rules"
)

// Actions.
const (
	ActionLabelDrop = "labeldrop"
	ActionDrop      = "drop"
	ActionReview    = "review"
)

// Application scopes.
const (
	ScopeScrapeWide = "scrape_config_wide"
	ScopeMetric     = "metric_scoped"
	ScopeReview     = "review_only"
)

const (
	labeldropScopeWarning = "Prometheus labeldrop applies to all matching label names in the scrape/metric relabel scope."
	planNote              = "Proposal only — never applied by this tool. Apply via GitOps review."
	planScopeWarning      = "labeldrop rules are scrape/metric_relabel-scope-wide; impact is a lower bound estimated from metrics in analysis_report.json only."
	estimateBasis         = "metrics present in analysis_report.json only"
)

// MetricRelabelConfig is one Prometheus metric_relabel_configs entry.
type MetricRelabelConfig struct {
	SourceLabels []string `yaml:"source_labels,omitempty"`
	Regex        string   `yaml:"regex"`
	Action       string   `yaml:"action"`
}

// ExpectedImpact differs by action: labeldrop/review report triggered_series (a
// lower bound), labeldrop also sets actual_impact_may_be_larger; metric-scoped
// drop reports affected_series.
type ExpectedImpact struct {
	TriggeredSeries         *int   `yaml:"triggered_series,omitempty"`
	ActualImpactMayBeLarger *bool  `yaml:"actual_impact_may_be_larger,omitempty"`
	EstimateBasis           string `yaml:"estimate_basis,omitempty"`
	AffectedSeries          *int   `yaml:"affected_series,omitempty"`
	StorageReductionHint    string `yaml:"storage_reduction_hint"`
}

// Rule is one relabel proposal. Every rule has review_required=true.
type Rule struct {
	RuleID               string                `yaml:"rule_id"`
	Action               string                `yaml:"action"`
	LabelKey             string                `yaml:"label_key,omitempty"`
	MetricName           string                `yaml:"metric_name,omitempty"`
	InvalidTypes         []string              `yaml:"invalid_types"`
	RiskLevel            string                `yaml:"risk_level"`
	Reason               string                `yaml:"reason"`
	AffectedMetrics      []string              `yaml:"affected_metrics"`
	ApplicationScope     string                `yaml:"application_scope"`
	CopyPasteSafe        bool                  `yaml:"copy_paste_safe"`
	ReviewRequired       bool                  `yaml:"review_required"`
	ScopeWarning         string                `yaml:"scope_warning"`
	ExpectedImpact       ExpectedImpact        `yaml:"expected_impact"`
	MetricRelabelConfigs []MetricRelabelConfig `yaml:"metric_relabel_configs"`
}

// GeneratedFrom records the source report (no scan re-run).
type GeneratedFrom struct {
	Report string `yaml:"report"`
	ScanID string `yaml:"scan_id"`
}

// DryRunSummary is the top-level summary.
type DryRunSummary struct {
	TotalRules                 int            `yaml:"total_rules"`
	ByAction                   map[string]int `yaml:"by_action"`
	LabelsDropped              []string       `yaml:"labels_dropped"`
	MetricsCovered             int            `yaml:"metrics_covered"`
	EstimatedMinAffectedSeries int            `yaml:"estimated_min_affected_series"`
	Note                       string         `yaml:"note"`
	ScopeWarning               string         `yaml:"scope_warning"`
}

// RelabelPlan is the full relabel_rules.yaml document.
type RelabelPlan struct {
	SchemaVersion string        `yaml:"schema_version"`
	GeneratedFrom GeneratedFrom `yaml:"generated_from"`
	DryRunSummary DryRunSummary `yaml:"dry_run_summary"`
	Rules         []Rule        `yaml:"rules"`
}

// labeldrop accumulator per label key.
type ldGroup struct {
	metrics map[string]int // metric_name -> series_count (dedup)
	types   map[string]bool
	risk    int
}

// per-metric accumulators for drop and review.
type metricAcc struct {
	types       map[string]bool
	seriesCount int
	risk        string
}

// Generate builds the relabel proposal from the report. Actionable rules are
// emitted only when relabel_candidate is true AND a concrete rule-signal target
// exists; everything else becomes review-only.
func Generate(rep report.Report) RelabelPlan {
	dynamic := dynamicKeys(rep)

	ld := map[string]*ldGroup{}
	drop := map[string]*metricAcc{}
	review := map[string]*metricAcc{}

	for _, m := range rep.InvalidMetrics {
		for _, sig := range m.RuleSignals {
			kind, key := parseSignal(sig)
			switch kind {
			case "high_cardinality_label":
				routeLabel(m, key, rules.TypeHighCardinality, ld, review)
			case "invalid_name":
				routeLabel(m, key, rules.TypeInvalidLabelName, ld, review)
			case "empty_value":
				if dynamic[key] {
					routeLabel(m, key, rules.TypeEmptyLabelValue, ld, review)
				} else {
					addReview(review, m, rules.TypeEmptyLabelValue) // dropping normal labels removes context
				}
			case "deprecated":
				routeMetric(m, rules.TypeDeprecated, drop, review)
			case "meaningless":
				routeMetric(m, rules.TypeMeaningless, drop, review)
			case "duplicate":
				addReview(review, m, rules.TypeDuplicate)
			case "orphan":
				addReview(review, m, rules.TypeOrphan)
			case "high_cardinality_series":
				addReview(review, m, rules.TypeHighCardinality)
			}
		}
	}

	rulesOut := []Rule{}
	rulesOut = append(rulesOut, labeldropRules(ld)...)
	rulesOut = append(rulesOut, dropRules(drop)...)
	rulesOut = append(rulesOut, reviewRules(review)...)

	return RelabelPlan{
		SchemaVersion: "v1",
		GeneratedFrom: GeneratedFrom{Report: "analysis_report.json", ScanID: rep.ScanID},
		DryRunSummary: summarize(rulesOut),
		Rules:         rulesOut,
	}
}

// routeLabel sends a label-scoped finding to the labeldrop group when actionable
// (relabel_candidate), else to per-metric review.
func routeLabel(m model.MetricAnalysis, key, typ string, ld map[string]*ldGroup, review map[string]*metricAcc) {
	if !m.RelabelCandidate || key == "" {
		addReview(review, m, typ)
		return
	}
	g := ld[key]
	if g == nil {
		g = &ldGroup{metrics: map[string]int{}, types: map[string]bool{}}
		ld[key] = g
	}
	g.metrics[m.MetricName] = m.SeriesCount
	g.types[typ] = true
	if r := riskRank(m.RiskLevel); r > g.risk {
		g.risk = r
	}
}

// routeMetric sends a metric-scoped finding to the drop group when actionable.
func routeMetric(m model.MetricAnalysis, typ string, drop, review map[string]*metricAcc) {
	if !m.RelabelCandidate {
		addReview(review, m, typ)
		return
	}
	a := drop[m.MetricName]
	if a == nil {
		a = &metricAcc{types: map[string]bool{}, seriesCount: m.SeriesCount, risk: m.RiskLevel}
		drop[m.MetricName] = a
	}
	a.types[typ] = true
}

func addReview(review map[string]*metricAcc, m model.MetricAnalysis, typ string) {
	a := review[m.MetricName]
	if a == nil {
		a = &metricAcc{types: map[string]bool{}, seriesCount: m.SeriesCount, risk: m.RiskLevel}
		review[m.MetricName] = a
	}
	a.types[typ] = true
}

func labeldropRules(ld map[string]*ldGroup) []Rule {
	keys := sortedMapKeys(ld)
	out := make([]Rule, 0, len(keys))
	for _, key := range keys {
		g := ld[key]
		metrics := sortedIntMapKeys(g.metrics)
		triggered := 0
		for _, s := range g.metrics {
			triggered += s
		}
		risk := rankRisk(g.risk)
		t, larger := triggered, true
		out = append(out, Rule{
			RuleID:           "labeldrop_" + sanitizeID(key),
			Action:           ActionLabelDrop,
			LabelKey:         key,
			InvalidTypes:     sortedSet(g.types),
			RiskLevel:        risk,
			Reason:           fmt.Sprintf("labeldrop label %q: %s", key, strings.Join(sortedSet(g.types), ", ")),
			AffectedMetrics:  metrics,
			ApplicationScope: ScopeScrapeWide,
			CopyPasteSafe:    false,
			ReviewRequired:   true,
			ScopeWarning:     labeldropScopeWarning,
			ExpectedImpact: ExpectedImpact{
				TriggeredSeries: &t, ActualImpactMayBeLarger: &larger,
				EstimateBasis: estimateBasis, StorageReductionHint: hint(risk),
			},
			MetricRelabelConfigs: []MetricRelabelConfig{{
				Regex: regexp.QuoteMeta(key), Action: ActionLabelDrop,
			}},
		})
	}
	return out
}

func dropRules(drop map[string]*metricAcc) []Rule {
	names := sortedAccKeys(drop)
	out := make([]Rule, 0, len(names))
	for _, name := range names {
		a := drop[name]
		s := a.seriesCount
		out = append(out, Rule{
			RuleID:           "drop_metric_" + sanitizeID(name),
			Action:           ActionDrop,
			MetricName:       name,
			InvalidTypes:     sortedSet(a.types),
			RiskLevel:        a.risk,
			Reason:           fmt.Sprintf("drop metric %q: %s", name, strings.Join(sortedSet(a.types), ", ")),
			AffectedMetrics:  []string{name},
			ApplicationScope: ScopeMetric,
			CopyPasteSafe:    true, // __name__ regex is anchored/exact -> metric-scoped
			ReviewRequired:   true,
			ExpectedImpact:   ExpectedImpact{AffectedSeries: &s, StorageReductionHint: hint(a.risk)},
			MetricRelabelConfigs: []MetricRelabelConfig{{
				SourceLabels: []string{"__name__"}, Regex: regexp.QuoteMeta(name), Action: ActionDrop,
			}},
		})
	}
	return out
}

func reviewRules(review map[string]*metricAcc) []Rule {
	names := sortedAccKeys(review)
	out := make([]Rule, 0, len(names))
	for _, name := range names {
		a := review[name]
		s := a.seriesCount
		out = append(out, Rule{
			RuleID:               "review_" + sanitizeID(name),
			Action:               ActionReview,
			MetricName:           name,
			InvalidTypes:         sortedSet(a.types),
			RiskLevel:            a.risk,
			Reason:               fmt.Sprintf("review %q: %s — no safe automatic relabel", name, strings.Join(sortedSet(a.types), ", ")),
			AffectedMetrics:      []string{name},
			ApplicationScope:     ScopeReview,
			CopyPasteSafe:        false,
			ReviewRequired:       true,
			ExpectedImpact:       ExpectedImpact{TriggeredSeries: &s, StorageReductionHint: hint(a.risk)},
			MetricRelabelConfigs: []MetricRelabelConfig{},
		})
	}
	return out
}

func summarize(rs []Rule) DryRunSummary {
	by := map[string]int{}
	labels := map[string]bool{}
	metrics := map[string]bool{}
	minSeries := 0
	for _, r := range rs {
		by[r.Action]++
		if r.Action == ActionLabelDrop {
			labels[r.LabelKey] = true
			if r.ExpectedImpact.TriggeredSeries != nil {
				minSeries += *r.ExpectedImpact.TriggeredSeries
			}
		}
		if r.Action == ActionDrop && r.ExpectedImpact.AffectedSeries != nil {
			minSeries += *r.ExpectedImpact.AffectedSeries
		}
		for _, m := range r.AffectedMetrics {
			metrics[m] = true
		}
	}
	return DryRunSummary{
		TotalRules:                 len(rs),
		ByAction:                   by,
		LabelsDropped:              sortedSetBool(labels),
		MetricsCovered:             len(metrics),
		EstimatedMinAffectedSeries: minSeries,
		Note:                       planNote,
		ScopeWarning:               planScopeWarning,
	}
}

// dynamicKeys returns label keys that trigger high_cardinality anywhere in the
// report (treated as forbidden/dynamic for the empty_value decision).
func dynamicKeys(rep report.Report) map[string]bool {
	out := map[string]bool{}
	for _, m := range rep.InvalidMetrics {
		for _, sig := range m.RuleSignals {
			if kind, key := parseSignal(sig); kind == "high_cardinality_label" && key != "" {
				out[key] = true
			}
		}
	}
	return out
}

// parseSignal classifies a rule signal. Label-scoped signals tolerate ':' in the
// key (e.g. route:path) by stripping the known suffix.
func parseSignal(sig string) (kind, key string) {
	switch {
	case strings.HasPrefix(sig, "label:") && strings.HasSuffix(sig, ":high_cardinality"):
		return "high_cardinality_label", strings.TrimSuffix(strings.TrimPrefix(sig, "label:"), ":high_cardinality")
	case strings.HasPrefix(sig, "label:") && strings.HasSuffix(sig, ":invalid_name"):
		return "invalid_name", strings.TrimSuffix(strings.TrimPrefix(sig, "label:"), ":invalid_name")
	case strings.HasPrefix(sig, "label:") && strings.HasSuffix(sig, ":empty_value"):
		return "empty_value", strings.TrimSuffix(strings.TrimPrefix(sig, "label:"), ":empty_value")
	case sig == "metric:deprecated":
		return "deprecated", ""
	case sig == "metric:meaningless":
		return "meaningless", ""
	case sig == "metric:duplicate_series":
		return "duplicate", ""
	case sig == "service:orphan":
		return "orphan", ""
	case strings.HasPrefix(sig, "metric:series="):
		return "high_cardinality_series", ""
	}
	return "", ""
}

func hint(risk string) string {
	switch risk {
	case rules.RiskSevere:
		return "high"
	case rules.RiskWarning:
		return "medium"
	default:
		return "low"
	}
}

func riskRank(level string) int {
	switch level {
	case rules.RiskSevere:
		return 3
	case rules.RiskWarning:
		return 2
	case rules.RiskMinor:
		return 1
	}
	return 0
}

func rankRisk(rank int) string {
	switch rank {
	case 3:
		return rules.RiskSevere
	case 2:
		return rules.RiskWarning
	default:
		return rules.RiskMinor
	}
}

var idUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func sanitizeID(s string) string {
	return strings.Trim(idUnsafe.ReplaceAllString(s, "_"), "_")
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedSetBool(m map[string]bool) []string { return sortedSet(m) }

func sortedMapKeys(m map[string]*ldGroup) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedAccKeys(m map[string]*metricAcc) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedIntMapKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
