package rules

import (
	"prom-ai-guard/internal/config"
	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/tsdb"
)

// Contexts resolves the ownership context for every metric in stats, reusing
// the same inventory resolution as the rule engine so owner/service/namespace
// match the analysis report. It is additive and does not affect Evaluate.
func Contexts(stats map[string]*tsdb.MetricStat, inv config.Inventory) map[string]model.MetricContext {
	index := buildInventoryIndex(inv)
	out := make(map[string]model.MetricContext, len(stats))
	for name, st := range stats {
		matched, _ := resolveOwner(st, index)
		owner, service, namespace := ownerContext(st, matched)
		out[name] = model.MetricContext{
			Owner:     owner,
			Service:   service,
			Namespace: namespace,
			Jobs:      st.JobValues(),
		}
	}
	return out
}
