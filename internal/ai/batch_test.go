package ai

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/profile"
)

// genProfiles returns n locally-valid profiles named metric_000..metric_NNN.
func genProfiles(n int) []profile.MetricProfile {
	out := make([]profile.MetricProfile, n)
	for i := 0; i < n; i++ {
		out[i] = profile.MetricProfile{
			MetricName: fmt.Sprintf("metric_%03d", i), SeriesCount: 1, LabelKeys: []string{"service"},
			LabelCardinality:  map[string]int{"service": 1},
			SampleLabelValues: map[string][]string{"service": {"svc"}},
			Owner:             "o", Service: "svc", Namespace: "ns",
			RuleSignals: []string{}, InvalidTypes: []string{},
		}
	}
	return out
}

var metricNameRe = regexp.MustCompile(`"metric_name":"([^"]+)"`)

func metricsInPrompt(user string) []string {
	var out []string
	for _, m := range metricNameRe.FindAllStringSubmatch(user, -1) {
		out = append(out, m[1])
	}
	return out
}

// echoValid returns a parseable response marking each name is_invalid:false.
func echoValid(names []string) string {
	items := make([]string, len(names))
	for i, n := range names {
		items[i] = fmt.Sprintf(`{"metric_name":%q,"is_invalid":false}`, n)
	}
	return fmt.Sprintf(`{"metrics":[%s],"summary":"ok"}`, strings.Join(items, ","))
}

func batchAnalyzer(comp Completer, batchSize, maxPayload int) Analyzer {
	a := newAnalyzer(ModeLLMFullScan, ScopeAll, comp, true, maxPayload)
	a.BatchSize = batchSize
	return a
}

func TestRunBatching101ProfilesThreeCalls(t *testing.T) {
	comp := &mockCompleter{fn: func(_ int, user string) (string, error) {
		return echoValid(metricsInPrompt(user)), nil
	}}
	res := batchAnalyzer(comp, 50, 0).Run(context.Background(), "s", genProfiles(101), nil)

	if comp.calls != 3 {
		t.Errorf("LLM calls = %d, want 3 (50/50/1)", comp.calls)
	}
	if res.Info.Status != StatusSuccess {
		t.Errorf("status = %q, want success", res.Info.Status)
	}
	if res.Info.BatchSize != 50 || res.Info.BatchCount != 3 {
		t.Errorf("batch_size/count = %d/%d, want 50/3", res.Info.BatchSize, res.Info.BatchCount)
	}
	if res.Info.SuccessfulBatches != 3 || res.Info.FailedBatches != 0 {
		t.Errorf("successful/failed = %d/%d, want 3/0", res.Info.SuccessfulBatches, res.Info.FailedBatches)
	}
	if res.Info.LLMInScopeMetricCount != 101 || res.Info.AnalyzedMetricCount != 101 {
		t.Errorf("in_scope/analyzed = %d/%d, want 101/101", res.Info.LLMInScopeMetricCount, res.Info.AnalyzedMetricCount)
	}
	if res.Info.PartialFallbackUsed || res.Info.FallbackUsed {
		t.Errorf("no fallback expected on full success")
	}
}

func TestRunBatchingOneBatchFailsPartial(t *testing.T) {
	comp := &mockCompleter{fn: func(_ int, user string) (string, error) {
		for _, n := range metricsInPrompt(user) {
			if n == "metric_050" { // batch index 1 (050..099)
				return "", errors.New("boom")
			}
		}
		return echoValid(metricsInPrompt(user)), nil
	}}
	res := batchAnalyzer(comp, 50, 0).Run(context.Background(), "s", genProfiles(101), nil)

	if res.Info.Status != StatusPartial {
		t.Fatalf("status = %q, want partial", res.Info.Status)
	}
	if res.Info.SuccessfulBatches != 2 || res.Info.FailedBatches != 1 {
		t.Errorf("successful/failed = %d/%d, want 2/1", res.Info.SuccessfulBatches, res.Info.FailedBatches)
	}
	if !res.Info.PartialFallbackUsed {
		t.Errorf("partial_fallback_used must be true when a batch failed")
	}
	if res.Info.FallbackUsed {
		t.Errorf("fallback_used must stay false on partial (full-fallback only)")
	}
	if len(res.Info.BatchFailures) != 1 {
		t.Fatalf("batch_failures = %v, want 1", res.Info.BatchFailures)
	}
	bf := res.Info.BatchFailures[0]
	if bf.BatchIndex != 1 || bf.MetricCount != 50 {
		t.Errorf("batch_failure = %+v, want index 1 metric_count 50", bf)
	}
	if bf.Reason != "request_failed" {
		t.Errorf("reason = %q, want safe category request_failed", bf.Reason)
	}
	if strings.Contains(bf.Reason, "boom") {
		t.Errorf("reason must not leak the raw error/body: %q", bf.Reason)
	}
}

func TestRunBatchingAllFailFallback(t *testing.T) {
	comp := &mockCompleter{fn: func(_ int, _ string) (string, error) { return "", errors.New("down") }}
	res := batchAnalyzer(comp, 50, 0).Run(context.Background(), "s", genProfiles(101), nil)

	if res.Info.Status != StatusFallback {
		t.Fatalf("status = %q, want fallback", res.Info.Status)
	}
	if !res.Info.FallbackUsed {
		t.Errorf("fallback_used must be true on full fallback")
	}
	if res.Info.PartialFallbackUsed {
		t.Errorf("partial_fallback_used must be false on full fallback")
	}
	if res.Info.AnalyzedMetricCount != 0 {
		t.Errorf("analyzed = %d, want 0", res.Info.AnalyzedMetricCount)
	}
	if res.Info.SuccessfulBatches != 0 || res.Info.FailedBatches != 3 {
		t.Errorf("successful/failed = %d/%d, want 0/3", res.Info.SuccessfulBatches, res.Info.FailedBatches)
	}
}

func TestRunBatchingNoDowngradeAcrossBatches(t *testing.T) {
	// metric_050 is a SEVERE local finding; its batch's AI says minor meaningless.
	ri := []model.MetricAnalysis{{
		MetricName: "metric_050", IsInvalid: true, InvalidTypes: []string{"high_cardinality"},
		RiskLevel: "severe", RiskScore: 90, Confidence: 1.0,
		RuleSignals: []string{"label:user_id:high_cardinality"}, LabelCardinality: map[string]int{"user_id": 3},
	}}
	comp := &mockCompleter{fn: func(_ int, user string) (string, error) {
		var items []string
		for _, n := range metricsInPrompt(user) {
			if n == "metric_050" {
				items = append(items, `{"metric_name":"metric_050","is_invalid":true,"invalid_types":["meaningless_metric"],"risk_level":"minor","risk_reason":"r","root_cause":"rc","recommendations":["x"],"confidence":0.5}`)
			} else {
				items = append(items, fmt.Sprintf(`{"metric_name":%q,"is_invalid":false}`, n))
			}
		}
		return fmt.Sprintf(`{"metrics":[%s],"summary":"ok"}`, strings.Join(items, ",")), nil
	}}
	res := batchAnalyzer(comp, 50, 0).Run(context.Background(), "s", genProfiles(101), ri)

	m, ok := findInvalid(res.Invalids, "metric_050")
	if !ok || m.RiskLevel != "severe" {
		t.Fatalf("severe local finding downgraded: ok=%v level=%q", ok, m.RiskLevel)
	}
	if !reflectContains(m.InvalidTypes, "high_cardinality") || !reflectContains(m.InvalidTypes, "meaningless_metric") {
		t.Errorf("types = %v, want union of rule + AI", m.InvalidTypes)
	}
}

func TestRunBatchingAIOnlyFindingFromLaterBatch(t *testing.T) {
	// metric_100 is in the LAST batch (index 2) and locally valid; AI flags it.
	comp := &mockCompleter{fn: func(_ int, user string) (string, error) {
		var items []string
		for _, n := range metricsInPrompt(user) {
			if n == "metric_100" {
				items = append(items, `{"metric_name":"metric_100","is_invalid":true,"invalid_types":["meaningless_metric"],"risk_level":"minor","risk_reason":"r","root_cause":"rc","recommendations":["x"],"confidence":0.6}`)
			} else {
				items = append(items, fmt.Sprintf(`{"metric_name":%q,"is_invalid":false}`, n))
			}
		}
		return fmt.Sprintf(`{"metrics":[%s],"summary":"ok"}`, strings.Join(items, ",")), nil
	}}
	res := batchAnalyzer(comp, 50, 0).Run(context.Background(), "s", genProfiles(101), nil)

	m, ok := findInvalid(res.Invalids, "metric_100")
	if !ok {
		t.Fatalf("AI-only finding from the last batch must be added; got none")
	}
	if !reflect.DeepEqual(m.AnalysisSources, []string{SourceLLM}) {
		t.Errorf("analysis_sources = %v, want [llm]", m.AnalysisSources)
	}
}

func TestRunBatchingPayloadIsolation(t *testing.T) {
	var seen [][]string
	comp := &mockCompleter{fn: func(_ int, user string) (string, error) {
		names := metricsInPrompt(user)
		seen = append(seen, names)
		return echoValid(names), nil
	}}
	batchAnalyzer(comp, 50, 0).Run(context.Background(), "s", genProfiles(101), nil)

	if len(seen) != 3 {
		t.Fatalf("batches = %d, want 3", len(seen))
	}
	if len(seen[0]) != 50 || len(seen[1]) != 50 || len(seen[2]) != 1 {
		t.Errorf("batch sizes = %d/%d/%d, want 50/50/1", len(seen[0]), len(seen[1]), len(seen[2]))
	}
	// Each request carries ONLY its own batch's metrics — disjoint sets.
	if contains(seen[0], "metric_050") || contains(seen[1], "metric_000") || contains(seen[2], "metric_000") {
		t.Errorf("payload isolation violated: a request contained another batch's metric")
	}
	if !contains(seen[0], "metric_000") || !contains(seen[1], "metric_050") || !contains(seen[2], "metric_100") {
		t.Errorf("batch contents misordered: %v", seen)
	}
}

func TestRunBatchingPerBatchPayloadTooLarge(t *testing.T) {
	// batch_size 1: make metric_001's payload exceed MaxPayloadBytes; the other two
	// are small. The oversized batch must skip its LLM call; the others still run.
	profs := genProfiles(3)
	big := make([]string, 200)
	for i := range big {
		big[i] = fmt.Sprintf("value-%03d", i)
	}
	profs[1].LabelCardinality = map[string]int{"service": 200}
	profs[1].SampleLabelValues = map[string][]string{"service": big}

	comp := &mockCompleter{fn: func(_ int, user string) (string, error) {
		if contains(metricsInPrompt(user), "metric_001") {
			t.Fatalf("oversized batch must NOT call the LLM")
		}
		return echoValid(metricsInPrompt(user)), nil
	}}
	res := batchAnalyzer(comp, 1, 1500).Run(context.Background(), "s", profs, nil)

	if res.Info.Status != StatusPartial {
		t.Fatalf("status = %q, want partial", res.Info.Status)
	}
	if comp.calls != 2 {
		t.Errorf("LLM calls = %d, want 2 (oversized batch skipped, others run)", comp.calls)
	}
	if res.Info.SuccessfulBatches != 2 || res.Info.FailedBatches != 1 {
		t.Errorf("successful/failed = %d/%d, want 2/1", res.Info.SuccessfulBatches, res.Info.FailedBatches)
	}
	if len(res.Info.BatchFailures) != 1 || res.Info.BatchFailures[0].Reason != "payload_too_large" ||
		res.Info.BatchFailures[0].BatchIndex != 1 {
		t.Errorf("batch_failures = %+v, want [{index 1, payload_too_large}]", res.Info.BatchFailures)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
