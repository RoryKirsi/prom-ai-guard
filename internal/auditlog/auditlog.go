// Package auditlog writes a safe, structured JSONL audit trail for a scan
// (reports/scan.log.jsonl). It is secondary to the scan: open/write failures
// warn once to stderr and are otherwise non-fatal. The Event schema is typed and
// closed — there is deliberately no field for an API key, Authorization header,
// raw LLM prompt/response, raw MetricProfile payload, or raw label samples, so
// such data cannot be logged by construction.
package auditlog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

// Level values.
const (
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

const reasonMaxLen = 300

// Event is the closed set of safe fields an audit line may carry. Numeric/bool
// fields are pointers so a real 0/false is emitted (not dropped by omitempty).
type Event struct {
	Timestamp string `json:"timestamp"`
	ScanID    string `json:"scan_id"`
	Level     string `json:"level"`
	Event     string `json:"event"`

	SourceType string `json:"source_type,omitempty"`
	PromURL    string `json:"prom_url,omitempty"`
	InputRef   string `json:"input_ref,omitempty"`
	ScanScope  string `json:"scan_scope,omitempty"`

	SeriesCount      *int           `json:"series_count,omitempty"`
	MetricNameCount  *int           `json:"metric_name_count,omitempty"`
	InvalidCount     *int           `json:"invalid_count,omitempty"`
	ParseWarnings    *int           `json:"parse_warnings,omitempty"`
	RiskDistribution map[string]int `json:"risk_distribution,omitempty"`
	InvalidRatio     *float64       `json:"invalid_ratio,omitempty"`

	AIMode                string `json:"ai_mode,omitempty"`
	AIScope               string `json:"ai_scope,omitempty"`
	AIStatus              string `json:"ai_status,omitempty"`
	FallbackUsed          *bool  `json:"fallback_used,omitempty"`
	PartialFallbackUsed   *bool  `json:"partial_fallback_used,omitempty"`
	FallbackReason        string `json:"fallback_reason,omitempty"`
	AnalyzedMetricCount   *int   `json:"analyzed_metric_count,omitempty"`
	LLMInScopeMetricCount *int   `json:"llm_in_scope_metric_count,omitempty"`

	BatchSize         *int `json:"batch_size,omitempty"`
	BatchCount        *int `json:"batch_count,omitempty"`
	SuccessfulBatches *int `json:"successful_batches,omitempty"`
	FailedBatches     *int `json:"failed_batches,omitempty"`
	BatchIndex        *int `json:"batch_index,omitempty"`
	MetricCount       *int `json:"metric_count,omitempty"`

	Reason     string `json:"reason,omitempty"`
	ReportPath string `json:"report_path,omitempty"`
	Artifact   string `json:"artifact,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
}

// Logger writes one JSON object per line. A nil *Logger is a valid no-op, so the
// caller can keep emitting events even when the log could not be opened.
type Logger struct {
	w      io.Writer
	stderr io.Writer
	scanID string
	warned bool
}

// New returns a Logger writing JSONL to w. stderr receives at most one warning if
// a subsequent write fails.
func New(w, stderr io.Writer, scanID string) *Logger {
	return &Logger{w: w, stderr: stderr, scanID: scanID}
}

// emit fills the common fields and writes one line. A write error produces at
// most one stderr warning; further write errors are dropped silently.
func (l *Logger) emit(level, event string, e Event) {
	if l == nil || l.w == nil {
		return
	}
	e.Timestamp = time.Now().UTC().Format(time.RFC3339)
	e.ScanID = l.scanID
	e.Level = level
	e.Event = event
	b, err := json.Marshal(e)
	if err == nil {
		b = append(b, '\n')
		_, err = l.w.Write(b)
	}
	if err != nil && !l.warned {
		l.warned = true
		if l.stderr != nil {
			fmt.Fprintf(l.stderr, "warning: scan.log.jsonl write failed; continuing without further audit logging: %v\n", err)
		}
	}
}

func intp(v int) *int           { return &v }
func boolp(v bool) *bool        { return &v }
func floatp(v float64) *float64 { return &v }

// sanitizeURL returns scheme://host[:port] only — no userinfo, path, or query.
func sanitizeURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// sanitizeReason collapses whitespace/newlines to single spaces and caps length.
func sanitizeReason(msg string) string {
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > reasonMaxLen {
		msg = msg[:reasonMaxLen-len("…[truncated]")] + "…[truncated]"
	}
	return msg
}

// --- typed per-event methods (each emits only its safe fields) ---

func (l *Logger) ScanStarted(sourceType, promURL, inputRef, scanScope, aiMode, aiScope string, batchSize int) {
	l.emit(LevelInfo, "scan_started", Event{
		SourceType: sourceType, PromURL: sanitizeURL(promURL), InputRef: inputRef, ScanScope: scanScope,
		AIMode: aiMode, AIScope: aiScope, BatchSize: intp(batchSize),
	})
}

func (l *Logger) SourceReadStarted(sourceType, promURL, inputRef string) {
	l.emit(LevelInfo, "source_read_started", Event{
		SourceType: sourceType, PromURL: sanitizeURL(promURL), InputRef: inputRef,
	})
}

func (l *Logger) SourceReadCompleted(sourceType string, seriesCount int) {
	l.emit(LevelInfo, "source_read_completed", Event{SourceType: sourceType, SeriesCount: intp(seriesCount)})
}

func (l *Logger) ParseWarningsSummary(count int) {
	lvl := LevelInfo
	if count > 0 {
		lvl = LevelWarn
	}
	l.emit(lvl, "parse_warnings_summary", Event{ParseWarnings: intp(count)})
}

func (l *Logger) LocalRulesCompleted(metricNameCount, seriesCount, invalidCount int) {
	l.emit(LevelInfo, "local_rules_completed", Event{
		MetricNameCount: intp(metricNameCount), SeriesCount: intp(seriesCount), InvalidCount: intp(invalidCount),
	})
}

func (l *Logger) AIStarted(aiMode, aiScope string, batchSize int) {
	l.emit(LevelInfo, "ai_started", Event{AIMode: aiMode, AIScope: aiScope, BatchSize: intp(batchSize)})
}

func (l *Logger) AIBatchSummary(batchSize, batchCount, successful, failed int, status string) {
	l.emit(LevelInfo, "ai_batch_summary", Event{
		BatchSize: intp(batchSize), BatchCount: intp(batchCount),
		SuccessfulBatches: intp(successful), FailedBatches: intp(failed), AIStatus: status,
	})
}

func (l *Logger) AIBatchFailure(batchIndex, metricCount int, reason string) {
	l.emit(LevelWarn, "ai_batch_failure", Event{
		BatchIndex: intp(batchIndex), MetricCount: intp(metricCount), Reason: sanitizeReason(reason),
	})
}

func (l *Logger) AICompleted(status string, analyzed, inScope int, fallbackUsed, partialFallbackUsed bool, fallbackReason string) {
	l.emit(LevelInfo, "ai_completed", Event{
		AIStatus: status, AnalyzedMetricCount: intp(analyzed), LLMInScopeMetricCount: intp(inScope),
		FallbackUsed: boolp(fallbackUsed), PartialFallbackUsed: boolp(partialFallbackUsed),
		FallbackReason: fallbackReason,
	})
}

func (l *Logger) ReportWritten(artifact, path string) {
	l.emit(LevelInfo, "report_written", Event{Artifact: artifact, ReportPath: path})
}

func (l *Logger) ScanCompleted(invalidCount int, risk map[string]int, ratio float64, aiStatus string) {
	l.emit(LevelInfo, "scan_completed", Event{
		InvalidCount: intp(invalidCount), RiskDistribution: risk, InvalidRatio: floatp(ratio),
		AIStatus: aiStatus, ExitCode: intp(0),
	})
}

func (l *Logger) ScanFailed(reason string) {
	l.emit(LevelError, "scan_failed", Event{Reason: sanitizeReason(reason), ExitCode: intp(1)})
}
