package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"prom-ai-guard/internal/ai"
	"prom-ai-guard/internal/config"
	"prom-ai-guard/internal/model"
	"prom-ai-guard/internal/parser"
	"prom-ai-guard/internal/profile"
	"prom-ai-guard/internal/promapi"
	"prom-ai-guard/internal/redact"
	"prom-ai-guard/internal/report"
	"prom-ai-guard/internal/rules"
	"prom-ai-guard/internal/scan"
	"prom-ai-guard/internal/storage"
	"prom-ai-guard/internal/tsdb"
)

// sampleValuesPerLabel bounds how many sample label values a MetricProfile
// carries per label key in the AI input preview.
const sampleValuesPerLabel = 5

type scanOptions struct {
	source      string
	input       string
	out         string
	config      string
	scanScope   string
	aiMode      string
	aiScope     string
	model       string
	baseURL     string
	aiBatchSize int

	// prometheus_api source
	promURL        string
	match          []string
	start          string
	end            string
	maxSeries      int
	maxMetricNames int
	promTimeout    int
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
	f.StringVar(&opts.source, "source", "file", "data source: file or prometheus_api")
	f.StringVar(&opts.input, "input", "", "path to a local Prometheus text metrics file (required for source=file)")
	f.StringVar(&opts.out, "out", "reports", "directory for generated reports")
	f.StringVar(&opts.config, "config", "configs", "directory holding rules.yaml, service_inventory.yaml, ai.yaml")
	f.StringVar(&opts.scanScope, "scan-scope", "all", "scan scope: all or filtered")
	f.StringVar(&opts.aiMode, "ai-mode", "llm_fullscan", "AI mode: llm_fullscan or local_rules")
	f.StringVar(&opts.aiScope, "ai-scope", "all", "AI scope: all or invalid")
	f.StringVar(&opts.model, "model", "", "LLM model, OpenAI-compatible (overrides ai.yaml)")
	f.StringVar(&opts.baseURL, "base-url", "", "LLM base URL, OpenAI-compatible (overrides ai.yaml)")
	f.IntVar(&opts.aiBatchSize, "ai-batch-size", 0, "LLM FullScan batch size (0 = use ai.yaml/default 50; overrides ai.yaml)")
	// prometheus_api source (read-only; metadata-oriented; auth none in this version)
	f.StringVar(&opts.promURL, "prom-url", "", "Prometheus base URL (required for source=prometheus_api; overrides prometheus.yaml)")
	f.StringArrayVar(&opts.match, "match", nil, "optional Prometheus series matcher(s); presence makes scan_scope=filtered (repeatable)")
	f.StringVar(&opts.start, "start", "", "optional start time passed to /api/v1/series")
	f.StringVar(&opts.end, "end", "", "optional end time passed to /api/v1/series")
	f.IntVar(&opts.maxSeries, "max-series", 100000, "guardrail: fail if fetched series exceed this (0 = unlimited)")
	f.IntVar(&opts.maxMetricNames, "max-metric-names", 100000, "guardrail: fail if metric-name enumeration exceeds this (0 = unlimited)")
	f.IntVar(&opts.promTimeout, "prom-timeout-seconds", 30, "HTTP timeout for Prometheus API requests")
	return cmd
}

func runScan(cmd *cobra.Command, opts *scanOptions) error {
	if opts.source != "file" && opts.source != "prometheus_api" {
		return fmt.Errorf("--source %q is invalid; allowed values are %q or %q", opts.source, "file", "prometheus_api")
	}
	if opts.source == "file" && opts.input == "" {
		return fmt.Errorf("--input is required when --source=file")
	}
	if opts.scanScope != "all" && opts.scanScope != "filtered" {
		return fmt.Errorf("--scan-scope %q is invalid; allowed values are %q or %q", opts.scanScope, "all", "filtered")
	}
	if opts.aiMode != ai.ModeLLMFullScan && opts.aiMode != ai.ModeLocalRules {
		return fmt.Errorf("--ai-mode %q is not supported; allowed values are %q or %q", opts.aiMode, ai.ModeLLMFullScan, ai.ModeLocalRules)
	}
	if opts.aiScope != ai.ScopeAll && opts.aiScope != ai.ScopeInvalid {
		return fmt.Errorf("--ai-scope %q is invalid; allowed values are %q or %q", opts.aiScope, ai.ScopeAll, ai.ScopeInvalid)
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
	// Top-level config_hash stays rules.yaml + service_inventory.yaml only.
	configHash, err := config.HashFiles(rulesPath, invPath)
	if err != nil {
		return err
	}
	aiCfg, err := config.LoadAI(filepath.Join(opts.config, "ai.yaml"))
	if err != nil {
		return err
	}
	if opts.model != "" {
		aiCfg.Model = opts.model
	}
	if opts.baseURL != "" {
		aiCfg.BaseURL = opts.baseURL
	}
	if cmd.Flags().Changed("ai-batch-size") {
		aiCfg.BatchSize = opts.aiBatchSize
	}

	// Acquire metric series from the selected source. On any source error we
	// return before writing anything, so no partial report is produced.
	var series []model.MetricSeries
	var warns []model.ParseWarning
	sourceScanScope := opts.scanScope
	promURL := ""

	switch opts.source {
	case "file":
		f, oerr := os.Open(opts.input)
		if oerr != nil {
			return fmt.Errorf("opening input: %w", oerr)
		}
		defer f.Close()
		series, warns, err = parser.ParseReader(f)
		if err != nil {
			return err
		}
	case "prometheus_api":
		promCfg, perr := config.LoadPrometheus(filepath.Join(opts.config, "prometheus.yaml"))
		if perr != nil {
			return perr
		}
		promURL = promCfg.BaseURL
		if opts.promURL != "" {
			promURL = opts.promURL
		}
		if promURL == "" {
			return fmt.Errorf("--prom-url (or base_url in prometheus.yaml) is required when --source=prometheus_api")
		}
		maxSeries := promCfg.MaxSeries
		if cmd.Flags().Changed("max-series") {
			maxSeries = opts.maxSeries
		}
		maxNames := promCfg.MaxMetricNames
		if cmd.Flags().Changed("max-metric-names") {
			maxNames = opts.maxMetricNames
		}
		timeoutSec := promCfg.TimeoutSeconds
		if cmd.Flags().Changed("prom-timeout-seconds") {
			timeoutSec = opts.promTimeout
		}
		client, cerr := promapi.NewClient(promURL, time.Duration(timeoutSec)*time.Second, maxSeries, maxNames, promCfg.BatchSize)
		if cerr != nil {
			return cerr
		}
		series, warns, err = client.FetchSeries(cmd.Context(), promapi.Options{Matchers: opts.match, Start: opts.start, End: opts.end})
		if err != nil {
			return err
		}
		// Derive scope from actual matchers, never imply filtering without them.
		sourceScanScope = "all"
		if len(opts.match) > 0 {
			sourceScanScope = "filtered"
		}
	}

	stats := tsdb.Build(series)
	invalids, contribs := rules.Evaluate(stats, rulesCfg, inventory)
	contexts := rules.Contexts(stats, inventory)

	analysesByName := make(map[string]model.MetricAnalysis, len(invalids))
	for _, a := range invalids {
		analysesByName[a.MetricName] = a
	}
	profiles := profile.Build(stats, analysesByName, contexts, sampleValuesPerLabel)
	redactedProfiles, redaction := redact.Profiles(profiles)

	now := time.Now().UTC()
	scanID := now.Format("20060102T150405Z") + "-scan"

	// AI analysis over the redacted profiles. Merged invalids drive the report.
	apiKey := os.Getenv(aiCfg.APIKeyEnv)
	var completer ai.Completer
	if opts.aiMode == ai.ModeLLMFullScan {
		client, cerr := ai.NewClient(aiCfg.BaseURL, aiCfg.Model, apiKey, time.Duration(aiCfg.TimeoutSeconds)*time.Second)
		if cerr != nil {
			return fmt.Errorf("ai client configuration: %w", cerr)
		}
		completer = client
	}
	analyzer := ai.Analyzer{
		Provider:         aiCfg.Provider,
		Model:            aiCfg.Model,
		BaseURL:          aiCfg.BaseURL,
		Mode:             opts.aiMode,
		Scope:            opts.aiScope,
		MaxAttempts:      aiCfg.MaxAttempts,
		BatchSize:        aiCfg.BatchSize,
		MaxPayloadBytes:  aiCfg.MaxPayloadBytes,
		ConfigHash:       aiCfg.SanitizedHash(),
		RedactionEnabled: true,
		KeyPresent:       apiKey != "",
		APIKeyEnvName:    aiCfg.APIKeyEnv,
		Completer:        completer,
	}
	aiResult := analyzer.Run(cmd.Context(), scanID, redactedProfiles, invalids)

	result := scan.Assemble(len(series), len(stats), aiResult.Invalids, contribs)

	// Slice 12: deterministic TSDB-index storage-impact simulation over the
	// invalid metrics (mutates them in place; nests the aggregate under summary).
	storageSummary := storage.Annotate(result.InvalidMetrics, storage.Thresholds{
		HighIndexEntries:       rulesCfg.StorageImpact.HighIndexEntries,
		MediumIndexEntries:     rulesCfg.StorageImpact.MediumIndexEntries,
		HighLabelCardinality:   rulesCfg.StorageImpact.HighLabelCardinality,
		MediumLabelCardinality: rulesCfg.StorageImpact.MediumLabelCardinality,
	})
	result.Summary.StorageImpact = &storageSummary

	aiInfo := aiResult.Info
	rep := report.Report{
		SchemaVersion: "v1",
		ScanID:        scanID,
		ScanTime:      now.Format(time.RFC3339),
		ToolVersion:   report.ToolVersion,
		ConfigHash:    configHash,
		Source: report.Source{
			SourceType:      opts.source,
			InputRef:        opts.input,
			PromURL:         promURL,
			ScanScope:       sourceScanScope,
			SeriesCount:     result.Summary.TotalSeries,
			MetricNameCount: result.Summary.TotalMetricNames,
		},
		AI:                 &aiInfo,
		Summary:            result.Summary,
		InvalidMetrics:     result.InvalidMetrics,
		TopRiskMetrics:     result.TopRiskMetrics,
		TopViolationLabels: result.TopViolationLabels,
		Warnings:           warns,
	}

	// JSON is the machine contract and is written first. Markdown and Excel are
	// rendered from the same rep object (no analysis re-run); a write failure
	// there is a tool error (exit 1) but the JSON artifact is already in place.
	jsonPath := filepath.Join(opts.out, "analysis_report.json")
	if err := report.WriteJSON(rep, jsonPath); err != nil {
		return err
	}
	mdPath := filepath.Join(opts.out, "analysis_report.md")
	if err := report.WriteMarkdown(rep, mdPath); err != nil {
		return err
	}
	xlsxPath := filepath.Join(opts.out, "analysis_report.xlsx")
	if err := report.WriteExcel(rep, xlsxPath); err != nil {
		return err
	}

	// Redacted AI input preview (separate artifact; never raw sensitive values).
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
	fmt.Fprintf(cmd.OutOrStdout(), "  report (json):      %s\n", jsonPath)
	fmt.Fprintf(cmd.OutOrStdout(), "  report (markdown):  %s\n", mdPath)
	fmt.Fprintf(cmd.OutOrStdout(), "  report (excel):     %s\n", xlsxPath)
	fmt.Fprintf(cmd.OutOrStdout(), "  ai_input_preview:   %s (redacted_values=%d)\n", previewPath, redaction.RedactedValueCount)
	return nil
}
