package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// AIConfig mirrors configs/ai.yaml. It describes an OpenAI-compatible Chat
// Completions provider (DeepSeek by default). The API key is never stored here —
// only the name of the environment variable that holds it (api_key_env).
type AIConfig struct {
	Provider        string `yaml:"provider"`
	Mode            string `yaml:"mode"`
	Model           string `yaml:"model"`
	BaseURL         string `yaml:"base_url"`
	APIKeyEnv       string `yaml:"api_key_env"`
	MaxAttempts     int    `yaml:"max_attempts"`
	MaxPayloadBytes int    `yaml:"max_payload_bytes"`
	TimeoutSeconds  int    `yaml:"timeout_seconds"`
}

// DefaultAIConfig returns the built-in defaults used when configs/ai.yaml is
// absent or a field is left empty.
func DefaultAIConfig() AIConfig {
	return AIConfig{
		Provider:        "deepseek",
		Mode:            "deepseek_fullscan",
		Model:           "deepseek-v4-flash",
		BaseURL:         "https://api.deepseek.com",
		APIKeyEnv:       "DEEPSEEK_API_KEY",
		MaxAttempts:     2,
		MaxPayloadBytes: 262144,
		TimeoutSeconds:  30,
	}
}

// LoadAI reads configs/ai.yaml, overlaying it on the defaults. A missing file
// is not an error — defaults are returned so scans work without the file.
func LoadAI(path string) (AIConfig, error) {
	cfg := DefaultAIConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading ai config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing ai config %s: %w", path, err)
	}
	return applyAIDefaults(cfg), nil
}

func applyAIDefaults(cfg AIConfig) AIConfig {
	d := DefaultAIConfig()
	if cfg.Provider == "" {
		cfg.Provider = d.Provider
	}
	if cfg.Mode == "" {
		cfg.Mode = d.Mode
	}
	if cfg.Model == "" {
		cfg.Model = d.Model
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = d.BaseURL
	}
	if cfg.APIKeyEnv == "" {
		cfg.APIKeyEnv = d.APIKeyEnv
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = d.MaxAttempts
	}
	if cfg.MaxPayloadBytes <= 0 {
		cfg.MaxPayloadBytes = d.MaxPayloadBytes
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = d.TimeoutSeconds
	}
	return cfg
}

// SanitizedHash returns sha256:<hex> over a sanitized subset of the AI config.
// It deliberately excludes api_key_env and never touches environment values, so
// the resulting ai.config_hash carries no secret material.
func (c AIConfig) SanitizedHash() string {
	canonical := fmt.Sprintf("provider=%s\nmode=%s\nmodel=%s\nbase_url=%s\nmax_attempts=%d\nmax_payload_bytes=%d\ntimeout_seconds=%d\n",
		c.Provider, c.Mode, c.Model, c.BaseURL, c.MaxAttempts, c.MaxPayloadBytes, c.TimeoutSeconds)
	sum := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(sum[:])
}
