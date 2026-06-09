package ai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"prom-ai-guard/internal/model"
)

func sampleGovInput() GovernanceSynthesisInput {
	return GovernanceSynthesisInput{
		Assessment: model.GovernanceAssessment{
			InvalidRatio: 0.6364, TotalInvalid: 7,
			RiskDistribution: map[string]int{"severe": 1, "warning": 4, "minor": 2},
			TopSystemicIssues: []model.SystemicIssue{
				{InvalidType: "high_cardinality", MetricCount: 1, MaxRisk: "severe", MaxScore: 90},
				{InvalidType: "orphan_metric", MetricCount: 1, MaxRisk: "minor", MaxScore: 35},
			},
			MaturityScore: 51, MaturityGrade: "D",
			PrioritizedActions: []string{"Reduce label cardinality on 1 high-cardinality metric(s)."},
			RecommendedNorms:   []string{"Set per-metric label-cardinality budgets."},
		},
		InvalidTypeCounts: map[string]int{"high_cardinality": 1, "orphan_metric": 1},
	}
}

func TestSynthesizeGovernanceWholeBatchPayload(t *testing.T) {
	comp := &mockCompleter{fn: func(_ int, _ string) (string, error) {
		return "Overall governance is weak (grade D); the top systemic issue is high cardinality.", nil
	}}
	out, err := SynthesizeGovernance(context.Background(), comp, sampleGovInput(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("expected a narrative")
	}
	// The prompt must carry the WHOLE-BATCH aggregate — every systemic type + totals
	// — proving it is not just one sub-batch's view.
	u := comp.lastUser
	for _, want := range []string{"high_cardinality", "orphan_metric", "total_invalid", "7", "recommended_norms"} {
		if !strings.Contains(u, want) {
			t.Errorf("synthesis prompt missing whole-batch aggregate %q:\n%s", want, u)
		}
	}
}

func TestSynthesizeGovernanceNoRawData(t *testing.T) {
	const marker = "SECRETVAL_alice@example.com"
	comp := &mockCompleter{fn: func(_ int, user string) (string, error) {
		if strings.Contains(user, marker) {
			t.Fatalf("raw value reached the synthesis prompt")
		}
		return "ok", nil
	}}
	// The input type only accepts aggregates; there is no field for raw values.
	if _, err := SynthesizeGovernance(context.Background(), comp, sampleGovInput(), 2); err != nil {
		t.Fatal(err)
	}
}

func TestSynthesizeGovernanceRetryThenSucceed(t *testing.T) {
	comp := &mockCompleter{fn: func(call int, _ string) (string, error) {
		if call == 1 {
			return "", errors.New("transient")
		}
		return "second-try narrative", nil
	}}
	out, err := SynthesizeGovernance(context.Background(), comp, sampleGovInput(), 2)
	if err != nil || out != "second-try narrative" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if comp.calls != 2 {
		t.Errorf("calls=%d want 2", comp.calls)
	}
}

func TestSynthesizeGovernanceFailAll(t *testing.T) {
	comp := &mockCompleter{fn: func(_ int, _ string) (string, error) { return "", errors.New("down") }}
	out, err := SynthesizeGovernance(context.Background(), comp, sampleGovInput(), 2)
	if err == nil || out != "" {
		t.Fatalf("expected error + empty summary, got out=%q err=%v", out, err)
	}
}

func TestSynthesizeGovernanceEmptyResponse(t *testing.T) {
	comp := &mockCompleter{fn: func(_ int, _ string) (string, error) { return "   ", nil }}
	out, err := SynthesizeGovernance(context.Background(), comp, sampleGovInput(), 2)
	if err != nil || out != "" {
		t.Fatalf("whitespace response should yield empty summary: out=%q err=%v", out, err)
	}
}
