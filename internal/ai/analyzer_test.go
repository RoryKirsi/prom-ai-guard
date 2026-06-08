package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/profile"
)

type mockCompleter struct {
	fn       func(call int, user string) (string, error)
	calls    int
	lastUser string
}

func (m *mockCompleter) Complete(_ context.Context, _, user string) (string, error) {
	m.calls++
	m.lastUser = user
	return m.fn(m.calls, user)
}

func mockProfiles() []profile.MetricProfile {
	return []profile.MetricProfile{
		{
			MetricName: "clean_metric", SeriesCount: 1, LabelKeys: []string{"service"},
			LabelCardinality:  map[string]int{"service": 1},
			SampleLabelValues: map[string][]string{"service": {"order-api"}},
			Owner:             "orders", Service: "order-api", Namespace: "orders",
			RuleSignals: []string{}, InvalidTypes: []string{},
		},
		{
			MetricName: "http_user_requests_total", SeriesCount: 3, LabelKeys: []string{"service", "user_id"},
			LabelCardinality:  map[string]int{"service": 1, "user_id": 3},
			SampleLabelValues: map[string][]string{"service": {"payment-api"}, "user_id": {"<redacted>", "<redacted>", "<redacted>"}},
			Owner:             "platform", Service: "payment-api", Namespace: "payments",
			RuleSignals: []string{"label:user_id:high_cardinality"}, InvalidTypes: []string{"high_cardinality"},
		},
	}
}

func ruleInvalids() []model.MetricAnalysis {
	return []model.MetricAnalysis{{
		MetricName: "http_user_requests_total", IsInvalid: true, InvalidTypes: []string{"high_cardinality"},
		RiskLevel: "severe", RiskScore: 90, Confidence: 1.0,
		RuleSignals: []string{"label:user_id:high_cardinality"},
		RootCause:   "rule root cause", Recommendations: []string{"rule rec"},
		Owner: "platform", Service: "payment-api", Namespace: "payments", SeriesCount: 3,
		LabelCardinality: map[string]int{"service": 1, "user_id": 3},
	}}
}

func newAnalyzer(mode, scope string, comp Completer, keyPresent bool, maxPayload int) Analyzer {
	if maxPayload == 0 {
		maxPayload = 262144
	}
	return Analyzer{
		Provider: "deepseek", Model: "deepseek-v4-flash", BaseURL: "http://127.0.0.1:1",
		Mode: mode, Scope: scope, MaxAttempts: 2, MaxPayloadBytes: maxPayload,
		ConfigHash: "sha256:x", RedactionEnabled: true, KeyPresent: keyPresent, Completer: comp,
	}
}

func findInvalid(invs []model.MetricAnalysis, name string) (model.MetricAnalysis, bool) {
	for _, a := range invs {
		if a.MetricName == name {
			return a, true
		}
	}
	return model.MetricAnalysis{}, false
}

func TestRunSuccess(t *testing.T) {
	comp := &mockCompleter{fn: func(int, string) (string, error) {
		return `{"metrics":[
{"metric_name":"http_user_requests_total","is_invalid":true,"invalid_types":["high_cardinality"],"risk_level":"severe","risk_reason":"unbounded user_id","root_cause":"ai root cause","recommendations":["drop user_id"],"confidence":0.9},
{"metric_name":"clean_metric","is_invalid":false}
],"summary":"overall governance"}`, nil
	}}
	res := newAnalyzer(ModeDeepSeekFullScan, ScopeAll, comp, true, 0).Run(context.Background(), "scan1", mockProfiles(), ruleInvalids())

	if res.Info.Status != StatusSuccess {
		t.Fatalf("status = %q, want success", res.Info.Status)
	}
	if res.Info.AnalyzedMetricCount != 2 || res.Info.AttemptCount != 1 || res.Info.Summary != "overall governance" {
		t.Errorf("info = %+v", res.Info)
	}
	a, ok := findInvalid(res.Invalids, "http_user_requests_total")
	if !ok {
		t.Fatal("http_user_requests_total should be invalid")
	}
	if a.RiskReason != "unbounded user_id" || a.RootCause != "ai root cause" {
		t.Errorf("AI refinement missing: %+v", a)
	}
	if !reflectContains(a.AnalysisSources, SourceLocalRules) || !reflectContains(a.AnalysisSources, SourceLLM) {
		t.Errorf("analysis_sources = %v, want both", a.AnalysisSources)
	}
	if _, ok := findInvalid(res.Invalids, "clean_metric"); ok {
		t.Errorf("clean_metric must stay valid")
	}
}

func TestRunPartialMissingEntry(t *testing.T) {
	comp := &mockCompleter{fn: func(int, string) (string, error) {
		// only one of two in-scope metrics returned
		return `{"metrics":[{"metric_name":"http_user_requests_total","is_invalid":true,"invalid_types":["high_cardinality"],"risk_level":"severe","risk_reason":"r","root_cause":"rc","recommendations":["x"],"confidence":0.8}],"summary":"s"}`, nil
	}}
	res := newAnalyzer(ModeDeepSeekFullScan, ScopeAll, comp, true, 0).Run(context.Background(), "s", mockProfiles(), ruleInvalids())
	if res.Info.Status != StatusPartial {
		t.Fatalf("status = %q, want partial", res.Info.Status)
	}
	if res.Info.AnalyzedMetricCount != 1 {
		t.Errorf("analyzed = %d, want 1", res.Info.AnalyzedMetricCount)
	}
}

func TestRunPartialEmptyTypesDropped(t *testing.T) {
	comp := &mockCompleter{fn: func(int, string) (string, error) {
		return `{"metrics":[
{"metric_name":"http_user_requests_total","is_invalid":false},
{"metric_name":"clean_metric","is_invalid":true,"invalid_types":[]}
],"summary":"s"}`, nil
	}}
	res := newAnalyzer(ModeDeepSeekFullScan, ScopeAll, comp, true, 0).Run(context.Background(), "s", mockProfiles(), ruleInvalids())
	// clean_metric entry is dropped (is_invalid true but no types) -> partial.
	if res.Info.Status != StatusPartial {
		t.Fatalf("status = %q, want partial", res.Info.Status)
	}
	// http_user_requests_total said is_invalid=false by AI, but rule baseline keeps it invalid.
	if _, ok := findInvalid(res.Invalids, "http_user_requests_total"); !ok {
		t.Errorf("rule invalid must persist even if AI says is_invalid=false")
	}
}

func TestRunTwoFailuresFallback(t *testing.T) {
	comp := &mockCompleter{fn: func(int, string) (string, error) {
		return "", errors.New("llm request failed: status 500")
	}}
	res := newAnalyzer(ModeDeepSeekFullScan, ScopeAll, comp, true, 0).Run(context.Background(), "s", mockProfiles(), ruleInvalids())
	if res.Info.Status != StatusFallback || res.Info.AttemptCount != 2 || !res.Info.FallbackUsed {
		t.Fatalf("info = %+v", res.Info)
	}
	if res.Info.FallbackReason == "" || res.Info.AnalysisMethod != MethodLocalRulesFallback {
		t.Errorf("fallback fields = %+v", res.Info)
	}
	a, ok := findInvalid(res.Invalids, "http_user_requests_total")
	if !ok || a.RootCause != "rule root cause" {
		t.Errorf("rule baseline must be preserved on fallback: %+v", a)
	}
	if !reflectEqual(a.AnalysisSources, []string{SourceLocalRules}) {
		t.Errorf("sources = %v, want [local_rules]", a.AnalysisSources)
	}
}

func TestRunMalformedTwiceFallback(t *testing.T) {
	comp := &mockCompleter{fn: func(int, string) (string, error) { return "not json at all", nil }}
	res := newAnalyzer(ModeDeepSeekFullScan, ScopeAll, comp, true, 0).Run(context.Background(), "s", mockProfiles(), ruleInvalids())
	if res.Info.Status != StatusFallback || res.Info.AttemptCount != 2 {
		t.Fatalf("info = %+v", res.Info)
	}
}

func TestRunPayloadTooLarge(t *testing.T) {
	comp := &mockCompleter{fn: func(int, string) (string, error) { return `{"metrics":[]}`, nil }}
	res := newAnalyzer(ModeDeepSeekFullScan, ScopeAll, comp, true, 10).Run(context.Background(), "s", mockProfiles(), ruleInvalids())
	if res.Info.Status != StatusFallback || res.Info.FallbackReason != "payload_too_large" {
		t.Fatalf("info = %+v", res.Info)
	}
	if res.Info.AttemptCount != 0 || comp.calls != 0 {
		t.Errorf("must not call AI when payload too large: attempts=%d calls=%d", res.Info.AttemptCount, comp.calls)
	}
}

func TestRunDisabledLocalRules(t *testing.T) {
	comp := &mockCompleter{fn: func(int, string) (string, error) { t.Fatal("must not call AI"); return "", nil }}
	res := newAnalyzer(ModeLocalRules, ScopeAll, comp, true, 0).Run(context.Background(), "s", mockProfiles(), ruleInvalids())
	if res.Info.Status != StatusDisabled || res.Info.Enabled {
		t.Fatalf("info = %+v", res.Info)
	}
	if comp.calls != 0 {
		t.Errorf("AI must not be called in local_rules mode")
	}
	a, _ := findInvalid(res.Invalids, "http_user_requests_total")
	if !reflectEqual(a.AnalysisSources, []string{SourceLocalRules}) {
		t.Errorf("sources = %v", a.AnalysisSources)
	}
}

func TestRunMissingKeyFallback(t *testing.T) {
	comp := &mockCompleter{fn: func(int, string) (string, error) { t.Fatal("must not call AI"); return "", nil }}
	res := newAnalyzer(ModeDeepSeekFullScan, ScopeAll, comp, false, 0).Run(context.Background(), "s", mockProfiles(), ruleInvalids())
	if res.Info.Status != StatusFallback || res.Info.FallbackReason != "missing DEEPSEEK_API_KEY" || res.Info.AttemptCount != 0 {
		t.Fatalf("info = %+v", res.Info)
	}
}

func TestRunAIAddsFindingToValidMetric(t *testing.T) {
	comp := &mockCompleter{fn: func(int, string) (string, error) {
		return `{"metrics":[
{"metric_name":"http_user_requests_total","is_invalid":true,"invalid_types":["high_cardinality"],"risk_level":"severe","risk_reason":"r","root_cause":"rc","recommendations":["x"],"confidence":0.9},
{"metric_name":"clean_metric","is_invalid":true,"invalid_types":["deprecated_metric"],"risk_level":"warning","risk_reason":"legacy naming","root_cause":"ai found legacy","recommendations":["rename"],"confidence":0.7}
],"summary":"s"}`, nil
	}}
	res := newAnalyzer(ModeDeepSeekFullScan, ScopeAll, comp, true, 0).Run(context.Background(), "s", mockProfiles(), ruleInvalids())
	if res.Info.Status != StatusSuccess {
		t.Fatalf("status = %q", res.Info.Status)
	}
	a, ok := findInvalid(res.Invalids, "clean_metric")
	if !ok {
		t.Fatal("AI should have added clean_metric as invalid")
	}
	if !reflectEqual(a.AnalysisSources, []string{SourceLLM}) {
		t.Errorf("AI-only finding sources = %v, want [llm]", a.AnalysisSources)
	}
	if a.RiskLevel != "warning" || a.RiskScore != 50 {
		t.Errorf("AI-added deprecated should score 50/warning, got %d/%s", a.RiskScore, a.RiskLevel)
	}
	// deprecated_metric is relabel-eligible; relabel_candidate must be recomputed.
	if !a.RelabelCandidate {
		t.Errorf("AI-added deprecated_metric should set relabel_candidate=true")
	}
}

func TestRunDuplicateEntryDoesNotEraseFinding(t *testing.T) {
	// AI returns clean_metric twice: a valid confirmation, then a real finding.
	comp := &mockCompleter{fn: func(int, string) (string, error) {
		return `{"metrics":[
{"metric_name":"http_user_requests_total","is_invalid":false},
{"metric_name":"clean_metric","is_invalid":false},
{"metric_name":"clean_metric","is_invalid":true,"invalid_types":["deprecated_metric"],"risk_level":"warning","risk_reason":"r","root_cause":"rc","recommendations":["x"],"confidence":0.8}
],"summary":"s"}`, nil
	}}
	res := newAnalyzer(ModeDeepSeekFullScan, ScopeAll, comp, true, 0).Run(context.Background(), "s", mockProfiles(), ruleInvalids())
	if _, ok := findInvalid(res.Invalids, "clean_metric"); !ok {
		t.Errorf("a later duplicate must not erase the AI finding for clean_metric")
	}
}

func TestRunNeverDowngradesSevere(t *testing.T) {
	// AI tries to reclassify a severe high_cardinality metric as minor meaningless.
	comp := &mockCompleter{fn: func(int, string) (string, error) {
		return `{"metrics":[
{"metric_name":"http_user_requests_total","is_invalid":true,"invalid_types":["meaningless_metric"],"risk_level":"minor","risk_reason":"r","root_cause":"rc","recommendations":["x"],"confidence":0.9},
{"metric_name":"clean_metric","is_invalid":false}
],"summary":"s"}`, nil
	}}
	res := newAnalyzer(ModeDeepSeekFullScan, ScopeAll, comp, true, 0).Run(context.Background(), "s", mockProfiles(), ruleInvalids())
	a, _ := findInvalid(res.Invalids, "http_user_requests_total")
	if a.RiskLevel != "severe" {
		t.Errorf("severity downgraded to %q; rule severe must be a floor", a.RiskLevel)
	}
	// union keeps high_cardinality and adds meaningless_metric
	if !reflectContains(a.InvalidTypes, "high_cardinality") || !reflectContains(a.InvalidTypes, "meaningless_metric") {
		t.Errorf("types = %v, want union", a.InvalidTypes)
	}
}

func TestRunPayloadHasNoRawSensitiveValue(t *testing.T) {
	comp := &mockCompleter{fn: func(int, string) (string, error) { return `{"metrics":[],"summary":"s"}`, nil }}
	_ = newAnalyzer(ModeDeepSeekFullScan, ScopeAll, comp, true, 0).Run(context.Background(), "s", mockProfiles(), ruleInvalids())
	// profiles are pre-redacted; the outbound payload must contain only placeholders.
	if strings.Contains(comp.lastUser, "u1") || strings.Contains(comp.lastUser, "u2") {
		t.Errorf("raw sensitive value present in outbound payload")
	}
	if !strings.Contains(comp.lastUser, "<redacted>") {
		t.Errorf("expected redacted placeholders in payload")
	}
}

func reflectContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func reflectEqual(a, b []string) bool {
	d, _ := json.Marshal(a)
	e, _ := json.Marshal(b)
	return string(d) == string(e)
}
