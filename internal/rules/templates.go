package rules

// Slice 2 has no AI, so root_cause and recommendations come from deterministic
// per-type templates. DeepSeek refines these in a later slice.

var rootCauseByType = map[string]string{
	TypeHighCardinality:  "High-cardinality or forbidden label creates unbounded time series.",
	TypeInvalidLabelName: "Label name violates the Prometheus naming convention.",
	TypeDuplicate:        "Duplicate fingerprint: identical metric name and label set reported more than once.",
	TypeDeprecated:       "Metric name matches a deprecated/legacy naming pattern.",
	TypeEmptyLabelValue:  "Metric carries a label with an empty value.",
	TypeOrphan:           "Metric's service/job does not resolve to any entry in service_inventory.yaml.",
	TypeMeaningless:      "Metric name matches a debug/test/temp pattern.",
}

var recommendationsByType = map[string][]string{
	TypeHighCardinality: {
		"Remove high-cardinality labels (e.g. user_id) from the metric.",
		"Use logs or tracing for per-entity investigation.",
	},
	TypeInvalidLabelName: {
		"Rename the label to match [a-zA-Z_][a-zA-Z0-9_]*.",
		"Drop the non-conforming label via metric_relabel_configs.",
	},
	TypeDuplicate: {
		"Deduplicate the exporter output.",
		"Check for double scraping or merged jobs producing identical series.",
	},
	TypeDeprecated: {
		"Confirm the metric is unused and remove the instrumentation.",
		"Add a metric_relabel drop rule if removal is delayed.",
	},
	TypeEmptyLabelValue: {
		"Stop emitting empty labels at the source.",
		"Use metric_relabel_configs to drop empty labels.",
	},
	TypeOrphan: {
		"Add the service to service_inventory.yaml or fix the job/service label.",
		"Confirm the owning team and assign remediation.",
	},
	TypeMeaningless: {
		"Remove debug/test metrics from production exposition.",
		"Gate temporary metrics behind a flag.",
	},
}

// rootCause returns the template for the dominant (highest base score) type.
func rootCause(types []string) string {
	dominant := dominantType(types)
	if rc, ok := rootCauseByType[dominant]; ok {
		return rc
	}
	return "Invalid metric detected by local rules."
}

// recommendations unions the per-type recommendations, ordered by descending
// base score and de-duplicated.
func recommendations(types []string) []string {
	ordered := append([]string{}, types...)
	sortByBaseDesc(ordered)
	var out []string
	seen := map[string]bool{}
	for _, t := range ordered {
		for _, r := range recommendationsByType[t] {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	return out
}

func dominantType(types []string) string {
	best, bestScore := "", -1
	for _, t := range types {
		if baseScore[t] > bestScore {
			best, bestScore = t, baseScore[t]
		}
	}
	return best
}

func sortByBaseDesc(types []string) {
	for i := 1; i < len(types); i++ {
		for j := i; j > 0 && baseScore[types[j]] > baseScore[types[j-1]]; j-- {
			types[j], types[j-1] = types[j-1], types[j]
		}
	}
}
