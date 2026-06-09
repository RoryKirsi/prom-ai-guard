package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"prom-ai-guard/internal/model"
)

// maxGovSummaryLen caps the advisory governance narrative length.
const maxGovSummaryLen = 4000

// GovernanceSynthesisInput is the AGGREGATED, safe-only input to the whole-batch
// governance synthesis. It carries no raw MetricProfile, no raw label values, and
// no prompt/response text — only deterministic counts/types/label-keys that are
// already in analysis_report.json.
type GovernanceSynthesisInput struct {
	Assessment        model.GovernanceAssessment
	InvalidTypeCounts map[string]int
	BatchSummaries    []string // optional per-batch short summaries (advisory)
}

// SynthesizeGovernance makes ONE final LLM call over the aggregated governance data
// and returns an advisory whole-batch narrative. It is independent of metric-level
// batching/classification: on failure it returns ("", err) and the caller leaves
// ai.governance_summary empty without changing ai.status/fallback_used. A
// whitespace-only response yields ("", nil).
func SynthesizeGovernance(ctx context.Context, c Completer, in GovernanceSynthesisInput, maxAttempts int) (string, error) {
	if c == nil {
		return "", errors.New("no completer")
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	user, err := governanceUserPrompt(in)
	if err != nil {
		return "", err
	}
	sys := governanceSystemPrompt()

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		content, cerr := c.Complete(ctx, sys, user)
		if cerr != nil {
			lastErr = cerr
			continue
		}
		return capLen(strings.TrimSpace(content)), nil
	}
	return "", lastErr
}

func capLen(s string) string {
	if len(s) > maxGovSummaryLen {
		return s[:maxGovSummaryLen]
	}
	return s
}

func governanceSystemPrompt() string {
	return `You are a Prometheus observability governance expert. You are given AGGREGATED,
already-computed governance data for an entire metrics scan (totals, risk
distribution, invalid-type counts, the top systemic issues, prioritized actions,
recommended norms, and a heuristic maturity grade). Write ONE concise overall
monitoring-governance assessment for the WHOLE batch: summarize the systemic
issues, the risk posture, and the prioritized remediation. Plain prose only — no
JSON, no markdown headers. This narrative is advisory; the provided deterministic
data is authoritative.`
}

// governanceUserPrompt serializes only the safe aggregate.
func governanceUserPrompt(in GovernanceSynthesisInput) (string, error) {
	a := in.Assessment
	payload := map[string]any{
		"total_invalid":       a.TotalInvalid,
		"invalid_ratio":       a.InvalidRatio,
		"risk_distribution":   a.RiskDistribution,
		"invalid_type_counts": in.InvalidTypeCounts,
		"top_systemic_issues": a.TopSystemicIssues,
		"prioritized_actions": a.PrioritizedActions,
		"recommended_norms":   a.RecommendedNorms,
		"maturity":            map[string]any{"grade": a.MaturityGrade, "score": a.MaturityScore},
	}
	if len(in.BatchSummaries) > 0 {
		payload["batch_summaries"] = in.BatchSummaries
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return "Aggregated whole-batch governance data (deterministic, authoritative):\n" + string(b) +
		"\n\nWrite the overall monitoring-governance assessment for the whole batch.", nil
}
