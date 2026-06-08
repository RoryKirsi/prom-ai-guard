package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"prom-ai-guard/internal/doctor"
	"prom-ai-guard/internal/report"
)

type doctorOptions struct {
	report  string
	metric  string
	label   string
	service string
	out     string
	jsonOut string
}

func newDoctorCmd() *cobra.Command {
	opts := &doctorOptions{}
	cmd := &cobra.Command{
		Use:     "doctor",
		Aliases: []string{"inspect"},
		Short:   "Focused report-only diagnosis of a metric/label/service from analysis_report.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd, opts)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.report, "report", "reports/analysis_report.json", "path to analysis_report.json (read-only)")
	f.StringVar(&opts.metric, "metric", "", "metric name selector (exact match)")
	f.StringVar(&opts.label, "label", "", "label key selector (must exist in the metric's label_cardinality)")
	f.StringVar(&opts.service, "service", "", "service selector (exact match)")
	f.StringVar(&opts.out, "out", "", "optional path to also write the Markdown diagnosis (default: console only)")
	f.StringVar(&opts.jsonOut, "json", "", "optional path to also write the DoctorResult JSON (default: console only)")
	return cmd
}

func runDoctor(cmd *cobra.Command, opts *doctorOptions) error {
	q := doctor.Query{Metric: opts.metric, Label: opts.label, Service: opts.service}
	if q.Empty() {
		return fmt.Errorf("provide at least one selector: --metric, --label, or --service")
	}

	raw, err := os.ReadFile(opts.report)
	if err != nil {
		return fmt.Errorf("reading report: %w", err)
	}
	if err := doctor.ValidateReport(raw); err != nil {
		return fmt.Errorf("report %s: %w", opts.report, err)
	}
	var rep report.Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		return fmt.Errorf("decoding report: %w", err)
	}

	result := doctor.Diagnose(rep, q)

	// Console is the default output; files are written only when requested.
	fmt.Fprint(cmd.OutOrStdout(), doctor.Render(result))
	if opts.out != "" {
		if err := doctor.WriteMarkdown(result, opts.out); err != nil {
			return err
		}
	}
	if opts.jsonOut != "" {
		if err := doctor.WriteJSON(result, opts.jsonOut); err != nil {
			return err
		}
	}
	return nil
}
