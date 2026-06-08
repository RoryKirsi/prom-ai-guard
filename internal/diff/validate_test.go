package diff

import (
	"strings"
	"testing"
)

const validReport = `{
  "schema_version": "v1",
  "summary": {
    "invalid_metric_names": 2,
    "total_metric_names": 10,
    "invalid_ratio": 0.2,
    "risk_distribution": {"severe": 1, "warning": 1, "minor": 0},
    "invalid_type_counts": {"high_cardinality": 1}
  },
  "invalid_metrics": [
    {"metric_name": "a", "risk_score": 90, "risk_level": "severe", "invalid_types": ["high_cardinality"]},
    {"metric_name": "b", "risk_score": 50, "risk_level": "warning", "invalid_types": ["deprecated_metric"]}
  ]
}`

func TestValidateReportValid(t *testing.T) {
	if err := ValidateReport([]byte(validReport)); err != nil {
		t.Fatalf("valid report rejected: %v", err)
	}
}

func TestValidateReportRejects(t *testing.T) {
	cases := map[string]string{
		"missing schema_version":       `{"summary":{"invalid_metric_names":1,"total_metric_names":1,"invalid_ratio":0.1,"risk_distribution":{"severe":0,"warning":0,"minor":0}},"invalid_metrics":[]}`,
		"wrong schema_version":         `{"schema_version":"v2","summary":{"invalid_metric_names":1,"total_metric_names":1,"invalid_ratio":0.1,"risk_distribution":{"severe":0,"warning":0,"minor":0}},"invalid_metrics":[]}`,
		"missing summary":              `{"schema_version":"v1","invalid_metrics":[]}`,
		"missing invalid_metric_names": `{"schema_version":"v1","summary":{"total_metric_names":1,"invalid_ratio":0.1,"risk_distribution":{"severe":0,"warning":0,"minor":0}},"invalid_metrics":[]}`,
		"missing total_metric_names":   `{"schema_version":"v1","summary":{"invalid_metric_names":1,"invalid_ratio":0.1,"risk_distribution":{"severe":0,"warning":0,"minor":0}},"invalid_metrics":[]}`,
		"missing invalid_ratio":        `{"schema_version":"v1","summary":{"invalid_metric_names":1,"total_metric_names":1,"risk_distribution":{"severe":0,"warning":0,"minor":0}},"invalid_metrics":[]}`,
		"invalid_ratio not number":     `{"schema_version":"v1","summary":{"invalid_metric_names":1,"total_metric_names":1,"invalid_ratio":"x","risk_distribution":{"severe":0,"warning":0,"minor":0}},"invalid_metrics":[]}`,
		"invalid_ratio null":           `{"schema_version":"v1","summary":{"invalid_metric_names":1,"total_metric_names":1,"invalid_ratio":null,"risk_distribution":{"severe":0,"warning":0,"minor":0}},"invalid_metrics":[]}`,
		"missing risk_distribution":    `{"schema_version":"v1","summary":{"invalid_metric_names":1,"total_metric_names":1,"invalid_ratio":0.1},"invalid_metrics":[]}`,
		"missing severe":               `{"schema_version":"v1","summary":{"invalid_metric_names":1,"total_metric_names":1,"invalid_ratio":0.1,"risk_distribution":{"warning":0,"minor":0}},"invalid_metrics":[]}`,
		"missing warning":              `{"schema_version":"v1","summary":{"invalid_metric_names":1,"total_metric_names":1,"invalid_ratio":0.1,"risk_distribution":{"severe":0,"minor":0}},"invalid_metrics":[]}`,
		"missing minor":                `{"schema_version":"v1","summary":{"invalid_metric_names":1,"total_metric_names":1,"invalid_ratio":0.1,"risk_distribution":{"severe":0,"warning":0}},"invalid_metrics":[]}`,
		"invalid_metrics not array":    `{"schema_version":"v1","summary":{"invalid_metric_names":1,"total_metric_names":1,"invalid_ratio":0.1,"risk_distribution":{"severe":0,"warning":0,"minor":0}},"invalid_metrics":{}}`,
		"empty metric_name":            `{"schema_version":"v1","summary":{"invalid_metric_names":1,"total_metric_names":1,"invalid_ratio":0.1,"risk_distribution":{"severe":0,"warning":0,"minor":0}},"invalid_metrics":[{"metric_name":"","risk_score":1,"invalid_types":[]}]}`,
		"missing risk_score":           `{"schema_version":"v1","summary":{"invalid_metric_names":1,"total_metric_names":1,"invalid_ratio":0.1,"risk_distribution":{"severe":0,"warning":0,"minor":0}},"invalid_metrics":[{"metric_name":"a","invalid_types":[]}]}`,
		"risk_score not number":        `{"schema_version":"v1","summary":{"invalid_metric_names":1,"total_metric_names":1,"invalid_ratio":0.1,"risk_distribution":{"severe":0,"warning":0,"minor":0}},"invalid_metrics":[{"metric_name":"a","risk_score":"hi","invalid_types":[]}]}`,
		"missing invalid_types":        `{"schema_version":"v1","summary":{"invalid_metric_names":1,"total_metric_names":1,"invalid_ratio":0.1,"risk_distribution":{"severe":0,"warning":0,"minor":0}},"invalid_metrics":[{"metric_name":"a","risk_score":1}]}`,
		"invalid_types not array":      `{"schema_version":"v1","summary":{"invalid_metric_names":1,"total_metric_names":1,"invalid_ratio":0.1,"risk_distribution":{"severe":0,"warning":0,"minor":0}},"invalid_metrics":[{"metric_name":"a","risk_score":1,"invalid_types":"x"}]}`,
		"duplicate metric_name":        `{"schema_version":"v1","summary":{"invalid_metric_names":2,"total_metric_names":2,"invalid_ratio":0.1,"risk_distribution":{"severe":0,"warning":0,"minor":0}},"invalid_metrics":[{"metric_name":"a","risk_score":1,"invalid_types":[]},{"metric_name":"a","risk_score":2,"invalid_types":[]}]}`,
		"top-level not object":         `[]`,
	}
	for name, body := range cases {
		if err := ValidateReport([]byte(body)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestValidateReportDuplicateMessage(t *testing.T) {
	body := `{"schema_version":"v1","summary":{"invalid_metric_names":2,"total_metric_names":2,"invalid_ratio":0.1,"risk_distribution":{"severe":0,"warning":0,"minor":0}},"invalid_metrics":[{"metric_name":"dup","risk_score":1,"invalid_types":[]},{"metric_name":"dup","risk_score":2,"invalid_types":[]}]}`
	err := ValidateReport([]byte(body))
	if err == nil || !strings.Contains(err.Error(), "duplicate metric_name") {
		t.Fatalf("want duplicate metric_name error, got %v", err)
	}
}
