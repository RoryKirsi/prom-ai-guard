// Package ai integrates an OpenAI-compatible Chat Completions LLM provider as
// the AI analyzer. The provider/model/base_url/api_key_env are configurable;
// DeepSeek (deepseek-v4-flash) is the default demo provider. Anthropic/Gemini
// or other vendor-specific APIs are out of scope — only OpenAI-compatible
// Chat Completions is supported.
//
// It sends only redacted MetricProfile data, refines/augments the local rule
// findings (root_cause, risk_reason, recommendations, confidence), and falls
// back deterministically to the local rule output. The API key lives only in
// the client's private field and is never serialized or logged. AI may add or
// upgrade findings but can never downgrade a deterministic rule severity.
package ai

// AI modes. llm_fullscan is the provider-neutral full-scan mode; local_rules
// disables the LLM. Any other value (e.g. hybrid, fallback_only, or the old
// deepseek_fullscan) is rejected by the caller.
const (
	ModeLLMFullScan = "llm_fullscan"
	ModeLocalRules  = "local_rules"
)

// AI scopes.
const (
	ScopeAll     = "all"
	ScopeInvalid = "invalid"
)

// AI status values.
const (
	StatusSuccess  = "success"
	StatusPartial  = "partial"
	StatusFallback = "fallback"
	StatusDisabled = "disabled"
)

// analysis_method values.
const (
	MethodLLMFullScan        = "llm_fullscan"
	MethodLocalRulesFallback = "local_rules_fallback"
	MethodDisabled           = "disabled"
)

// Analysis source tags for MetricAnalysis.AnalysisSources. The LLM tag is
// provider-neutral ("llm"); the actual provider is recorded in ai.provider.
const (
	SourceLocalRules = "local_rules"
	SourceLLM        = "llm"
)

// Info is the top-level `ai` block written into analysis_report.json. It never
// contains the API key or any environment value.
type Info struct {
	Provider                    string `json:"provider"`
	Model                       string `json:"model"`
	BaseURL                     string `json:"base_url"`
	AIMode                      string `json:"ai_mode"`
	AIScope                     string `json:"ai_scope"`
	Enabled                     bool   `json:"enabled"`
	Status                      string `json:"status"`
	FallbackReason              string `json:"fallback_reason"`
	AnalysisMethod              string `json:"analysis_method"`
	AttemptCount                int    `json:"attempt_count"`
	FallbackUsed                bool   `json:"fallback_used"`
	RedactionEnabled            bool   `json:"redaction_enabled"`
	AnalyzedMetricCount         int    `json:"analyzed_metric_count"`
	Summary                     string `json:"summary"`
	HistoricalComparisonSummary string `json:"historical_comparison_summary"`
	ConfigHash                  string `json:"config_hash"`
}
