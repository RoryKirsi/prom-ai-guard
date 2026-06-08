package relabel

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// WriteYAML writes the relabel proposal to path, creating parent directories as
// needed. It only writes the file; it never applies the rules anywhere.
func WriteYAML(plan RelabelPlan, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	data, err := yaml.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encoding relabel rules: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
