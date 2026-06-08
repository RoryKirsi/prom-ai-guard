package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"prom-ai-guard/internal/diff"
	"prom-ai-guard/internal/report"
)

type diffOptions struct {
	previous string
	current  string
	out      string
	jsonOut  string
}

func newDiffCmd() *cobra.Command {
	opts := &diffOptions{}
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Deterministically compare two analysis_report.json files (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd, opts)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.previous, "previous", "", "path to the older analysis_report.json (required)")
	f.StringVar(&opts.current, "current", "", "path to the newer analysis_report.json (required)")
	f.StringVar(&opts.out, "out", "reports/diff_report.md", "path to write the Markdown diff report")
	f.StringVar(&opts.jsonOut, "json", "", "optional path to also write the DiffResult JSON")
	_ = cmd.MarkFlagRequired("previous")
	_ = cmd.MarkFlagRequired("current")
	return cmd
}

func runDiff(cmd *cobra.Command, opts *diffOptions) error {
	previous, err := loadReportForDiff(opts.previous, "previous")
	if err != nil {
		return err
	}
	current, err := loadReportForDiff(opts.current, "current")
	if err != nil {
		return err
	}

	result := diff.Compute(previous, current)

	if err := diff.WriteMarkdown(result, opts.out); err != nil {
		return err
	}
	if opts.jsonOut != "" {
		if err := diff.WriteJSON(result, opts.jsonOut); err != nil {
			return err
		}
	}

	printDiffSummary(cmd, opts, result)
	return nil
}

// loadReportForDiff reads, strictly validates, and decodes one report. A read,
// JSON, or schema/data error is a tool error (exit 1) — naming which side failed.
func loadReportForDiff(path, side string) (report.Report, error) {
	var r report.Report
	raw, err := os.ReadFile(path)
	if err != nil {
		return r, fmt.Errorf("reading %s report: %w", side, err)
	}
	if err := diff.ValidateReport(raw); err != nil {
		return r, fmt.Errorf("%s report %s: %w", side, path, err)
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, fmt.Errorf("decoding %s report: %w", side, err)
	}
	return r, nil
}

func printDiffSummary(cmd *cobra.Command, opts *diffOptions, d diff.DiffResult) {
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "prom-ai-guard diff")
	fmt.Fprintf(w, "  previous:     %s (scan_id=%s)\n", opts.previous, orNA(d.Previous.ScanID))
	fmt.Fprintf(w, "  current:      %s (scan_id=%s)\n", opts.current, orNA(d.Current.ScanID))
	fmt.Fprintf(w, "  added:        %d   resolved: %d   still: %d\n", len(d.AddedInvalid), len(d.ResolvedInvalid), len(d.StillInvalid))
	fmt.Fprintf(w, "  risk +/-:     +%d / -%d   type_changes: %d\n", len(d.RiskIncreased), len(d.RiskDecreased), len(d.TypeChanges))
	fmt.Fprintf(w, "  report (md):  %s\n", opts.out)
	if opts.jsonOut != "" {
		fmt.Fprintf(w, "  report (json):%s\n", " "+opts.jsonOut)
	}
	if d.ConfigChanged {
		fmt.Fprintln(w, "  ⚠ config_hash changed between reports — differences may reflect rule/config changes")
	}
}

func orNA(s string) string {
	if s == "" {
		return "n/a"
	}
	return s
}
