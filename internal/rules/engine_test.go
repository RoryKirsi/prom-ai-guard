package rules

import (
	"strings"
	"testing"

	"prom-ai-guard/internal/config"
	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/tsdb"
)

func testRules() config.Rules {
	var r config.Rules
	r.Thresholds.HighCardinalityLabelValues = 100
	r.Thresholds.HighCardinalityMetricSeries = 1000
	// Token-boundary patterns, matching the shipped configs/rules.yaml.
	r.Patterns.DeprecatedMetricNames = []string{"(^|[_:])deprecated([_:]|$)", "(^|[_:])legacy([_:]|$)"}
	r.Patterns.DebugMetricNames = []string{"(^|[_:])debug([_:]|$)", "(^|[_:])test([_:]|$)", "(^|[_:])temp([_:]|$)"}
	r.Patterns.ForbiddenLabelKeys = []string{"user_id", "session_id", "trace_id", "request_id", "password", "token"}
	return r
}

func testInventory() config.Inventory {
	return config.Inventory{
		Services: []config.Service{
			{Namespace: "orders", Service: "order-api", Jobs: []string{"order-api"}, Owner: "orders-team"},
			{Namespace: "payments", Service: "payment-api", Jobs: []string{"payment-api"}, Owner: "payments-team"},
		},
	}
}

// evalSeries is a helper: build stats from series and evaluate.
func evalSeries(t *testing.T, series ...model.MetricSeries) []model.MetricAnalysis {
	t.Helper()
	stats := tsdb.Build(series)
	analyses, _ := Evaluate(stats, testRules(), testInventory())
	return analyses
}

func find(analyses []model.MetricAnalysis, name string) (model.MetricAnalysis, bool) {
	for _, a := range analyses {
		if a.MetricName == name {
			return a, true
		}
	}
	return model.MetricAnalysis{}, false
}

func hasType(a model.MetricAnalysis, typ string) bool {
	for _, t := range a.InvalidTypes {
		if t == typ {
			return true
		}
	}
	return false
}

func TestDeprecatedMetric(t *testing.T) {
	a := mustFind(t, evalSeries(t,
		model.MetricSeries{MetricName: "order_legacy_latency_seconds", Labels: map[string]string{"service": "order-api"}, Value: 1},
	), "order_legacy_latency_seconds")
	if !hasType(a, TypeDeprecated) {
		t.Fatalf("expected deprecated, got %v", a.InvalidTypes)
	}
	if a.RiskLevel != RiskWarning {
		t.Errorf("risk level = %q, want warning", a.RiskLevel)
	}
}

func TestMeaninglessMetric(t *testing.T) {
	a := mustFind(t, evalSeries(t,
		model.MetricSeries{MetricName: "debug_trace_count", Labels: map[string]string{"service": "order-api"}, Value: 1},
	), "debug_trace_count")
	if !hasType(a, TypeMeaningless) {
		t.Fatalf("expected meaningless, got %v", a.InvalidTypes)
	}
	if a.RiskLevel != RiskMinor {
		t.Errorf("risk level = %q, want minor", a.RiskLevel)
	}
}

func TestEmptyLabelValueIsWarning(t *testing.T) {
	a := mustFind(t, evalSeries(t,
		model.MetricSeries{MetricName: "queue_depth", Labels: map[string]string{"service": "order-api", "env": ""}, Value: 0},
	), "queue_depth")
	if !hasType(a, TypeEmptyLabelValue) {
		t.Fatalf("expected empty_label_value, got %v", a.InvalidTypes)
	}
	// Adjustment: empty_label_value base risk is 55 -> warning.
	if a.RiskScore != 55 {
		t.Errorf("risk score = %d, want 55", a.RiskScore)
	}
	if a.RiskLevel != RiskWarning {
		t.Errorf("risk level = %q, want warning", a.RiskLevel)
	}
}

func TestInvalidLabelName(t *testing.T) {
	a := mustFind(t, evalSeries(t,
		model.MetricSeries{MetricName: "cache_hits_total", Labels: map[string]string{"service": "order-api", "route:path": "/a"}, Value: 1},
	), "cache_hits_total")
	if !hasType(a, TypeInvalidLabelName) {
		t.Fatalf("expected invalid_label_name, got %v", a.InvalidTypes)
	}
}

func TestDuplicateFingerprint(t *testing.T) {
	// Two identical series (same name + label set) -> fingerprint collision.
	a := mustFind(t, evalSeries(t,
		model.MetricSeries{MetricName: "dup_orders_total", Labels: map[string]string{"service": "order-api", "shard": "a"}, Value: 5},
		model.MetricSeries{MetricName: "dup_orders_total", Labels: map[string]string{"shard": "a", "service": "order-api"}, Value: 5},
	), "dup_orders_total")
	if !hasType(a, TypeDuplicate) {
		t.Fatalf("expected duplicate_metric, got %v", a.InvalidTypes)
	}
}

func TestDistinctLabelSetsAreNotDuplicate(t *testing.T) {
	analyses := evalSeries(t,
		model.MetricSeries{MetricName: "http_requests_total", Labels: map[string]string{"service": "order-api", "code": "200"}, Value: 1},
		model.MetricSeries{MetricName: "http_requests_total", Labels: map[string]string{"service": "order-api", "code": "500"}, Value: 1},
	)
	if a, ok := find(analyses, "http_requests_total"); ok && hasType(a, TypeDuplicate) {
		t.Fatalf("distinct label sets must not be duplicate: %v", a.InvalidTypes)
	}
}

func TestOrphanMetric(t *testing.T) {
	a := mustFind(t, evalSeries(t,
		model.MetricSeries{MetricName: "ghost_exporter_up", Labels: map[string]string{"service": "ghost-api"}, Value: 1},
	), "ghost_exporter_up")
	if !hasType(a, TypeOrphan) {
		t.Fatalf("expected orphan_metric, got %v", a.InvalidTypes)
	}
	if a.RiskLevel != RiskMinor {
		t.Errorf("risk level = %q, want minor", a.RiskLevel)
	}
}

func TestMetricWithoutServiceLabelIsNotOrphan(t *testing.T) {
	analyses := evalSeries(t,
		model.MetricSeries{MetricName: "process_cpu_seconds_total", Value: 1},
	)
	if a, ok := find(analyses, "process_cpu_seconds_total"); ok {
		t.Fatalf("system metric without service/job must not be invalid: %+v", a)
	}
}

func TestHighCardinalityForbiddenKey(t *testing.T) {
	a := mustFind(t, evalSeries(t,
		model.MetricSeries{MetricName: "http_user_requests_total", Labels: map[string]string{"service": "payment-api", "user_id": "u1"}, Value: 1},
		model.MetricSeries{MetricName: "http_user_requests_total", Labels: map[string]string{"service": "payment-api", "user_id": "u2"}, Value: 1},
	), "http_user_requests_total")
	if !hasType(a, TypeHighCardinality) {
		t.Fatalf("expected high_cardinality, got %v", a.InvalidTypes)
	}
	if a.RiskLevel != RiskSevere {
		t.Errorf("risk level = %q, want severe", a.RiskLevel)
	}
	if !a.RelabelCandidate {
		t.Errorf("high_cardinality should be a relabel candidate")
	}
}

func TestValidMetricIsExcluded(t *testing.T) {
	analyses := evalSeries(t,
		model.MetricSeries{MetricName: "http_requests_total", Labels: map[string]string{"service": "payment-api", "code": "200"}, Value: 1},
	)
	if _, ok := find(analyses, "http_requests_total"); ok {
		t.Fatalf("valid resolvable metric must not appear in invalid list")
	}
}

func TestConfidenceAlwaysOne(t *testing.T) {
	for _, a := range evalSeries(t,
		model.MetricSeries{MetricName: "order_legacy_latency_seconds", Labels: map[string]string{"service": "order-api"}, Value: 1},
	) {
		if a.Confidence != 1.0 {
			t.Errorf("%s confidence = %v, want 1.0", a.MetricName, a.Confidence)
		}
	}
}

func TestRiskScoreCombinesTypes(t *testing.T) {
	// Deprecated (base 50) + empty_label_value (base 55): max 55 + 5 = 60.
	a := mustFind(t, evalSeries(t,
		model.MetricSeries{MetricName: "legacy_queue_depth", Labels: map[string]string{"service": "order-api", "env": ""}, Value: 0},
	), "legacy_queue_depth")
	if !hasType(a, TypeDeprecated) || !hasType(a, TypeEmptyLabelValue) {
		t.Fatalf("expected both types, got %v", a.InvalidTypes)
	}
	if a.RiskScore != 60 {
		t.Errorf("risk score = %d, want 60", a.RiskScore)
	}
}

func TestRequiredFieldsPopulated(t *testing.T) {
	a := mustFind(t, evalSeries(t,
		model.MetricSeries{MetricName: "http_user_requests_total", Labels: map[string]string{"service": "payment-api", "user_id": "u1"}, Value: 1},
	), "http_user_requests_total")
	if !a.IsInvalid || a.RootCause == "" || len(a.Recommendations) == 0 || len(a.RuleSignals) == 0 {
		t.Errorf("required fields missing: %+v", a)
	}
	if a.Owner != "payments-team" || a.Service != "payment-api" || a.Namespace != "payments" {
		t.Errorf("inventory resolution wrong: owner=%q service=%q ns=%q", a.Owner, a.Service, a.Namespace)
	}
	if a.SeriesCount != 1 || a.LabelCardinality["user_id"] != 1 {
		t.Errorf("stats wrong: series=%d card=%v", a.SeriesCount, a.LabelCardinality)
	}
}

func TestRegexNoFalsePositives(t *testing.T) {
	// These legitimate names must NOT be flagged deprecated or meaningless by
	// the token-boundary patterns (they merely contain test/temp/legacy as
	// substrings inside latest/fastest/temperature/attempt).
	for _, name := range []string{
		"http_latest_total",
		"fastest_response_seconds",
		"node_temperature_celsius",
		"attempt_count",
	} {
		analyses := evalSeries(t,
			model.MetricSeries{MetricName: name, Labels: map[string]string{"service": "order-api"}, Value: 1},
		)
		if a, ok := find(analyses, name); ok {
			if hasType(a, TypeDeprecated) || hasType(a, TypeMeaningless) {
				t.Errorf("%s wrongly flagged: %v", name, a.InvalidTypes)
			}
		}
	}
}

func TestOrphanSignalOmitsRawValue(t *testing.T) {
	a := mustFind(t, evalSeries(t,
		model.MetricSeries{MetricName: "ghost_exporter_up", Labels: map[string]string{"service": "ghost-api"}, Value: 1},
	), "ghost_exporter_up")
	for _, sig := range a.RuleSignals {
		if strings.Contains(sig, "ghost-api") {
			t.Errorf("rule signal must not embed raw service value: %q", sig)
		}
	}
	found := false
	for _, sig := range a.RuleSignals {
		if sig == "service:orphan" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected signal 'service:orphan', got %v", a.RuleSignals)
	}
}

func TestOwnerContextConsistentForMultiService(t *testing.T) {
	// A metric carrying two service values where only the second resolves: the
	// owner/service/namespace triple must all come from the matched entry, never
	// pairing owner from one service with the name of another.
	inv := config.Inventory{Services: []config.Service{
		{Service: "zeta", Namespace: "ns-z", Owner: "team-z"},
	}}
	stats := tsdb.Build([]model.MetricSeries{
		{MetricName: "m", Labels: map[string]string{"service": "alpha"}, Value: 1},
		{MetricName: "m", Labels: map[string]string{"service": "zeta"}, Value: 1},
	})
	c := Contexts(stats, inv)["m"]
	if c.Owner != "team-z" || c.Service != "zeta" || c.Namespace != "ns-z" {
		t.Errorf("inconsistent owner context: %+v (must all derive from matched 'zeta')", c)
	}
}

func mustFind(t *testing.T, analyses []model.MetricAnalysis, name string) model.MetricAnalysis {
	t.Helper()
	a, ok := find(analyses, name)
	if !ok {
		t.Fatalf("metric %q not found in %d analyses", name, len(analyses))
	}
	return a
}
