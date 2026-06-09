package ai

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// ai.summary under FullScan batching is the LAST successful batch's summary — NOT
// a whole-batch synthesis. This documents the limitation that ai.governance_summary
// (Slice 17) exists to fix.
func TestRunBatchSummaryIsLastBatchNotGlobal(t *testing.T) {
	comp := &mockCompleter{fn: func(_ int, user string) (string, error) {
		names := metricsInPrompt(user)
		first := "none"
		if len(names) > 0 {
			first = names[0]
		}
		items := make([]string, len(names))
		for i, n := range names {
			items[i] = fmt.Sprintf(`{"metric_name":%q,"is_invalid":false}`, n)
		}
		return fmt.Sprintf(`{"metrics":[%s],"summary":"summary-of-%s"}`, strings.Join(items, ","), first), nil
	}}
	// 101 profiles, batch_size 50 -> batches [000..049],[050..099],[100]; last is metric_100.
	res := batchAnalyzer(comp, 50, 0).Run(context.Background(), "s", genProfiles(101), nil)
	if res.Info.Summary != "summary-of-metric_100" {
		t.Errorf("ai.summary = %q, want the LAST batch's summary (proves per-batch, not whole-batch)", res.Info.Summary)
	}
}
