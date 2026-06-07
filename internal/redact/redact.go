// Package redact removes sensitive label values and high-risk dynamic values
// from MetricProfiles before they are written to the AI input preview.
//
// Policy (CONTEXT.md design principle 7): sensitive label *keys* are kept as
// governance evidence, but their values are replaced. High-risk values
// (emails, tokens, secrets, ...) are scrubbed from every outbound string field
// of the profile — metric_name, owner, service, namespace, jobs, rule_signals
// and sample_label_values — even under non-sensitive keys. Ordinary context
// values (namespace/service/job names like "payments") are preserved.
package redact

import (
	"regexp"
	"sort"
	"strings"

	"prom-ai-guard/internal/profile"
)

// Placeholder replaces any redacted value. It is written literally (the report
// writer disables HTML escaping), not as <redacted>.
const Placeholder = "<redacted>"

// Info is the redaction summary written into the AI input preview.
type Info struct {
	Enabled            bool     `json:"enabled"`
	SensitiveLabelKeys []string `json:"sensitive_label_keys"`
	RedactedValueCount int      `json:"redacted_value_count"`
}

// sensitiveTokens are matched as substrings of the normalized (lowercased,
// alphanumeric-only) label key, so user_id, X-User-Id, api_key and client_secret
// all match.
var sensitiveTokens = []string{
	"token", "secret", "password", "cookie", "authorization", "apikey", "email",
	"userid", "sessionid", "traceid", "requestid",
	// aliases added in Slice 3.5 hardening
	"pwd", "pass", "privatekey", "accesskey", "ssn", "phone", "mobile",
}

// highRiskValue matches values that must never be sent to the AI even when the
// label key is not itself sensitive. Patterns are deliberately unanchored (no
// \b) so a secret embedded in a metric/label name — e.g. m_AKIA... where '_' is
// a word char and would defeat \b — is still caught. This biases toward
// over-redaction, which is the safe failure mode for outbound data.
var highRiskValue = []*regexp.Regexp{
	regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`),                      // email
	regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`),                              // JWT
	regexp.MustCompile(`(?i)bearer\s+\S+`),                                                 // bearer token
	regexp.MustCompile(`(?i)[0-9a-f]{32,}`),                                                // hex secret/digest (any case), >=32
	regexp.MustCompile(`(?:AKIA|ASIA)[0-9A-Z]{16}`),                                        // AWS access key id
	regexp.MustCompile(`sk-[A-Za-z0-9_\-]{8,}`),                                            // sk-* style API token
	regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`), // UUID
}

// IsSensitiveKey reports whether a label key is sensitive by name.
func IsSensitiveKey(key string) bool {
	n := normalize(key)
	for _, tok := range sensitiveTokens {
		if strings.Contains(n, tok) {
			return true
		}
	}
	return false
}

// IsHighRiskValue reports whether a value looks like a secret/identity token.
func IsHighRiskValue(v string) bool {
	for _, re := range highRiskValue {
		if re.MatchString(v) {
			return true
		}
	}
	return false
}

// Profiles returns redacted copies of the profiles plus a redaction summary.
// The input slice and its maps/slices are not mutated.
func Profiles(in []profile.MetricProfile) ([]profile.MetricProfile, Info) {
	out := make([]profile.MetricProfile, len(in))
	sensitive := map[string]struct{}{}
	count := 0

	// scrub replaces embedded high-risk substrings in a free-text field.
	scrub := func(s string) string {
		out := s
		for _, re := range highRiskValue {
			out = re.ReplaceAllString(out, Placeholder)
		}
		if out != s {
			count++
		}
		return out
	}

	for i, p := range in {
		np := p

		// Free-text fields: scrub embedded secrets, keep the rest.
		np.MetricName = scrub(p.MetricName)
		np.Owner = scrub(p.Owner)
		np.Service = scrub(p.Service)
		np.Namespace = scrub(p.Namespace)
		np.Jobs = scrubSlice(p.Jobs, scrub)
		np.RuleSignals = scrubSlice(p.RuleSignals, scrub)

		// Sample label values: sensitive key -> replace all; otherwise replace
		// any value that matches a high-risk pattern.
		redacted := make(map[string][]string, len(p.SampleLabelValues))
		for key, vals := range p.SampleLabelValues {
			rv := make([]string, len(vals))
			if IsSensitiveKey(key) {
				sensitive[key] = struct{}{}
				for j := range vals {
					rv[j] = Placeholder
					count++
				}
			} else {
				for j, v := range vals {
					if IsHighRiskValue(v) {
						rv[j] = Placeholder
						count++
					} else {
						rv[j] = v
					}
				}
			}
			redacted[key] = rv
		}
		np.SampleLabelValues = redacted

		out[i] = np
	}

	return out, Info{
		Enabled:            true,
		SensitiveLabelKeys: sortedKeys(sensitive),
		RedactedValueCount: count,
	}
}

func scrubSlice(in []string, scrub func(string) string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = scrub(s)
	}
	return out
}

func normalize(key string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(key) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
