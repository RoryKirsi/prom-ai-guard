// Package diff computes a deterministic historical comparison between two
// analysis_report.json files. It reads only the two reports — it never re-runs
// the scan, never calls an LLM, and makes no network calls.
package diff

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidateReport strictly checks that a report has every field the diff needs.
// Missing/invalid fields are treated as a schema/data error (the caller exits 1)
// because a 0-default would produce a misleading historical comparison. It does
// not read any human Markdown — only the JSON structure.
func ValidateReport(raw []byte) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return fmt.Errorf("report is not a JSON object: %w", err)
	}

	var sv string
	if r, ok := top["schema_version"]; !ok {
		return fmt.Errorf("missing schema_version")
	} else if err := json.Unmarshal(r, &sv); err != nil || sv != "v1" {
		return fmt.Errorf("unsupported schema_version %q (want v1)", sv)
	}

	// summary object + strictly-required numeric fields used by SummaryDelta.
	sumRaw, ok := top["summary"]
	if !ok {
		return fmt.Errorf("missing summary")
	}
	var sum map[string]json.RawMessage
	if err := json.Unmarshal(sumRaw, &sum); err != nil {
		return fmt.Errorf("summary is not an object")
	}
	for _, f := range []string{"invalid_metric_names", "total_metric_names", "invalid_ratio"} {
		if err := requireNumber(sum, f, "summary."+f); err != nil {
			return err
		}
	}
	rdRaw, ok := sum["risk_distribution"]
	if !ok {
		return fmt.Errorf("missing summary.risk_distribution")
	}
	var rd map[string]json.RawMessage
	if err := json.Unmarshal(rdRaw, &rd); err != nil {
		return fmt.Errorf("summary.risk_distribution is not an object")
	}
	for _, f := range []string{"severe", "warning", "minor"} {
		if err := requireNumber(rd, f, "summary.risk_distribution."+f); err != nil {
			return err
		}
	}

	// invalid_metrics must be an array of well-formed, uniquely-named entries.
	imRaw, ok := top["invalid_metrics"]
	if !ok {
		return fmt.Errorf("missing invalid_metrics")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(imRaw, &items); err != nil {
		return fmt.Errorf("invalid_metrics is not an array")
	}
	seen := make(map[string]bool, len(items))
	for i, it := range items {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(it, &m); err != nil {
			return fmt.Errorf("invalid_metrics[%d] is not an object", i)
		}
		nr, ok := m["metric_name"]
		if !ok {
			return fmt.Errorf("invalid_metrics[%d] missing metric_name", i)
		}
		var name string
		if err := json.Unmarshal(nr, &name); err != nil || name == "" {
			return fmt.Errorf("invalid_metrics[%d] has an empty or invalid metric_name", i)
		}
		if seen[name] {
			return fmt.Errorf("duplicate metric_name %q in invalid_metrics", name)
		}
		seen[name] = true
		if err := requireNumber(m, "risk_score", fmt.Sprintf("invalid_metrics[%q].risk_score", name)); err != nil {
			return err
		}
		tr, ok := m["invalid_types"]
		if !ok {
			return fmt.Errorf("invalid_metrics[%q] missing invalid_types", name)
		}
		var types []json.RawMessage
		if err := json.Unmarshal(tr, &types); err != nil {
			return fmt.Errorf("invalid_metrics[%q].invalid_types is not an array", name)
		}
	}
	return nil
}

func requireNumber(obj map[string]json.RawMessage, key, label string) error {
	r, ok := obj[key]
	if !ok {
		return fmt.Errorf("missing %s", label)
	}
	if s := strings.TrimSpace(string(r)); s == "" || s == "null" {
		return fmt.Errorf("%s is not a number", label)
	}
	var n float64
	if err := json.Unmarshal(r, &n); err != nil {
		return fmt.Errorf("%s is not a number", label)
	}
	return nil
}
