package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteJSON writes the analysis report as pretty-printed JSON to path, creating
// parent directories as needed.
func WriteJSON(r Report, path string) error {
	return writeJSON(r, path)
}

// writeJSON marshals any value as indented JSON to path, creating parent
// directories as needed. HTML escaping is disabled so values like the
// "<redacted>" placeholder are written literally rather than as <.
func writeJSON(v any, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
