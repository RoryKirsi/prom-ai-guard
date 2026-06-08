package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"prom-ai-guard/internal/config"
	"prom-ai-guard/internal/gate"
	"prom-ai-guard/internal/report"
)

type gateOptions struct {
	report  string
	policy  string
	jsonOut bool
}

func newGateCmd() *cobra.Command {
	opts := &gateOptions{}
	cmd := &cobra.Command{
		Use:   "gate",
		Short: "Evaluate analysis_report.json against a policy (deterministic CI/CD gate)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGate(cmd, opts)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.report, "report", "reports/analysis_report.json", "path to analysis_report.json")
	f.StringVar(&opts.policy, "policy", "configs/policy.yaml", "path to policy.yaml")
	f.BoolVar(&opts.jsonOut, "json", false, "emit only the GateResult JSON on stdout (CI-safe)")
	return cmd
}

func runGate(cmd *cobra.Command, opts *gateOptions) error {
	// Policy is required; a missing/malformed policy is a tool/config error (exit 1).
	policy, err := config.LoadPolicy(opts.policy)
	if err != nil {
		return err
	}

	raw, err := os.ReadFile(opts.report)
	if err != nil {
		return fmt.Errorf("reading report: %w", err)
	}
	if !json.Valid(raw) {
		return fmt.Errorf("report %s is not valid JSON", opts.report)
	}

	// Schema check governed by fail_on_schema_error.
	if serr := gate.ValidateReportSchema(raw); serr != nil {
		if policy.Gate.FailOnSchemaError {
			return fmt.Errorf("report schema error: %w", serr)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: report schema issue (fail_on_schema_error=false): %v\n", serr)
	}

	var rep report.Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		return fmt.Errorf("decoding report: %w", err)
	}

	result := gate.Evaluate(rep, policy)

	if opts.jsonOut {
		// CI-safe: stdout contains only the GateResult JSON, nothing else.
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
	} else {
		printGateText(cmd, opts, result)
	}

	if result.ExitCode == 2 {
		return &exitCodeError{code: 2}
	}
	return nil
}

func printGateText(cmd *cobra.Command, opts *gateOptions, r gate.GateResult) {
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "prom-ai-guard gate")
	fmt.Fprintf(w, "  report:       %s\n", opts.report)
	fmt.Fprintf(w, "  policy:       %s\n", opts.policy)
	verdict := "PASS"
	if !r.Passed {
		verdict = "FAIL"
	}
	fmt.Fprintf(w, "  result:       %s (exit %d)\n", verdict, r.ExitCode)
	if len(r.PolicyHits) == 0 {
		fmt.Fprintln(w, "  policy_hits:  none")
		return
	}
	fmt.Fprintln(w, "  policy_hits:")
	for _, h := range r.PolicyHits {
		fmt.Fprintf(w, "    - %s: %s\n", h.PolicyID, h.Message)
	}
}
