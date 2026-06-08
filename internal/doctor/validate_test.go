package doctor

import "testing"

func TestValidateReportValid(t *testing.T) {
	body := `{"schema_version":"v1","invalid_metrics":[{"metric_name":"a","risk_score":90,"invalid_types":["high_cardinality"]}]}`
	if err := ValidateReport([]byte(body)); err != nil {
		t.Fatalf("valid report rejected: %v", err)
	}
}

func TestValidateReportNoSummaryRequired(t *testing.T) {
	// Doctor does not require summary fields.
	body := `{"schema_version":"v1","invalid_metrics":[]}`
	if err := ValidateReport([]byte(body)); err != nil {
		t.Errorf("summary must not be required by doctor: %v", err)
	}
}

func TestValidateReportRejects(t *testing.T) {
	cases := map[string]string{
		"missing schema_version":    `{"invalid_metrics":[]}`,
		"wrong schema_version":      `{"schema_version":"v2","invalid_metrics":[]}`,
		"missing invalid_metrics":   `{"schema_version":"v1"}`,
		"invalid_metrics not array": `{"schema_version":"v1","invalid_metrics":{}}`,
		"empty metric_name":         `{"schema_version":"v1","invalid_metrics":[{"metric_name":""}]}`,
		"missing metric_name":       `{"schema_version":"v1","invalid_metrics":[{"risk_score":1}]}`,
		"top-level not object":      `[]`,
		"malformed":                 `{not json`,
	}
	for name, body := range cases {
		if err := ValidateReport([]byte(body)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}
