package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteJSON writes the report as pretty-printed JSON to path, creating parent
// directories as needed. It returns the path written for logging.
func WriteJSON(r Report, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding report: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing report %s: %w", path, err)
	}
	return nil
}
