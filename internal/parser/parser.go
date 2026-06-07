// Package parser turns Prometheus text-format exposition into MetricSeries.
//
// Scope (Slice 1): parse `metric_name{label="value",...} value [timestamp]`,
// skip HELP/TYPE/comment/blank lines, and report malformed lines as warnings
// without aborting the scan. It does not judge metric validity — that is a
// later-slice rule concern. OpenMetrics-specific syntax is out of scope.
package parser

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"prom-ai-guard/internal/model"
)

// maxLineBytes bounds a single exposition line so the scanner can grow its
// buffer beyond bufio's small default for high-cardinality label sets.
const maxLineBytes = 1024 * 1024

// ParseReader reads Prometheus text format and returns the parsed series plus a
// warning for every line it could not parse. The error is non-nil only on an
// I/O failure of the underlying reader, never on malformed content.
func ParseReader(r io.Reader) ([]model.MetricSeries, []model.ParseWarning, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	var (
		series []model.MetricSeries
		warns  []model.ParseWarning
	)

	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue // blank line or HELP/TYPE/comment
		}
		s, reason := parseLine(trimmed)
		if reason != "" {
			warns = append(warns, model.ParseWarning{Line: lineNo, Raw: raw, Reason: reason})
			continue
		}
		series = append(series, s)
	}
	if err := scanner.Err(); err != nil {
		return series, warns, fmt.Errorf("reading metrics input: %w", err)
	}
	return series, warns, nil
}

// parseLine parses one already-trimmed, non-comment line. On success it returns
// the series and an empty reason; on failure it returns a zero series and a
// human-readable reason.
func parseLine(line string) (model.MetricSeries, string) {
	i := 0
	name, ok := scanMetricName(line, &i)
	if !ok {
		return model.MetricSeries{}, "missing or invalid metric name"
	}

	labels := map[string]string{}
	if i < len(line) && line[i] == '{' {
		var reason string
		labels, reason = scanLabels(line, &i)
		if reason != "" {
			return model.MetricSeries{}, reason
		}
	}

	// Require whitespace between the metric/labels and the value.
	if i >= len(line) || (line[i] != ' ' && line[i] != '\t') {
		return model.MetricSeries{}, "missing value"
	}
	skipSpace(line, &i)

	valTok, ok := nextToken(line, &i)
	if !ok {
		return model.MetricSeries{}, "missing value"
	}
	value, err := strconv.ParseFloat(valTok, 64)
	if err != nil {
		return model.MetricSeries{}, fmt.Sprintf("invalid value %q", valTok)
	}

	// An optional integer timestamp may follow the value. It is allowed but not
	// retained in Slice 1; anything beyond it is malformed.
	skipSpace(line, &i)
	if i < len(line) {
		tsTok, _ := nextToken(line, &i)
		if _, err := strconv.ParseInt(tsTok, 10, 64); err != nil {
			return model.MetricSeries{}, fmt.Sprintf("invalid timestamp %q", tsTok)
		}
		skipSpace(line, &i)
		if i < len(line) {
			return model.MetricSeries{}, "unexpected token after timestamp"
		}
	}

	if len(labels) == 0 {
		labels = nil
	}
	return model.MetricSeries{MetricName: name, Labels: labels, Value: value}, ""
}

// scanMetricName consumes a Prometheus metric name [a-zA-Z_:][a-zA-Z0-9_:]*.
func scanMetricName(s string, i *int) (string, bool) {
	start := *i
	for *i < len(s) {
		c := s[*i]
		if isNameChar(c, *i == start) {
			*i++
			continue
		}
		break
	}
	if *i == start {
		return "", false
	}
	return s[start:*i], true
}

// scanLabels parses `{k="v",...}` starting at s[*i]=='{' and advances past '}'.
func scanLabels(s string, i *int) (map[string]string, string) {
	labels := map[string]string{}
	*i++ // consume '{'
	for {
		skipSpace(s, i)
		if *i >= len(s) {
			return nil, "unterminated label set"
		}
		if s[*i] == '}' {
			*i++
			return labels, ""
		}
		key, ok := scanMetricName(s, i)
		if !ok {
			return nil, "invalid label name"
		}
		skipSpace(s, i)
		if *i >= len(s) || s[*i] != '=' {
			return nil, "expected '=' after label name"
		}
		*i++ // consume '='
		skipSpace(s, i)
		if *i >= len(s) || s[*i] != '"' {
			return nil, "expected '\"' for label value"
		}
		val, ok := scanQuoted(s, i)
		if !ok {
			return nil, "unterminated label value"
		}
		labels[key] = val
		skipSpace(s, i)
		if *i >= len(s) {
			return nil, "unterminated label set"
		}
		switch s[*i] {
		case ',':
			*i++ // consume ',' and continue (trailing comma allowed)
		case '}':
			*i++
			return labels, ""
		default:
			return nil, "expected ',' or '}' in label set"
		}
	}
}

// scanQuoted reads a double-quoted string starting at s[*i]=='"', handling the
// Prometheus escapes \\ \" and \n, and advances past the closing quote.
func scanQuoted(s string, i *int) (string, bool) {
	*i++ // consume opening '"'
	var b strings.Builder
	for *i < len(s) {
		c := s[*i]
		switch c {
		case '\\':
			if *i+1 >= len(s) {
				return "", false
			}
			next := s[*i+1]
			switch next {
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			case 'n':
				b.WriteByte('\n')
			default:
				b.WriteByte(next)
			}
			*i += 2
		case '"':
			*i++ // consume closing '"'
			return b.String(), true
		default:
			b.WriteByte(c)
			*i++
		}
	}
	return "", false
}

// nextToken returns the run of non-space characters starting at s[*i].
func nextToken(s string, i *int) (string, bool) {
	start := *i
	for *i < len(s) && s[*i] != ' ' && s[*i] != '\t' {
		*i++
	}
	if *i == start {
		return "", false
	}
	return s[start:*i], true
}

func skipSpace(s string, i *int) {
	for *i < len(s) && (s[*i] == ' ' || s[*i] == '\t') {
		*i++
	}
}

func isNameChar(c byte, first bool) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_', c == ':':
		return true
	case !first && c >= '0' && c <= '9':
		return true
	}
	return false
}
