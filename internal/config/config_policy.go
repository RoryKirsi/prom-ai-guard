package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Policy mirrors configs/policy.yaml. It drives the deterministic CI/CD Gate.
type Policy struct {
	SchemaVersion string     `yaml:"schema_version"`
	Gate          GatePolicy `yaml:"gate"`
}

// GatePolicy holds the gate thresholds. Numeric thresholds are pointers so an
// absent key means "no check" (a present 0 still enforces). Bools default false.
type GatePolicy struct {
	FailOnSchemaError         bool     `yaml:"fail_on_schema_error"`
	FailOnFallbackUsed        bool     `yaml:"fail_on_fallback_used"`
	MaxSevere                 *int     `yaml:"max_severe"`
	MaxWarning                *int     `yaml:"max_warning"`
	MaxInvalidRatio           *float64 `yaml:"max_invalid_ratio"`
	MaxHighCardinalityMetrics *int     `yaml:"max_high_cardinality_metrics"`
	ForbiddenLabelKeys        []string `yaml:"forbidden_label_keys"`
}

// LoadPolicy reads and parses a policy.yaml file. A missing or malformed file is
// an error — the Gate requires a policy (caller treats it as a tool/config error).
func LoadPolicy(path string) (Policy, error) {
	var p Policy
	data, err := os.ReadFile(path)
	if err != nil {
		return p, fmt.Errorf("reading policy: %w", err)
	}
	if err := yaml.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("parsing policy %s: %w", path, err)
	}
	return p, nil
}
