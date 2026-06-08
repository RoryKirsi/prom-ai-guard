package ai

import (
	"context"
	"fmt"
	"strings"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/profile"
	"prom-ai-guard/internal/rules"
)

// validInvalidTypes is the closed vocabulary the model may use.
var validInvalidTypes = map[string]bool{
	rules.TypeDeprecated:       true,
	rules.TypeDuplicate:        true,
	rules.TypeEmptyLabelValue:  true,
	rules.TypeInvalidLabelName: true,
	rules.TypeMeaningless:      true,
	rules.TypeOrphan:           true,
	rules.TypeHighCardinality:  true,
}

// Analyzer runs the AI analysis (or the disabled/fallback paths) and merges the
// result with the local rule baseline.
type Analyzer struct {
	Provider         string
	Model            string
	BaseURL          string
	Mode             string // ModeDeepSeekFullScan | ModeLocalRules
	Scope            string // ScopeAll | ScopeInvalid
	MaxAttempts      int
	MaxPayloadBytes  int
	ConfigHash       string
	RedactionEnabled bool
	KeyPresent       bool
	Completer        Completer // nil for local_rules
}

// Result carries the report's `ai` block and the merged invalid-metric set.
type Result struct {
	Info     Info
	Invalids []model.MetricAnalysis
}

// Run executes the analyzer over the (already redacted) profiles and the local
// rule invalids, returning the merged invalid set and the `ai` block.
func (a Analyzer) Run(ctx context.Context, scanID string, profiles []profile.MetricProfile, ruleInvalids []model.MetricAnalysis) Result {
	baseline, order := a.buildBaseline(profiles, ruleInvalids)
	inScope := a.inScopeNames(profiles, ruleInvalids)

	// Disabled: local_rules mode never calls the API.
	if a.Mode == ModeLocalRules {
		return a.result(baseline, order, StatusDisabled, MethodDisabled, "", 0, false, 0,
			localSummary(ruleInvalids), false)
	}

	// Enabled (deepseek_fullscan): decide fallback vs call.
	if !a.KeyPresent {
		return a.result(baseline, order, StatusFallback, MethodLocalRulesFallback,
			"missing DEEPSEEK_API_KEY", 0, true, 0, localSummary(ruleInvalids), true)
	}

	payload := buildPayload(scanID, inScopeProfiles(profiles, inScope))
	user, err := userPrompt(payload)
	if err != nil {
		return a.result(baseline, order, StatusFallback, MethodLocalRulesFallback,
			"payload_encode_error", 0, true, 0, localSummary(ruleInvalids), true)
	}
	if len(user) > a.MaxPayloadBytes {
		return a.result(baseline, order, StatusFallback, MethodLocalRulesFallback,
			"payload_too_large", 0, true, 0, localSummary(ruleInvalids), true)
	}

	resp, attempts, callErr := a.call(ctx, string(user))
	if callErr != nil {
		return a.result(baseline, order, StatusFallback, MethodLocalRulesFallback,
			categorize(callErr), attempts, true, 0, localSummary(ruleInvalids), true)
	}

	applied, status := a.apply(baseline, inScope, resp)
	summary := resp.Summary
	if strings.TrimSpace(summary) == "" {
		summary = localSummary(collectInvalids(baseline, order))
	}
	return a.result(baseline, order, status, MethodDeepSeekFullScan, "", attempts, false, applied, summary, true)
}

// call runs the retry loop (up to MaxAttempts). A single attempt covers one
// HTTP round trip plus JSON parse so invalid-JSON also triggers a retry.
func (a Analyzer) call(ctx context.Context, user string) (aiResponse, int, error) {
	maxAttempts := a.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	sys := systemPrompt()
	var lastErr error
	attempts := 0
	for attempts < maxAttempts {
		attempts++
		content, err := a.Completer.Complete(ctx, sys, user)
		if err != nil {
			lastErr = err
			continue
		}
		resp, perr := parseResponse(content)
		if perr != nil {
			lastErr = perr
			continue
		}
		return resp, attempts, nil
	}
	return aiResponse{}, attempts, lastErr
}

// apply merges valid AI entries into the baseline and decides success/partial.
func (a Analyzer) apply(baseline map[string]*model.MetricAnalysis, inScope []string, resp aiResponse) (analyzed int, status string) {
	// Deduplicate by metric_name. Prefer the first usable entry, and prefer an
	// is_invalid=true finding over a later is_invalid=false entry, so a duplicate
	// can never silently erase an earlier finding.
	byName := make(map[string]aiMetric, len(resp.Metrics))
	for _, m := range resp.Metrics {
		if ex, ok := byName[m.MetricName]; ok && (ex.IsInvalid || !m.IsInvalid) {
			continue
		}
		byName[m.MetricName] = m
	}

	applied := 0
	for _, name := range inScope {
		base := baseline[name]
		entry, ok := byName[name]
		if !ok {
			continue // missing entry -> not applied
		}
		if !entry.IsInvalid {
			applied++ // valid-and-confirmed counts as applied
			continue
		}
		validTypes := filterValidTypes(entry.InvalidTypes)
		if len(validTypes) == 0 {
			continue // is_invalid=true but no usable types -> dropped
		}
		mergeInto(base, entry, validTypes)
		applied++
	}

	if applied == len(inScope) {
		return applied, StatusSuccess
	}
	return applied, StatusPartial
}

// mergeInto applies an AI finding to the baseline analysis. The risk is
// re-scored from the union of rule + AI types, so severity can only rise.
func mergeInto(base *model.MetricAnalysis, entry aiMetric, aiTypes []string) {
	hadRuleFinding := len(base.InvalidTypes) > 0
	final := unionTypes(base.InvalidTypes, aiTypes)

	base.IsInvalid = true
	base.InvalidTypes = final
	base.RiskScore = rules.RiskScore(final)
	base.RiskLevel = rules.RiskLevelFor(base.RiskScore)
	base.RelabelCandidate = rules.IsRelabelCandidate(final)
	base.RiskReason = entry.RiskReason
	if strings.TrimSpace(entry.RootCause) != "" {
		base.RootCause = entry.RootCause
	}
	if len(entry.Recommendations) > 0 {
		base.Recommendations = entry.Recommendations
	}
	if entry.Confidence > 0 {
		base.Confidence = entry.Confidence
	}
	if hadRuleFinding {
		base.AnalysisSources = []string{SourceLocalRules, SourceLLM}
	} else {
		base.AnalysisSources = []string{SourceLLM}
	}
}

// buildBaseline constructs a per-metric analysis for every profile: rule
// invalids carry their rule analysis; everything else starts as a zero-finding
// baseline so AI can upgrade it. order preserves profile (name-sorted) order.
func (a Analyzer) buildBaseline(profiles []profile.MetricProfile, ruleInvalids []model.MetricAnalysis) (map[string]*model.MetricAnalysis, []string) {
	rule := make(map[string]model.MetricAnalysis, len(ruleInvalids))
	for _, r := range ruleInvalids {
		rule[r.MetricName] = r
	}
	baseline := make(map[string]*model.MetricAnalysis, len(profiles))
	order := make([]string, 0, len(profiles))
	for _, p := range profiles {
		order = append(order, p.MetricName)
		if r, ok := rule[p.MetricName]; ok {
			cp := r
			cp.AnalysisSources = []string{SourceLocalRules}
			baseline[p.MetricName] = &cp
			continue
		}
		baseline[p.MetricName] = &model.MetricAnalysis{
			MetricName:       p.MetricName,
			IsInvalid:        false,
			Owner:            p.Owner,
			Service:          p.Service,
			Namespace:        p.Namespace,
			SeriesCount:      p.SeriesCount,
			LabelCardinality: p.LabelCardinality,
			Confidence:       1.0,
		}
	}
	return baseline, order
}

func (a Analyzer) inScopeNames(profiles []profile.MetricProfile, ruleInvalids []model.MetricAnalysis) []string {
	if a.Scope == ScopeInvalid {
		rule := make(map[string]bool, len(ruleInvalids))
		for _, r := range ruleInvalids {
			rule[r.MetricName] = true
		}
		names := make([]string, 0, len(ruleInvalids))
		for _, p := range profiles {
			if rule[p.MetricName] {
				names = append(names, p.MetricName)
			}
		}
		return names
	}
	names := make([]string, 0, len(profiles))
	for _, p := range profiles {
		names = append(names, p.MetricName)
	}
	return names
}

// result assembles the final Result with the merged invalid set and Info block.
func (a Analyzer) result(baseline map[string]*model.MetricAnalysis, order []string,
	status, method, reason string, attempts int, fallbackUsed bool, analyzed int, summary string, enabled bool) Result {
	return Result{
		Info: Info{
			Provider:                    a.Provider,
			Model:                       a.Model,
			BaseURL:                     a.BaseURL,
			AIMode:                      a.Mode,
			AIScope:                     a.Scope,
			Enabled:                     enabled,
			Status:                      status,
			FallbackReason:              reason,
			AnalysisMethod:              method,
			AttemptCount:                attempts,
			FallbackUsed:                fallbackUsed,
			RedactionEnabled:            a.RedactionEnabled,
			AnalyzedMetricCount:         analyzed,
			Summary:                     summary,
			HistoricalComparisonSummary: "",
			ConfigHash:                  a.ConfigHash,
		},
		Invalids: collectInvalids(baseline, order),
	}
}

func collectInvalids(baseline map[string]*model.MetricAnalysis, order []string) []model.MetricAnalysis {
	out := make([]model.MetricAnalysis, 0)
	for _, name := range order {
		if b := baseline[name]; b != nil && b.IsInvalid {
			out = append(out, *b)
		}
	}
	return out
}

func inScopeProfiles(profiles []profile.MetricProfile, names []string) []profile.MetricProfile {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	out := make([]profile.MetricProfile, 0, len(names))
	for _, p := range profiles {
		if want[p.MetricName] {
			out = append(out, p)
		}
	}
	return out
}

func filterValidTypes(types []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range types {
		if validInvalidTypes[t] && !seen[t] {
			out = append(out, t)
			seen[t] = true
		}
	}
	return out
}

func unionTypes(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	seen := map[string]bool{}
	for _, t := range append(append([]string{}, a...), b...) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func localSummary(invalids []model.MetricAnalysis) string {
	return fmt.Sprintf("Local rule analysis: %d invalid metric(s).", len(invalids))
}

func categorize(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "status"):
		return "http_error"
	case strings.Contains(msg, "transport"):
		return "network_error"
	case strings.Contains(msg, "JSON"), strings.Contains(msg, "response"):
		return "invalid_response"
	default:
		return "request_failed"
	}
}
