package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// aiMetric is one per-metric assessment returned by the model.
type aiMetric struct {
	MetricName      string   `json:"metric_name"`
	IsInvalid       bool     `json:"is_invalid"`
	InvalidTypes    []string `json:"invalid_types"`
	RiskLevel       string   `json:"risk_level"`
	RiskReason      string   `json:"risk_reason"`
	RootCause       string   `json:"root_cause"`
	Recommendations []string `json:"recommendations"`
	Confidence      float64  `json:"confidence"`
}

// aiResponse is the whole model response.
type aiResponse struct {
	Metrics []aiMetric `json:"metrics"`
	Summary string     `json:"summary"`
}

// parseResponse parses the model content into an aiResponse. It tolerates a
// surrounding ```json ... ``` code fence but otherwise requires valid JSON with
// a top-level object. A parse error here causes a retry/fallback upstream.
func parseResponse(content string) (aiResponse, error) {
	var resp aiResponse
	trimmed := stripCodeFence(strings.TrimSpace(content))
	if trimmed == "" {
		return resp, fmt.Errorf("empty AI response")
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	if err := dec.Decode(&resp); err != nil {
		return resp, fmt.Errorf("invalid AI response JSON")
	}
	return resp, nil
}

// stripCodeFence removes a leading ```json / ``` fence and a trailing ``` fence
// if present, returning the inner content.
func stripCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the first line (``` or ```json) and a trailing fence.
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
