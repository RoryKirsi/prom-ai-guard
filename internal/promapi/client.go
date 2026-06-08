// Package promapi is a read-only Prometheus HTTP API data source. It enumerates
// metric names via /api/v1/label/__name__/values and fetches series label sets
// via /api/v1/series (POST with form-encoded match[] for batched requests, which
// is read-only). It never writes to Prometheus, never re-runs analysis, and has
// no Kubernetes/kubectl/Helm integration.
//
// API mode is metadata-oriented: /series returns label sets but no sample
// values, so each MetricSeries gets Value=0. Downstream rules are label/name/
// cardinality-based and do not read the value, so this is lossless for analysis.
package promapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"prom-ai-guard/internal/model"
)

const (
	pathLabelNames = "/api/v1/label/__name__/values"
	pathSeries     = "/api/v1/series"
)

// Options are the optional scope controls for a fetch.
type Options struct {
	Matchers []string // optional; presence => filtered scope
	Start    string   // optional, passed to /series
	End      string   // optional, passed to /series
}

// Client is a read-only Prometheus HTTP API client.
type Client struct {
	baseURL        string
	http           *http.Client
	maxSeries      int
	maxMetricNames int
	batchSize      int
}

// NewClient validates the base URL and constructs a client. It performs no
// network call. Auth is none; redirects are restricted to the same host.
func NewClient(baseURL string, timeout time.Duration, maxSeries, maxMetricNames, batchSize int) (*Client, error) {
	if err := ValidateBaseURL(baseURL); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 50
	}
	return &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		http:           &http.Client{Timeout: timeout, CheckRedirect: sameHostRedirect},
		maxSeries:      maxSeries,
		maxMetricNames: maxMetricNames,
		batchSize:      batchSize,
	}, nil
}

// ValidateBaseURL allows internal/private Prometheus hosts (no SSRF guard) but
// rejects non-http(s) schemes, userinfo, and malformed URLs.
func ValidateBaseURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("prometheus base URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid prometheus base URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("prometheus base URL scheme must be http or https")
	}
	if u.User != nil {
		return fmt.Errorf("prometheus base URL must not contain userinfo")
	}
	if u.Host == "" {
		return fmt.Errorf("prometheus base URL has no host")
	}
	return nil
}

// sameHostRedirect refuses redirects to a different host (auth is none, but we
// never silently follow a redirect to an unrelated host).
func sameHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	if len(via) > 0 && req.URL.Host != via[0].URL.Host {
		return fmt.Errorf("refusing cross-host redirect")
	}
	return nil
}

// FetchSeries returns the metric series for the given scope. With no matchers it
// performs a full metadata scan (enumerate names, then batched /series); with
// matchers it queries /series directly. On a guardrail breach or any request
// error it returns an error and no partial result.
func (c *Client) FetchSeries(ctx context.Context, opts Options) ([]model.MetricSeries, []model.ParseWarning, error) {
	matchers := opts.Matchers
	if len(matchers) == 0 {
		names, err := c.fetchMetricNames(ctx)
		if err != nil {
			return nil, nil, err
		}
		matchers = make([]string, 0, len(names))
		for _, n := range names {
			matchers = append(matchers, fmt.Sprintf("{__name__=%q}", n))
		}
	}
	if len(matchers) == 0 {
		return []model.MetricSeries{}, nil, nil
	}

	series := []model.MetricSeries{}
	var warns []model.ParseWarning
	count := 0

	for _, chunk := range chunk(matchers, c.batchSize) {
		resp, err := c.postSeries(ctx, chunk, opts.Start, opts.End)
		if err != nil {
			return nil, nil, err
		}
		derr := decodeEnvelope(resp.Body, func(dec *json.Decoder) error {
			var ls map[string]string
			if err := dec.Decode(&ls); err != nil {
				return err
			}
			name := ls["__name__"]
			if name == "" {
				warns = append(warns, model.ParseWarning{Reason: "series missing __name__", Raw: fmt.Sprintf("%v", ls)})
				return nil
			}
			count++
			if c.maxSeries > 0 && count > c.maxSeries {
				return fmt.Errorf("fetched series exceed --max-series %d; narrow with --match or raise the limit", c.maxSeries)
			}
			labels := make(map[string]string, len(ls))
			for k, v := range ls {
				if k != "__name__" {
					labels[k] = v
				}
			}
			series = append(series, model.MetricSeries{MetricName: name, Labels: labels, Value: 0})
			return nil
		})
		resp.Body.Close()
		if derr != nil {
			return nil, nil, derr
		}
	}
	return series, warns, nil
}

// fetchMetricNames enumerates metric names, enforcing max_metric_names BEFORE any
// series are fetched.
func (c *Client) fetchMetricNames(ctx context.Context) ([]string, error) {
	resp, err := c.get(ctx, pathLabelNames)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	names := []string{}
	count := 0
	derr := decodeEnvelope(resp.Body, func(dec *json.Decoder) error {
		var s string
		if err := dec.Decode(&s); err != nil {
			return err
		}
		count++
		if c.maxMetricNames > 0 && count > c.maxMetricNames {
			return fmt.Errorf("metric-name enumeration exceeds --max-metric-names %d; narrow scope or raise the limit", c.maxMetricNames)
		}
		names = append(names, s)
		return nil
	})
	if derr != nil {
		return nil, derr
	}
	return names, nil
}

func (c *Client) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("building request")
	}
	return c.do(req)
}

// postSeries sends a read-only POST to /api/v1/series with form-encoded match[]
// selectors (preferred over GET for batched requests / many matchers).
func (c *Client) postSeries(ctx context.Context, matchers []string, start, end string) (*http.Response, error) {
	v := url.Values{}
	for _, m := range matchers {
		v.Add("match[]", m)
	}
	if start != "" {
		v.Set("start", start)
	}
	if end != "" {
		v.Set("end", end)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+pathSeries, strings.NewReader(v.Encode()))
	if err != nil {
		return nil, fmt.Errorf("building request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req)
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prometheus request failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("prometheus request failed: status %d", resp.StatusCode)
	}
	return resp, nil
}

// decodeEnvelope streams a Prometheus API response {"status":..,"data":[...]},
// invoking onElem per data element. It enforces status=="success" and never
// buffers the whole data array, so per-element guards can stop early.
func decodeEnvelope(r io.Reader, onElem func(*json.Decoder) error) error {
	dec := json.NewDecoder(r)
	t, err := dec.Token()
	if err != nil {
		return fmt.Errorf("invalid prometheus response")
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("prometheus response is not a JSON object")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := kt.(string)
		switch key {
		case "status":
			var s string
			if err := dec.Decode(&s); err != nil {
				return err
			}
			if s != "success" {
				return fmt.Errorf("prometheus api returned status %q", s)
			}
		case "data":
			dt, err := dec.Token()
			if err != nil {
				return err
			}
			if d, ok := dt.(json.Delim); !ok || d != '[' {
				return fmt.Errorf("prometheus data is not an array")
			}
			for dec.More() {
				if err := onElem(dec); err != nil {
					return err
				}
			}
			if _, err := dec.Token(); err != nil { // consume ']'
				return err
			}
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return err
			}
		}
	}
	return nil
}

func chunk(s []string, size int) [][]string {
	if size <= 0 {
		size = len(s)
	}
	var out [][]string
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		out = append(out, s[i:end])
	}
	return out
}
