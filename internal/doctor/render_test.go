package doctor

import (
	"strings"
	"testing"
)

func TestRenderSectionsAndMatch(t *testing.T) {
	d := Diagnose(sampleReport(), Query{Metric: "http_user_requests_total", Label: "user_id"})
	out := Render(d)
	for _, want := range []string{
		"# prom-ai-guard doctor report",
		"## Query (AND of provided selectors)",
		"## Report",
		"## Matches (1)",
		"### http_user_requests_total",
		"relabel_candidate: true   relabel_proposal_possible: true",
		"matched_labels: user_id",
		"## Notes",
		"relabel_proposal_possible is an estimate", // disclaimer note
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}
}

func TestRenderNoMatchMessageAndAbsenceNote(t *testing.T) {
	out := Render(Diagnose(sampleReport(), Query{Metric: "totally_absent"}))
	if !strings.Contains(out, "## Matches (0)") || !strings.Contains(out, "No invalid metrics matched the selectors.") {
		t.Errorf("expected explicit no-match message:\n%s", out)
	}
	if !strings.Contains(out, "not found in invalid_metrics") || !strings.Contains(out, "cannot confirm healthy") {
		t.Errorf("expected absence/cannot-confirm-healthy note:\n%s", out)
	}
}

func TestRenderDeterministic(t *testing.T) {
	d := Diagnose(sampleReport(), Query{Service: "orders-api"})
	if Render(d) != Render(d) {
		t.Errorf("render is not deterministic")
	}
}
