package parser

import (
	"math"
	"strings"
	"testing"
)

func TestParseValidLineWithLabels(t *testing.T) {
	in := `http_requests_total{method="POST",code="200"} 1027`
	series, warns, err := ParseReader(strings.NewReader(in))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("expected no warnings, got %v", warns)
	}
	if len(series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(series))
	}
	s := series[0]
	if s.MetricName != "http_requests_total" {
		t.Errorf("metric name = %q", s.MetricName)
	}
	if s.Value != 1027 {
		t.Errorf("value = %v", s.Value)
	}
	if s.Labels["method"] != "POST" || s.Labels["code"] != "200" {
		t.Errorf("labels = %v", s.Labels)
	}
}

func TestParseValidLineNoLabels(t *testing.T) {
	in := "process_cpu_seconds_total 12.34"
	series, warns, _ := ParseReader(strings.NewReader(in))
	if len(warns) != 0 {
		t.Fatalf("expected no warnings, got %v", warns)
	}
	if len(series) != 1 || series[0].MetricName != "process_cpu_seconds_total" {
		t.Fatalf("series = %+v", series)
	}
	if len(series[0].Labels) != 0 {
		t.Errorf("expected no labels, got %v", series[0].Labels)
	}
	if series[0].Value != 12.34 {
		t.Errorf("value = %v", series[0].Value)
	}
}

func TestParseSkipsCommentsAndBlanks(t *testing.T) {
	in := `# HELP http_requests_total The total number of HTTP requests.
# TYPE http_requests_total counter

http_requests_total{method="GET"} 5
`
	series, warns, _ := ParseReader(strings.NewReader(in))
	if len(warns) != 0 {
		t.Fatalf("expected no warnings, got %v", warns)
	}
	if len(series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(series))
	}
}

func TestMalformedLineWarnsButContinues(t *testing.T) {
	in := `good_metric 1
this is not a metric line
another_good{a="b"} 2
http_requests_total{method=} 3
`
	series, warns, _ := ParseReader(strings.NewReader(in))
	if len(series) != 2 {
		t.Fatalf("expected 2 valid series, got %d: %+v", len(series), series)
	}
	if len(warns) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %+v", len(warns), warns)
	}
	// Warning line numbers should point at the offending input lines (2 and 4).
	if warns[0].Line != 2 {
		t.Errorf("first warning line = %d, want 2", warns[0].Line)
	}
	if warns[1].Line != 4 {
		t.Errorf("second warning line = %d, want 4", warns[1].Line)
	}
}

func TestParseEscapedQuotesInLabelValue(t *testing.T) {
	in := `msg_total{text="he said \"hi\"",path="a\\b"} 1`
	series, warns, _ := ParseReader(strings.NewReader(in))
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if series[0].Labels["text"] != `he said "hi"` {
		t.Errorf("text label = %q", series[0].Labels["text"])
	}
	if series[0].Labels["path"] != `a\b` {
		t.Errorf("path label = %q", series[0].Labels["path"])
	}
}

func TestParseValueFormsAndTimestamp(t *testing.T) {
	cases := map[string]float64{
		"m 1.5e3":                    1500,
		"m{a=\"b\"} 2 1700000000000": 2, // trailing timestamp is ignored
		"m -3":                       -3,
		"m +Inf":                     math.Inf(1),
	}
	for in, want := range cases {
		series, warns, _ := ParseReader(strings.NewReader(in))
		if len(warns) != 0 {
			t.Errorf("%q: unexpected warnings %v", in, warns)
			continue
		}
		if len(series) != 1 {
			t.Errorf("%q: expected 1 series, got %d", in, len(series))
			continue
		}
		got := series[0].Value
		if math.IsInf(want, 1) {
			if !math.IsInf(got, 1) {
				t.Errorf("%q: value = %v, want +Inf", in, got)
			}
			continue
		}
		if got != want {
			t.Errorf("%q: value = %v, want %v", in, got, want)
		}
	}
}

func TestParseOptionalTimestamp(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantSrs  int
		wantWarn int
	}{
		{"no timestamp", "m 1", 1, 0},
		{"integer timestamp", "m 1 1700000000000", 1, 0},
		{"non-integer timestamp", "m 1 1.5", 0, 1},
		{"extra token after timestamp", "m 1 1700000000000 extra", 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			series, warns, _ := ParseReader(strings.NewReader(tc.in))
			if len(series) != tc.wantSrs {
				t.Errorf("series = %d, want %d (%+v)", len(series), tc.wantSrs, series)
			}
			if len(warns) != tc.wantWarn {
				t.Errorf("warnings = %d, want %d (%+v)", len(warns), tc.wantWarn, warns)
			}
		})
	}
}

func TestParseEmptyLabelValueIsValid(t *testing.T) {
	// Empty label values are legal Prometheus syntax; flagging them as invalid
	// is a rule concern for a later slice, not a parse error.
	in := `q_total{env=""} 1`
	series, warns, _ := ParseReader(strings.NewReader(in))
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if v, ok := series[0].Labels["env"]; !ok || v != "" {
		t.Errorf("env label = %q present=%v", v, ok)
	}
}
