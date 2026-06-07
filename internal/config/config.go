// Package config loads the rules and service-inventory configuration and
// computes a deterministic config hash for the report (contract §4.3/§4.4).
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Rules mirrors configs/rules.yaml.
type Rules struct {
	SchemaVersion string `yaml:"schema_version"`
	Thresholds    struct {
		HighCardinalityLabelValues  int `yaml:"high_cardinality_label_values"`
		HighCardinalityMetricSeries int `yaml:"high_cardinality_metric_series"`
	} `yaml:"thresholds"`
	Patterns struct {
		DeprecatedMetricNames []string `yaml:"deprecated_metric_names"`
		DebugMetricNames      []string `yaml:"debug_metric_names"`
		ForbiddenLabelKeys    []string `yaml:"forbidden_label_keys"`
	} `yaml:"patterns"`
}

// Service is one entry in service_inventory.yaml.
type Service struct {
	Namespace   string   `yaml:"namespace"`
	Service     string   `yaml:"service"`
	Jobs        []string `yaml:"jobs"`
	Owner       string   `yaml:"owner"`
	Team        string   `yaml:"team"`
	Aliases     []string `yaml:"aliases"`
	Criticality string   `yaml:"criticality"`
}

// Inventory mirrors configs/service_inventory.yaml.
type Inventory struct {
	SchemaVersion string    `yaml:"schema_version"`
	Services      []Service `yaml:"services"`
}

// LoadRules reads and parses a rules.yaml file.
func LoadRules(path string) (Rules, error) {
	var r Rules
	data, err := os.ReadFile(path)
	if err != nil {
		return r, fmt.Errorf("reading rules config: %w", err)
	}
	if err := yaml.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("parsing rules config %s: %w", path, err)
	}
	return r, nil
}

// LoadInventory reads and parses a service_inventory.yaml file.
func LoadInventory(path string) (Inventory, error) {
	var inv Inventory
	data, err := os.ReadFile(path)
	if err != nil {
		return inv, fmt.Errorf("reading service inventory: %w", err)
	}
	if err := yaml.Unmarshal(data, &inv); err != nil {
		return inv, fmt.Errorf("parsing service inventory %s: %w", path, err)
	}
	return inv, nil
}

// HashFiles returns sha256:<hex> over the concatenated raw bytes of the given
// files in the order provided. Used to stamp config_hash into the report.
func HashFiles(paths ...string) (string, error) {
	h := sha256.New()
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("hashing config %s: %w", p, err)
		}
		h.Write(data)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
