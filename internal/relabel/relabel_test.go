package relabel

import (
	"reflect"
	"testing"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/report"
)

// sample report exercising every routing path.
func sample() report.Report {
	return report.Report{
		ScanID: "scan-1",
		InvalidMetrics: []model.MetricAnalysis{
			{MetricName: "m_high", RuleSignals: []string{"label:user_id:high_cardinality"},
				RiskLevel: "severe", SeriesCount: 3, RelabelCandidate: true},
			{MetricName: "m_high2", RuleSignals: []string{"label:user_id:high_cardinality"},
				RiskLevel: "warning", SeriesCount: 5, RelabelCandidate: true},
			{MetricName: "m_userempty", RuleSignals: []string{"label:user_id:high_cardinality", "label:user_id:empty_value"},
				RiskLevel: "warning", SeriesCount: 2, RelabelCandidate: true},
			{MetricName: "m_invalidname", RuleSignals: []string{"label:route:path:invalid_name"},
				RiskLevel: "warning", SeriesCount: 1, RelabelCandidate: true},
			{MetricName: "m_deprecated", RuleSignals: []string{"metric:deprecated"},
				RiskLevel: "warning", SeriesCount: 1, RelabelCandidate: true},
			{MetricName: "m_meaningless", RuleSignals: []string{"metric:meaningless"},
				RiskLevel: "minor", SeriesCount: 1, RelabelCandidate: true},
			{MetricName: "m_emptynondyn", RuleSignals: []string{"label:env:empty_value"},
				RiskLevel: "warning", SeriesCount: 1, RelabelCandidate: true},
			{MetricName: "m_orphan", RuleSignals: []string{"service:orphan"},
				RiskLevel: "minor", SeriesCount: 1, RelabelCandidate: false},
			{MetricName: "m_dup", RuleSignals: []string{"metric:duplicate_series"},
				RiskLevel: "warning", SeriesCount: 2, RelabelCandidate: false},
		},
	}
}

func findRule(p RelabelPlan, id string) (Rule, bool) {
	for _, r := range p.Rules {
		if r.RuleID == id {
			return r, true
		}
	}
	return Rule{}, false
}

func TestLabeldropGroupedAcrossMetrics(t *testing.T) {
	p := Generate(sample())
	r, ok := findRule(p, "labeldrop_user_id")
	if !ok {
		t.Fatalf("expected labeldrop_user_id rule; rules=%v", ruleIDs(p))
	}
	if r.Action != ActionLabelDrop || r.LabelKey != "user_id" {
		t.Errorf("rule = %+v", r)
	}
	// grouped across all metrics that triggered user_id (incl. the empty_value dynamic case)
	wantMetrics := []string{"m_high", "m_high2", "m_userempty"}
	if !reflect.DeepEqual(r.AffectedMetrics, wantMetrics) {
		t.Errorf("affected_metrics = %v, want %v", r.AffectedMetrics, wantMetrics)
	}
	if r.RiskLevel != "severe" {
		t.Errorf("risk_level = %q, want severe (max across metrics)", r.RiskLevel)
	}
	// scope-wide warnings + impact semantics
	if r.ApplicationScope != ScopeScrapeWide || r.CopyPasteSafe || r.ScopeWarning == "" || !r.ReviewRequired {
		t.Errorf("labeldrop scope fields wrong: %+v", r)
	}
	if r.ExpectedImpact.TriggeredSeries == nil || *r.ExpectedImpact.TriggeredSeries != 10 {
		t.Errorf("triggered_series = %v, want 10", r.ExpectedImpact.TriggeredSeries)
	}
	if r.ExpectedImpact.ActualImpactMayBeLarger == nil || !*r.ExpectedImpact.ActualImpactMayBeLarger {
		t.Errorf("actual_impact_may_be_larger must be true for labeldrop")
	}
	if r.ExpectedImpact.AffectedSeries != nil {
		t.Errorf("labeldrop must NOT use affected_series")
	}
	if len(r.MetricRelabelConfigs) != 1 || r.MetricRelabelConfigs[0].Action != "labeldrop" ||
		r.MetricRelabelConfigs[0].Regex != "user_id" || len(r.MetricRelabelConfigs[0].SourceLabels) != 0 {
		t.Errorf("labeldrop config wrong: %+v", r.MetricRelabelConfigs)
	}
}

func TestInvalidLabelNameLabeldropWithColonKey(t *testing.T) {
	p := Generate(sample())
	r, ok := findRule(p, "labeldrop_route_path")
	if !ok {
		t.Fatalf("expected labeldrop_route_path; rules=%v", ruleIDs(p))
	}
	if r.LabelKey != "route:path" || r.MetricRelabelConfigs[0].Regex != `route:path` {
		t.Errorf("colon key handling wrong: %+v", r)
	}
}

func TestDropMetricScoped(t *testing.T) {
	p := Generate(sample())
	for _, id := range []string{"drop_metric_m_deprecated", "drop_metric_m_meaningless"} {
		r, ok := findRule(p, id)
		if !ok {
			t.Fatalf("expected %s; rules=%v", id, ruleIDs(p))
		}
		if r.Action != ActionDrop || r.ApplicationScope != ScopeMetric || !r.CopyPasteSafe || !r.ReviewRequired {
			t.Errorf("%s scope fields wrong: %+v", id, r)
		}
		if r.ExpectedImpact.AffectedSeries == nil || *r.ExpectedImpact.AffectedSeries != 1 {
			t.Errorf("%s must report affected_series=1, got %v", id, r.ExpectedImpact.AffectedSeries)
		}
		if r.ExpectedImpact.TriggeredSeries != nil {
			t.Errorf("%s metric-scoped drop must NOT use triggered_series", id)
		}
		c := r.MetricRelabelConfigs[0]
		if c.Action != "drop" || !reflect.DeepEqual(c.SourceLabels, []string{"__name__"}) {
			t.Errorf("%s drop config wrong: %+v", id, c)
		}
	}
}

func TestEmptyValueNonDynamicIsReview(t *testing.T) {
	p := Generate(sample())
	if _, ok := findRule(p, "labeldrop_env"); ok {
		t.Errorf("env (non-dynamic) must NOT be auto-labeldropped")
	}
	r, ok := findRule(p, "review_m_emptynondyn")
	if !ok || r.Action != ActionReview {
		t.Fatalf("m_emptynondyn must be review-only; rules=%v", ruleIDs(p))
	}
}

func TestReviewOnlyTypes(t *testing.T) {
	p := Generate(sample())
	for _, id := range []string{"review_m_orphan", "review_m_dup"} {
		r, ok := findRule(p, id)
		if !ok || r.Action != ActionReview || len(r.MetricRelabelConfigs) != 0 || !r.ReviewRequired {
			t.Errorf("%s must be review-only with no configs; got ok=%v rule=%+v", id, ok, r)
		}
	}
}

func TestGatingRequiresRelabelCandidate(t *testing.T) {
	// A high_cardinality label signal but relabel_candidate=false -> no actionable
	// labeldrop, becomes review instead.
	rep := report.Report{InvalidMetrics: []model.MetricAnalysis{
		{MetricName: "m_x", RuleSignals: []string{"label:foo:high_cardinality"},
			RiskLevel: "severe", SeriesCount: 1, RelabelCandidate: false},
	}}
	p := Generate(rep)
	if _, ok := findRule(p, "labeldrop_foo"); ok {
		t.Errorf("must not generate actionable labeldrop when relabel_candidate=false")
	}
	if r, ok := findRule(p, "review_m_x"); !ok || r.Action != ActionReview {
		t.Errorf("non-candidate finding must become review; rules=%v", ruleIDs(p))
	}
}

func TestEveryRuleReviewRequired(t *testing.T) {
	p := Generate(sample())
	if len(p.Rules) == 0 {
		t.Fatal("no rules generated")
	}
	for _, r := range p.Rules {
		if !r.ReviewRequired {
			t.Errorf("rule %s missing review_required=true", r.RuleID)
		}
	}
}

func TestDryRunSummary(t *testing.T) {
	p := Generate(sample()).DryRunSummary
	if p.ByAction[ActionLabelDrop] != 2 || p.ByAction[ActionDrop] != 2 || p.ByAction[ActionReview] != 3 {
		t.Errorf("by_action = %v", p.ByAction)
	}
	if p.TotalRules != 7 {
		t.Errorf("total_rules = %d, want 7", p.TotalRules)
	}
	if !reflect.DeepEqual(p.LabelsDropped, []string{"route:path", "user_id"}) {
		t.Errorf("labels_dropped = %v", p.LabelsDropped)
	}
	if p.Note == "" || p.ScopeWarning == "" {
		t.Errorf("summary note/scope_warning must be set")
	}
}

func TestDeterministic(t *testing.T) {
	a := Generate(sample())
	b := Generate(sample())
	if !reflect.DeepEqual(a, b) {
		t.Errorf("Generate is not deterministic")
	}
}

func ruleIDs(p RelabelPlan) []string {
	out := make([]string, 0, len(p.Rules))
	for _, r := range p.Rules {
		out = append(out, r.RuleID)
	}
	return out
}
