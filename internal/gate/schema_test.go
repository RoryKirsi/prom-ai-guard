package gate

import "testing"

const validReport = `{
  "schema_version": "v1",
  "source": {},
  "ai": {"fallback_used": false},
  "summary": {
    "invalid_ratio": 0.1,
    "risk_distribution": {"severe": 0, "warning": 0, "minor": 0},
    "invalid_type_counts": {"high_cardinality": 0}
  },
  "invalid_metrics": [],
  "top_violation_labels": [],
  "warnings": []
}`

func TestValidateReportSchemaValid(t *testing.T) {
	if err := ValidateReportSchema([]byte(validReport)); err != nil {
		t.Fatalf("valid report rejected: %v", err)
	}
}

func TestValidateReportSchemaAIOptional(t *testing.T) {
	noAI := `{"schema_version":"v1","summary":{"invalid_ratio":0.1,"risk_distribution":{},"invalid_type_counts":{}},"invalid_metrics":[],"top_violation_labels":[]}`
	if err := ValidateReportSchema([]byte(noAI)); err != nil {
		t.Errorf("missing ai block must be allowed: %v", err)
	}
}

func TestValidateReportSchemaRejects(t *testing.T) {
	cases := map[string]string{
		"missing schema_version":  `{"summary":{"invalid_ratio":0.1,"risk_distribution":{},"invalid_type_counts":{}},"invalid_metrics":[],"top_violation_labels":[]}`,
		"wrong schema_version":    `{"schema_version":"v2","summary":{"invalid_ratio":0.1,"risk_distribution":{},"invalid_type_counts":{}},"invalid_metrics":[],"top_violation_labels":[]}`,
		"missing summary":         `{"schema_version":"v1","invalid_metrics":[],"top_violation_labels":[]}`,
		"missing risk_dist":       `{"schema_version":"v1","summary":{"invalid_ratio":0.1,"invalid_type_counts":{}},"invalid_metrics":[],"top_violation_labels":[]}`,
		"missing type_counts":     `{"schema_version":"v1","summary":{"invalid_ratio":0.1,"risk_distribution":{}},"invalid_metrics":[],"top_violation_labels":[]}`,
		"missing invalid_ratio":   `{"schema_version":"v1","summary":{"risk_distribution":{},"invalid_type_counts":{}},"invalid_metrics":[],"top_violation_labels":[]}`,
		"invalid_ratio not num":   `{"schema_version":"v1","summary":{"invalid_ratio":"x","risk_distribution":{},"invalid_type_counts":{}},"invalid_metrics":[],"top_violation_labels":[]}`,
		"invalid_metrics object":  `{"schema_version":"v1","summary":{"invalid_ratio":0.1,"risk_distribution":{},"invalid_type_counts":{}},"invalid_metrics":{},"top_violation_labels":[]}`,
		"top_violation missing":   `{"schema_version":"v1","summary":{"invalid_ratio":0.1,"risk_distribution":{},"invalid_type_counts":{}},"invalid_metrics":[]}`,
		"top_violation not array": `{"schema_version":"v1","summary":{"invalid_ratio":0.1,"risk_distribution":{},"invalid_type_counts":{}},"invalid_metrics":[],"top_violation_labels":{}}`,
		"ai fallback not bool":    `{"schema_version":"v1","ai":{"fallback_used":"yes"},"summary":{"invalid_ratio":0.1,"risk_distribution":{},"invalid_type_counts":{}},"invalid_metrics":[],"top_violation_labels":[]}`,
		"top-level not object":    `[]`,
	}
	for name, body := range cases {
		if err := ValidateReportSchema([]byte(body)); err == nil {
			t.Errorf("%s: expected schema error, got nil", name)
		}
	}
}
