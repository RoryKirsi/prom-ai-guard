package ai

import (
	"bytes"
	"encoding/json"

	"prom-ai-guard/internal/profile"
	"prom-ai-guard/internal/rules"
)

// promptMetric is one redacted metric as sent to the model. It is built only
// from a redacted MetricProfile plus the metric's local rule context.
type promptMetric struct {
	MetricName        string              `json:"metric_name"`
	SeriesCount       int                 `json:"series_count"`
	LabelKeys         []string            `json:"label_keys"`
	LabelCardinality  map[string]int      `json:"label_cardinality"`
	SampleLabelValues map[string][]string `json:"sample_label_values"`
	RuleSignals       []string            `json:"rule_signals"`
	RuleInvalidTypes  []string            `json:"rule_invalid_types"`
	RuleRiskLevel     string              `json:"rule_risk_level"`
}

// requestPayload is the user-message JSON.
type requestPayload struct {
	ScanID  string         `json:"scan_id"`
	Metrics []promptMetric `json:"metrics"`
}

// buildPayload converts the (already redacted) in-scope profiles into the
// request payload. RuleRiskLevel is derived deterministically from the metric's
// rule invalid types.
func buildPayload(scanID string, profiles []profile.MetricProfile) requestPayload {
	metrics := make([]promptMetric, 0, len(profiles))
	for _, p := range profiles {
		metrics = append(metrics, promptMetric{
			MetricName:        p.MetricName,
			SeriesCount:       p.SeriesCount,
			LabelKeys:         p.LabelKeys,
			LabelCardinality:  p.LabelCardinality,
			SampleLabelValues: p.SampleLabelValues,
			RuleSignals:       p.RuleSignals,
			RuleInvalidTypes:  p.InvalidTypes,
			RuleRiskLevel:     ruleRiskLevel(p.InvalidTypes),
		})
	}
	return requestPayload{ScanID: scanID, Metrics: metrics}
}

func ruleRiskLevel(types []string) string {
	if len(types) == 0 {
		return "none"
	}
	return rules.RiskLevelFor(rules.RiskScore(types))
}

// userPrompt serializes the payload as the user message body. HTML escaping is
// disabled so the "<redacted>" placeholder is sent literally rather than as
// <redacted>.
func userPrompt(p requestPayload) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// systemPrompt is the static instruction. It constrains the model to the seven
// invalid types and to JSON-only output, and forbids inventing values or
// lowering severity.
func systemPrompt() string {
	return `You are a Prometheus observability governance expert.
You receive redacted metric profiles with local rule signals. Sensitive label
values are already replaced with "<redacted>"; never try to reconstruct or
invent them.

For each metric, return a refined assessment. You MAY add a new finding or
upgrade severity, but you MUST NOT lower a severity the local rules assigned.
Use only these invalid_types: deprecated_metric, duplicate_metric,
empty_label_value, invalid_label_name, meaningless_metric, orphan_metric,
high_cardinality.

Respond with JSON ONLY, no markdown, in exactly this shape:
{"metrics":[{"metric_name":"...","is_invalid":true,"invalid_types":["..."],
"risk_level":"severe|warning|minor","risk_reason":"...","root_cause":"...",
"recommendations":["..."],"confidence":0.0}],"summary":"..."}

Rules:
- is_invalid=false means the metric is acceptable; omit invalid_types.
- is_invalid=true requires at least one valid invalid_types value.
- recommendations must be concrete (metric cleanup, relabel/labeldrop, label governance).
- Output the metric_name exactly as given.`
}
