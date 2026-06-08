package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"prom-ai-guard/internal/relabel"
	"prom-ai-guard/internal/report"
)

type relabelOptions struct {
	report string
	out    string
}

func newRelabelCmd() *cobra.Command {
	opts := &relabelOptions{}
	cmd := &cobra.Command{
		Use:   "relabel",
		Short: "Generate a Prometheus relabel-rule proposal from analysis_report.json (never applied)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRelabel(cmd, opts)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.report, "report", "reports/analysis_report.json", "path to analysis_report.json (read-only)")
	f.StringVar(&opts.out, "out", "reports/relabel_rules.yaml", "path to write the relabel proposal")
	return cmd
}

func runRelabel(cmd *cobra.Command, opts *relabelOptions) error {
	// Read the report read-only. The tool never re-runs the scan, never calls an
	// LLM, never mutates the report, and never applies rules anywhere.
	raw, err := os.ReadFile(opts.report)
	if err != nil {
		return fmt.Errorf("reading report: %w", err)
	}
	if !json.Valid(raw) {
		return fmt.Errorf("report %s is not valid JSON", opts.report)
	}
	var rep report.Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		return fmt.Errorf("decoding report: %w", err)
	}

	plan := relabel.Generate(rep)
	plan.GeneratedFrom.Report = opts.report
	if err := relabel.WriteYAML(plan, opts.out); err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "prom-ai-guard relabel")
	fmt.Fprintf(w, "  report:       %s\n", opts.report)
	fmt.Fprintf(w, "  output:       %s\n", opts.out)
	s := plan.DryRunSummary
	fmt.Fprintf(w, "  rules:        %d (labeldrop=%d drop=%d review=%d)\n",
		s.TotalRules, s.ByAction[relabel.ActionLabelDrop], s.ByAction[relabel.ActionDrop], s.ByAction[relabel.ActionReview])
	fmt.Fprintf(w, "  note:         %s\n", s.Note)
	fmt.Fprintf(w, "  scope:        %s\n", s.ScopeWarning)
	return nil
}
