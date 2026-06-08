// Package gate implements the deterministic CI/CD Gate. It reads an existing
// analysis_report.json (the machine contract) and a policy.yaml, evaluates only
// deterministic report fields, and returns a pass/fail GateResult. It never
// calls an LLM, never re-runs analysis, and never mutates the report. The AI
// prose summary is never used; local rule severity (already encoded in the
// report) is authoritative.
package gate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"prom-ai-guard/internal/config"
	"prom-ai-guard/internal/report"
	"prom-ai-guard/internal/rules"
)

// PolicyHit is one failed policy check (contract §5.5).
type PolicyHit struct {
	PolicyID string `json:"policy_id"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// GateResult is the gate decision (contract §5.5). It is printed/returned only,
// never written back into analysis_report.json.
type GateResult struct {
	Passed     bool        `json:"passed"`
	ExitCode   int         `json:"exit_code"`
	PolicyHits []PolicyHit `json:"policy_hits"`
}

// Evaluate applies the policy to the deterministic fields of the report.
func Evaluate(rep report.Report, pol config.Policy) GateResult {
	g := pol.Gate
	hits := []PolicyHit{}

	if g.FailOnFallbackUsed && rep.AI != nil && rep.AI.FallbackUsed {
		hits = append(hits, PolicyHit{"fail_on_fallback_used", "severe", "AI fallback was used"})
	}
	if g.MaxSevere != nil {
		if sev := rep.Summary.RiskDistribution["severe"]; sev > *g.MaxSevere {
			hits = append(hits, PolicyHit{"max_severe", "severe",
				fmt.Sprintf("severe=%d exceeds max %d", sev, *g.MaxSevere)})
		}
	}
	if g.MaxWarning != nil {
		if w := rep.Summary.RiskDistribution["warning"]; w > *g.MaxWarning {
			hits = append(hits, PolicyHit{"max_warning", "warning",
				fmt.Sprintf("warning=%d exceeds max %d", w, *g.MaxWarning)})
		}
	}
	if g.MaxInvalidRatio != nil {
		if r := rep.Summary.InvalidRatio; r > *g.MaxInvalidRatio {
			hits = append(hits, PolicyHit{"max_invalid_ratio", "warning",
				fmt.Sprintf("invalid_ratio %.4f exceeds max %.4f", r, *g.MaxInvalidRatio)})
		}
	}
	if g.MaxHighCardinalityMetrics != nil {
		if hc := rep.Summary.InvalidTypeCounts[rules.TypeHighCardinality]; hc > *g.MaxHighCardinalityMetrics {
			hits = append(hits, PolicyHit{"max_high_cardinality_metrics", "severe",
				fmt.Sprintf("high_cardinality=%d exceeds max %d", hc, *g.MaxHighCardinalityMetrics)})
		}
	}
	if keys := forbiddenPresent(rep, g.ForbiddenLabelKeys); len(keys) > 0 {
		hits = append(hits, PolicyHit{"forbidden_label_keys", "severe",
			fmt.Sprintf("forbidden label key(s) present: %s", strings.Join(keys, ", "))})
	}

	res := GateResult{PolicyHits: hits, Passed: len(hits) == 0}
	if res.Passed {
		res.ExitCode = 0
	} else {
		res.ExitCode = 2
	}
	return res
}

// forbiddenPresent returns the sorted set of forbidden keys found in either
// top_violation_labels[].label_key or any invalid_metrics[].label_cardinality
// key. Checking both ensures a forbidden label is not missed just because it is
// absent from the top aggregation.
func forbiddenPresent(rep report.Report, forbidden []string) []string {
	if len(forbidden) == 0 {
		return nil
	}
	set := make(map[string]bool, len(forbidden))
	for _, k := range forbidden {
		set[k] = true
	}
	found := map[string]bool{}
	for _, v := range rep.TopViolationLabels {
		if set[v.LabelKey] {
			found[v.LabelKey] = true
		}
	}
	for _, m := range rep.InvalidMetrics {
		for key := range m.LabelCardinality {
			if set[key] {
				found[key] = true
			}
		}
	}
	if len(found) == 0 {
		return nil
	}
	out := make([]string, 0, len(found))
	for k := range found {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ValidateReportSchema checks the fields the Gate depends on without a full
// JSON-Schema library. It rejects a silent zero-value pass when required fields
// are missing. (Malformed JSON is detected by the caller before this.)
func ValidateReportSchema(raw []byte) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return fmt.Errorf("report is not a JSON object: %w", err)
	}

	var sv string
	r, ok := top["schema_version"]
	if !ok {
		return fmt.Errorf("missing schema_version")
	}
	if err := json.Unmarshal(r, &sv); err != nil || sv != "v1" {
		return fmt.Errorf("unsupported schema_version %q (want v1)", sv)
	}

	sumRaw, ok := top["summary"]
	if !ok {
		return fmt.Errorf("missing summary")
	}
	var sum map[string]json.RawMessage
	if err := json.Unmarshal(sumRaw, &sum); err != nil {
		return fmt.Errorf("summary is not an object")
	}
	for _, k := range []string{"risk_distribution", "invalid_type_counts"} {
		v, ok := sum[k]
		if !ok {
			return fmt.Errorf("missing summary.%s", k)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(v, &obj); err != nil {
			return fmt.Errorf("summary.%s is not an object", k)
		}
	}
	ir, ok := sum["invalid_ratio"]
	if !ok {
		return fmt.Errorf("missing summary.invalid_ratio")
	}
	var ratio float64
	if err := json.Unmarshal(ir, &ratio); err != nil {
		return fmt.Errorf("summary.invalid_ratio is not a number")
	}

	if err := requireArray(top, "invalid_metrics"); err != nil {
		return err
	}
	if err := requireArray(top, "top_violation_labels"); err != nil {
		return err
	}

	if aiRaw, ok := top["ai"]; ok {
		var aiObj map[string]json.RawMessage
		if err := json.Unmarshal(aiRaw, &aiObj); err != nil {
			return fmt.Errorf("ai is not an object")
		}
		if fu, ok := aiObj["fallback_used"]; ok {
			var b bool
			if err := json.Unmarshal(fu, &b); err != nil {
				return fmt.Errorf("ai.fallback_used is not a bool")
			}
		}
	}
	return nil
}

func requireArray(top map[string]json.RawMessage, key string) error {
	r, ok := top[key]
	if !ok {
		return fmt.Errorf("missing %s", key)
	}
	if t := strings.TrimSpace(string(r)); t == "" || t[0] != '[' {
		return fmt.Errorf("%s is not an array", key)
	}
	return nil
}
