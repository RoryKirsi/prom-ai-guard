package report

import (
	"prom-ai-guard/internal/profile"
	"prom-ai-guard/internal/redact"
)

// AIInputPreview is the redacted AI input artifact (ai_input_preview.json). It
// is a generated artifact for inspection, separate from the analysis report,
// and shows exactly what would be sent to the LLM after redaction.
type AIInputPreview struct {
	SchemaVersion string                  `json:"schema_version"`
	ScanID        string                  `json:"scan_id"`
	Redaction     redact.Info             `json:"redaction"`
	Profiles      []profile.MetricProfile `json:"profiles"`
}

// WriteAIPreview writes the AI input preview as pretty-printed JSON.
func WriteAIPreview(p AIInputPreview, path string) error {
	return writeJSON(p, path)
}
