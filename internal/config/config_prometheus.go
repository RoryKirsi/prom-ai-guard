package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// PrometheusConfig mirrors configs/prometheus.yaml. It configures the read-only
// Prometheus HTTP API data source. Authentication is none in this version.
type PrometheusConfig struct {
	BaseURL        string `yaml:"base_url"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	MaxSeries      int    `yaml:"max_series"`
	MaxMetricNames int    `yaml:"max_metric_names"`
	BatchSize      int    `yaml:"batch_size"`
}

// DefaultPrometheusConfig returns built-in defaults (used when the file is
// absent or a field is empty). base_url is empty by default and must be supplied
// via configs/prometheus.yaml or --prom-url.
func DefaultPrometheusConfig() PrometheusConfig {
	return PrometheusConfig{
		BaseURL:        "",
		TimeoutSeconds: 30,
		MaxSeries:      100000,
		MaxMetricNames: 100000,
		BatchSize:      50,
	}
}

// LoadPrometheus reads configs/prometheus.yaml, overlaying it on the defaults. A
// missing file is not an error — defaults are returned.
func LoadPrometheus(path string) (PrometheusConfig, error) {
	cfg := DefaultPrometheusConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading prometheus config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing prometheus config %s: %w", path, err)
	}
	return applyPromDefaults(cfg), nil
}

func applyPromDefaults(cfg PrometheusConfig) PrometheusConfig {
	d := DefaultPrometheusConfig()
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = d.TimeoutSeconds
	}
	if cfg.MaxSeries < 0 {
		cfg.MaxSeries = d.MaxSeries
	}
	if cfg.MaxMetricNames < 0 {
		cfg.MaxMetricNames = d.MaxMetricNames
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = d.BatchSize
	}
	return cfg
}
