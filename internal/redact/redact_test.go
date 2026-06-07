package redact

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"prom-ai-guard/internal/profile"
)

func TestSensitiveKeyDetection(t *testing.T) {
	sensitive := []string{
		"token", "secret", "password", "cookie", "authorization", "apikey",
		"email", "user_id", "session_id", "trace_id", "request_id",
		// Normalized variants must also be caught.
		"api_key", "auth_token", "X-User-Id", "SessionID", "Authorization",
	}
	for _, k := range sensitive {
		if !IsSensitiveKey(k) {
			t.Errorf("IsSensitiveKey(%q) = false, want true", k)
		}
	}
	// Slice 3.5 aliases.
	for _, k := range []string{"pwd", "pass", "private_key", "access_key", "client_secret", "ssn", "phone", "mobile"} {
		if !IsSensitiveKey(k) {
			t.Errorf("IsSensitiveKey(%q) = false, want true (alias)", k)
		}
	}
	context := []string{"namespace", "service", "job", "instance", "method", "code", "shard"}
	for _, k := range context {
		if IsSensitiveKey(k) {
			t.Errorf("IsSensitiveKey(%q) = true, want false (context label)", k)
		}
	}
}

func TestHighRiskValueExpandedPatterns(t *testing.T) {
	risky := []string{
		"AKIAIOSFODNN7EXAMPLE",                     // AWS access key
		"ASIAY34FZKBOKMUTVV7A",                     // AWS temp key
		"sk-Ab12Cd34Ef56Gh78",                      // sk-* token
		"550e8400-e29b-41d4-a716-446655440000",     // UUID
		"DEADBEEFDEADBEEFDEADBEEFDEADBEEF",         // uppercase 32 hex
		"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", // lowercase 40 hex
		"contact me at bob@corp.io please",         // embedded email
	}
	for _, v := range risky {
		if !IsHighRiskValue(v) {
			t.Errorf("IsHighRiskValue(%q) = false, want true", v)
		}
	}
}

func TestHighRiskValueDetection(t *testing.T) {
	risky := []string{
		"alice@example.com",
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abc",
		"Bearer sk-12345",
		"deadbeefdeadbeefdeadbeefdeadbeef", // 32 hex
	}
	for _, v := range risky {
		if !IsHighRiskValue(v) {
			t.Errorf("IsHighRiskValue(%q) = false, want true", v)
		}
	}
	safe := []string{"payment-api", "GET", "200", "payments", "node-1", ""}
	for _, v := range safe {
		if IsHighRiskValue(v) {
			t.Errorf("IsHighRiskValue(%q) = true, want false", v)
		}
	}
}

func sampleProfiles() []profile.MetricProfile {
	return []profile.MetricProfile{
		{
			MetricName: "http_user_requests_total",
			LabelKeys:  []string{"note", "service", "user_id"},
			SampleLabelValues: map[string][]string{
				"user_id": {"u1", "u2", "u3"},
				"service": {"payment-api"},
				"note":    {"contact alice@example.com", "ok"},
			},
		},
		{
			MetricName: "auth_logins_total",
			LabelKeys:  []string{"api_key", "namespace"},
			SampleLabelValues: map[string][]string{
				"api_key":   {"abc123", "def456"},
				"namespace": {"payments"},
			},
		},
	}
}

func TestProfilesRedactsSensitiveAndRisky(t *testing.T) {
	out, info := Profiles(sampleProfiles())

	// Sensitive keys: every value replaced.
	p0 := out[0]
	for _, v := range p0.SampleLabelValues["user_id"] {
		if v != Placeholder {
			t.Errorf("user_id value not redacted: %q", v)
		}
	}
	// Non-sensitive key with a risky value: only the risky one replaced.
	gotNote := p0.SampleLabelValues["note"]
	wantNote := []string{Placeholder, "ok"}
	if !reflect.DeepEqual(gotNote, wantNote) {
		t.Errorf("note = %v, want %v", gotNote, wantNote)
	}
	// Context value preserved.
	if got := p0.SampleLabelValues["service"]; !reflect.DeepEqual(got, []string{"payment-api"}) {
		t.Errorf("service should be preserved, got %v", got)
	}

	// api_key is sensitive -> both values replaced.
	for _, v := range out[1].SampleLabelValues["api_key"] {
		if v != Placeholder {
			t.Errorf("api_key value not redacted: %q", v)
		}
	}
	if got := out[1].SampleLabelValues["namespace"]; !reflect.DeepEqual(got, []string{"payments"}) {
		t.Errorf("namespace should be preserved, got %v", got)
	}

	// Info: sensitive keys sorted, value count = replaced sample values.
	wantKeys := []string{"api_key", "user_id"}
	if !reflect.DeepEqual(info.SensitiveLabelKeys, wantKeys) {
		t.Errorf("sensitive_label_keys = %v, want %v", info.SensitiveLabelKeys, wantKeys)
	}
	// 3 (user_id) + 1 (note email) + 2 (api_key) = 6.
	if info.RedactedValueCount != 6 {
		t.Errorf("redacted_value_count = %d, want 6", info.RedactedValueCount)
	}
	if !info.Enabled {
		t.Errorf("redaction should be enabled")
	}
}

func TestProfilesDoesNotMutateInput(t *testing.T) {
	in := sampleProfiles()
	_, _ = Profiles(in)
	if in[0].SampleLabelValues["user_id"][0] != "u1" {
		t.Errorf("input was mutated: %v", in[0].SampleLabelValues["user_id"])
	}
}

func TestNoRawSensitiveValueInSerializedOutput(t *testing.T) {
	out, _ := Profiles(sampleProfiles())
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"u1", "u2", "u3", "abc123", "def456", "alice@example.com"} {
		if strings.Contains(string(data), raw) {
			t.Errorf("raw sensitive value %q leaked into output", raw)
		}
	}
}

// TestNoSecretInAnyOutboundField injects secrets into every outbound string
// field of a profile and asserts none survive into the serialized output.
func TestNoSecretInAnyOutboundField(t *testing.T) {
	secrets := map[string]string{
		"metric_name": "AKIAIOSFODNN7EXAMPLE",
		"owner":       "owner-eyJhbGciOi.eyJzdWIi",
		"service":     "alice@example.com",
		"namespace":   "ns-550e8400-e29b-41d4-a716-446655440000",
		"job":         "sk-Ab12Cd34Ef56Gh78",
		"signal":      "Bearer abcdef0123456789",
		"sample":      "DEADBEEFDEADBEEFDEADBEEFDEADBEEF",
		"token_value": "supersecretvalue",
	}
	in := []profile.MetricProfile{{
		MetricName:  "m_" + secrets["metric_name"],
		Owner:       secrets["owner"],
		Service:     secrets["service"],
		Namespace:   secrets["namespace"],
		Jobs:        []string{secrets["job"]},
		RuleSignals: []string{"service:orphan", "leak:" + secrets["signal"]},
		LabelKeys:   []string{"build", "token"},
		SampleLabelValues: map[string][]string{
			"build": {secrets["sample"]},      // high-risk value under non-sensitive key
			"token": {secrets["token_value"]}, // sensitive key -> value replaced
		},
	}}

	out, _ := Profiles(in)
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for field, raw := range secrets {
		if strings.Contains(string(data), raw) {
			t.Errorf("secret injected into %s leaked: %q present in output", field, raw)
		}
	}
	// Sensitive key name itself is kept as evidence.
	if !strings.Contains(string(data), `"token"`) {
		t.Errorf("sensitive key name should be preserved as evidence")
	}
}
