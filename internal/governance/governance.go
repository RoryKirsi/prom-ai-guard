// Package governance builds a deterministic, batch-level monitoring-governance
// assessment from the scan result. It is pure (no I/O, no LLM) and is always
// produced — including local_rules and fallback runs — so gate/diff/doctor never
// depend on LLM prose. The LLM may only enrich the advisory ai.summary narrative.
package governance

import (
	"fmt"
	"math"
	"sort"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/rules"
)

// maturityHeuristic documents the scoring factors. The score is a governance
// PRIORITIZATION heuristic — not an SLO, compliance score, or production-maturity
// certification.
const maturityHeuristic = "Heuristic governance-prioritization signal only — NOT an SLO, compliance score, or production-maturity certification. " +
	"score = 100 − round(invalid_ratio×40) − 10×severe − 3×warning − 1×minor − 5×high_storage − 2×medium_storage, clamped to 0–100; " +
	"grade A≥90 B≥75 C≥60 D≥40 F<40."

// Assess computes the deterministic GovernanceAssessment. invalidRatio is the
// scan's invalid_ratio; storage may be nil (no storage pass).
func Assess(invalids []model.MetricAnalysis, invalidRatio float64, storage *model.StorageImpactSummary) model.GovernanceAssessment {
	riskDist := map[string]int{rules.RiskSevere: 0, rules.RiskWarning: 0, rules.RiskMinor: 0}
	type agg struct {
		count, maxScore int
		maxLevel        string
	}
	groups := map[string]*agg{}
	for _, m := range invalids {
		riskDist[m.RiskLevel]++
		for _, ty := range m.InvalidTypes {
			g := groups[ty]
			if g == nil {
				g = &agg{}
				groups[ty] = g
			}
			g.count++
			if m.RiskScore > g.maxScore {
				g.maxScore = m.RiskScore
				g.maxLevel = m.RiskLevel
			}
		}
	}

	// top_systemic_issues: max_score desc, count desc, type asc.
	issues := make([]model.SystemicIssue, 0, len(groups))
	for ty, g := range groups {
		level := g.maxLevel
		if level == "" {
			level = rules.RiskLevelFor(g.maxScore)
		}
		issues = append(issues, model.SystemicIssue{
			InvalidType: ty, MetricCount: g.count, MaxRisk: level, MaxScore: g.maxScore,
		})
	}
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].MaxScore != issues[j].MaxScore {
			return issues[i].MaxScore > issues[j].MaxScore
		}
		if issues[i].MetricCount != issues[j].MetricCount {
			return issues[i].MetricCount > issues[j].MetricCount
		}
		return issues[i].InvalidType < issues[j].InvalidType
	})

	present := make(map[string]bool, len(groups))
	for ty := range groups {
		present[ty] = true
	}
	g := model.GovernanceAssessment{
		InvalidRatio:       invalidRatio,
		TotalInvalid:       len(invalids),
		RiskDistribution:   riskDist,
		TopSystemicIssues:  issues,
		StoragePressure:    storagePressure(storage),
		MaturityHeuristic:  maturityHeuristic,
		PrioritizedActions: prioritizedActions(issues),
		RecommendedNorms:   recommendedNorms(present, storage),
	}
	g.MaturityScore = maturityScore(invalidRatio, riskDist, storage)
	g.MaturityGrade = grade(g.MaturityScore)
	return g
}

func storagePressure(s *model.StorageImpactSummary) *model.StoragePressure {
	if s == nil {
		return nil
	}
	return &model.StoragePressure{
		HighImpactMetrics:            s.HighImpactMetrics,
		MediumImpactMetrics:          s.MediumImpactMetrics,
		LowImpactMetrics:             s.LowImpactMetrics,
		EstimatedInvalidIndexEntries: s.EstimatedInvalidIndexEntries,
	}
}

// maturityScore is the documented heuristic (see maturityHeuristic), clamped 0–100.
func maturityScore(ratio float64, risk map[string]int, s *model.StorageImpactSummary) int {
	score := 100
	score -= int(math.Round(ratio * 40))
	score -= 10 * risk[rules.RiskSevere]
	score -= 3 * risk[rules.RiskWarning]
	score -= 1 * risk[rules.RiskMinor]
	if s != nil {
		score -= 5 * s.HighImpactMetrics
		score -= 2 * s.MediumImpactMetrics
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

func grade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 75:
		return "B"
	case score >= 60:
		return "C"
	case score >= 40:
		return "D"
	default:
		return "F"
	}
}

// actionTemplates: a concrete remediation per invalid type (%d = metric count).
var actionTemplates = map[string]string{
	rules.TypeHighCardinality:  "Reduce label cardinality on %d high-cardinality metric(s): drop identity labels (user_id/session_id/…) or use recording rules / logs.",
	rules.TypeDuplicate:        "Deduplicate %d metric(s): merge duplicate series and fix double scraping.",
	rules.TypeDeprecated:       "Remove %d deprecated/legacy metric(s), or add metric_relabel drop rules until removal.",
	rules.TypeMeaningless:      "Remove %d debug/test/temp metric(s) from production exposition.",
	rules.TypeInvalidLabelName: "Fix %d metric(s) with non-conforming label names ([a-zA-Z_][a-zA-Z0-9_]*).",
	rules.TypeEmptyLabelValue:  "Stop emitting empty label values on %d metric(s).",
	rules.TypeOrphan:           "Map %d orphan metric(s) to service_inventory.yaml (assign owner/service).",
}

func prioritizedActions(issues []model.SystemicIssue) []string {
	out := make([]string, 0, len(issues))
	for _, s := range issues {
		tmpl, ok := actionTemplates[s.InvalidType]
		if !ok {
			tmpl = "Remediate %d metric(s) of type " + s.InvalidType + "."
		}
		out = append(out, fmt.Sprintf(tmpl, s.MetricCount))
	}
	return out
}

// recommendedNorms is the AUTHORITATIVE, deterministic governance-norms source,
// selected by the invalid-type mix and storage pressure. The LLM does not author
// these. Order is fixed for determinism; duplicates are collapsed.
func recommendedNorms(present map[string]bool, storage *model.StorageImpactSummary) []string {
	has := func(t string) bool { return present[t] }
	var norms []string
	seen := map[string]struct{}{}
	add := func(s string) {
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		norms = append(norms, s)
	}
	if has(rules.TypeHighCardinality) {
		add("Set per-metric label-cardinality budgets; forbid identity labels (user_id/session_id/trace_id/request_id).")
	}
	if has(rules.TypeDeprecated) || has(rules.TypeMeaningless) {
		add("Enforce a metric naming convention; ban deprecated/legacy/debug/test/temp tokens in production.")
	}
	if has(rules.TypeInvalidLabelName) || has(rules.TypeEmptyLabelValue) {
		add("Validate label names ([a-zA-Z_][a-zA-Z0-9_]*) and forbid empty label values at instrumentation.")
	}
	if has(rules.TypeDuplicate) {
		add("Prevent duplicate series: one exporter per metric; avoid double scraping / merged jobs.")
	}
	if has(rules.TypeOrphan) {
		add("Require owner/service labels and map every scrape job in service_inventory.yaml.")
	}
	if storage != nil && (storage.HighImpactMetrics > 0 || storage.MediumImpactMetrics > 0) {
		add("Adopt TSDB storage optimization: recording rules for high-cardinality aggregates and cardinality-growth alerts.")
	}
	if len(present) > 0 {
		add("Review the generated relabel_rules.yaml via a GitOps PR before applying; gate CI on policy.yaml.")
	}
	return norms
}
