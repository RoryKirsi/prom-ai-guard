package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"prom-ai-guard/internal/config"
	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/parser"
	"prom-ai-guard/internal/profile"
	"prom-ai-guard/internal/redact"
	"prom-ai-guard/internal/report"
	"prom-ai-guard/internal/rules"
	"prom-ai-guard/internal/scan"
	"prom-ai-guard/internal/tsdb"
)

// sampleValuesPerLabel bounds how many sample label values a MetricProfile
// carries per label key in the AI input preview.
const sampleValuesPerLabel = 5

type scanOptions struct {
	source    string
	input     string
	out       string
	config    string
	scanScope string
}

func newScanCmd() *cobra.Command {
	opts := &scanOptions{}
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan metrics and produce a summary report",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScan(cmd, opts)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.source, "source", "file", "data source: file (prometheus_api in a later slice)")
	f.StringVar(&opts.input, "input", "", "path to a local Prometheus text metrics file (required for source=file)")
	f.StringVar(&opts.out, "out", "reports", "directory for generated reports")
	f.StringVar(&opts.config, "config", "configs", "directory holding rules.yaml and service_inventory.yaml")
	f.StringVar(&opts.scanScope, "scan-scope", "all", "scan scope: all or filtered")
	return cmd
}

func runScan(cmd *cobra.Command, opts *scanOptions) error {
	if opts.source != "file" {
		return fmt.Errorf("source %q is not supported in this version; only %q is available", opts.source, "file")
	}
	if opts.input == "" {
		return fmt.Errorf("--input is required when --source=file")
	}
	if opts.scanScope != "all" && opts.scanScope != "filtered" {
		return fmt.Errorf("--scan-scope %q is invalid; allowed values are %q or %q", opts.scanScope, "all", "filtered")
	}

	rulesPath := filepath.Join(opts.config, "rules.yaml")
	invPath := filepath.Join(opts.config, "service_inventory.yaml")
	rulesCfg, err := config.LoadRules(rulesPath)
	if err != nil {
		return err
	}
	inventory, err := config.LoadInventory(invPath)
	if err != nil {
		return err
	}
	configHash, err := config.HashFiles(rulesPath, invPath)
	if err != nil {
		return err
	}

	f, err := os.Open(opts.input)
	if err != nil {
		return fmt.Errorf("opening input: %w", err)
	}
	defer f.Close()

	series, warns, err := parser.ParseReader(f)
	if err != nil {
		return err
	}

	stats := tsdb.Build(series)
	invalids, contribs := rules.Evaluate(stats, rulesCfg, inventory)
	result := scan.Assemble(len(series), len(stats), invalids, contribs)

	now := time.Now().UTC()
	scanID := now.Format("20060102T150405Z") + "-scan"
	rep := report.Report{
		SchemaVersion: "v1",
		ScanID:        scanID,
		ScanTime:      now.Format(time.RFC3339),
		ToolVersion:   report.ToolVersion,
		ConfigHash:    configHash,
		Source: report.Source{
			SourceType:      opts.source,
			InputRef:        opts.input,
			ScanScope:       opts.scanScope,
			SeriesCount:     result.Summary.TotalSeries,
			MetricNameCount: result.Summary.TotalMetricNames,
		},
		Summary:            result.Summary,
		InvalidMetrics:     result.InvalidMetrics,
		TopRiskMetrics:     result.TopRiskMetrics,
		TopViolationLabels: result.TopViolationLabels,
		Warnings:           warns,
	}

	jsonPath := filepath.Join(opts.out, "analysis_report.json")
	if err := report.WriteJSON(rep, jsonPath); err != nil {
		return err
	}

	// Build the redacted AI input preview (separate artifact; never sent or
	// stored with raw sensitive values).
	analysesByName := make(map[string]model.MetricAnalysis, len(invalids))
	for _, a := range invalids {
		analysesByName[a.MetricName] = a
	}
	contexts := rules.Contexts(stats, inventory)
	profiles := profile.Build(stats, analysesByName, contexts, sampleValuesPerLabel)
	redactedProfiles, redaction := redact.Profiles(profiles)
	preview := report.AIInputPreview{
		SchemaVersion: "v1",
		ScanID:        scanID,
		Redaction:     redaction,
		Profiles:      redactedProfiles,
	}
	previewPath := filepath.Join(opts.out, "ai_input_preview.json")
	if err := report.WriteAIPreview(preview, previewPath); err != nil {
		return err
	}

	report.PrintConsole(cmd.OutOrStdout(), rep)
	fmt.Fprintf(cmd.OutOrStdout(), "  report:             %s\n", jsonPath)
	fmt.Fprintf(cmd.OutOrStdout(), "  ai_input_preview:   %s (redacted_values=%d)\n", previewPath, redaction.RedactedValueCount)
	return nil
}
