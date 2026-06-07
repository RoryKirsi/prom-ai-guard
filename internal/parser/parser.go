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

// maxLineBytes bounds a single exposition line. A longer line is reported as a
// warning and skipped; the scan continues with subsequent lines.
const maxLineBytes = 1024 * 1024

// ParseReader reads Prometheus text format and returns the parsed series plus a
// warning for every line it could not parse. The error is non-nil only on an
// I/O failure of the underlying reader, never on malformed or oversized content.
func ParseReader(r io.Reader) ([]model.MetricSeries, []model.ParseWarning, error) {
	br := bufio.NewReaderSize(r, 64*1024)

	var (
		series []model.MetricSeries
		warns  []model.ParseWarning
	)

	lineNo := 0
	for {
		raw, tooLong, err := readBoundedLine(br, maxLineBytes)
		if err != nil && err != io.EOF {
			return series, warns, fmt.Errorf("reading metrics input: %w", err)
		}

		// A line exists when we read bytes, hit the length cap, or terminated on
		// a newline (err == nil) even with no bytes (a blank line).
		if len(raw) > 0 || tooLong || err == nil {
			lineNo++
			if tooLong {
				warns = append(warns, model.ParseWarning{
					Line:   lineNo,
					Raw:    previewRaw(raw),
					Reason: fmt.Sprintf("line exceeds %d bytes, skipped", maxLineBytes),
				})
			} else {
				trimmed := strings.TrimSpace(string(raw))
				if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
					if s, reason := parseLine(trimmed); reason != "" {
						warns = append(warns, model.ParseWarning{Line: lineNo, Raw: string(raw), Reason: reason})
					} else {
						series = append(series, s)
					}
				}
			}
		}

		if err == io.EOF {
			break
		}
	}
	return series, warns, nil
}

// readBoundedLine reads one line (up to the next '\n') without loading more than
// max bytes into memory. If the line is longer than max it sets tooLong, keeps
// the first max bytes for the warning preview, and drains the rest so the next
// call starts at the following line.
func readBoundedLine(br *bufio.Reader, max int) (line []byte, tooLong bool, err error) {
	var buf []byte
	for {
		frag, e := br.ReadSlice('\n')
		if !tooLong {
			if len(buf)+len(frag) > max {
				if remain := max - len(buf); remain > 0 {
					buf = append(buf, frag[:remain]...)
				}
				tooLong = true
			} else {
				buf = append(buf, frag...)
			}
		}
		if e == bufio.ErrBufferFull {
			continue // more of this line remains; keep reading/draining
		}
		return trimEOL(buf), tooLong, e
	}
}

func trimEOL(b []byte) []byte {
	if n := len(b); n > 0 && b[n-1] == '\n' {
		b = b[:n-1]
		if n2 := len(b); n2 > 0 && b[n2-1] == '\r' {
			b = b[:n2-1]
		}
	}
	return b
}

func previewRaw(b []byte) string {
	const n = 80
	if len(b) > n {
		return string(b[:n]) + "...(truncated)"
	}
	return string(b)
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
	// Reject Go-specific numeric syntax that Prometheus does not allow but
	// strconv.ParseFloat would accept (underscore separators, hex floats).
	if strings.ContainsAny(valTok, "_xX") {
		return model.MetricSeries{}, fmt.Sprintf("invalid value %q", valTok)
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
		// Duplicate label names are malformed in Prometheus; reject rather than
		// silently letting the last value win.
		if _, exists := labels[key]; exists {
			return nil, fmt.Sprintf("duplicate label name %q", key)
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
