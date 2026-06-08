package promapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// promServer is a mock Prometheus that serves /label/__name__/values from names
// and /series by returning one series per requested match[]={__name__="..."}.
type promServer struct {
	names      []string
	namesHits  int
	seriesHits int
}

func (s *promServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(pathLabelNames, func(w http.ResponseWriter, r *http.Request) {
		s.namesHits++
		fmt.Fprintf(w, `{"status":"success","data":[%s]}`, quoteJoin(s.names))
	})
	mux.HandleFunc(pathSeries, func(w http.ResponseWriter, r *http.Request) {
		s.seriesHits++
		_ = r.ParseForm()
		matchers := r.PostForm["match[]"]
		var items []string
		for _, m := range matchers {
			name := nameFromMatcher(m)
			items = append(items, fmt.Sprintf(`{"__name__":%q,"job":"demo"}`, name))
		}
		fmt.Fprintf(w, `{"status":"success","data":[%s]}`, strings.Join(items, ","))
	})
	return mux
}

func quoteJoin(ss []string) string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(out, ",")
}

// nameFromMatcher extracts X from `{__name__="X"}`.
func nameFromMatcher(m string) string {
	m = strings.TrimPrefix(strings.TrimSuffix(m, "}"), "{__name__=")
	return strings.Trim(m, `"`)
}

func newTestClient(t *testing.T, base string, maxSeries, maxNames, batch int) *Client {
	t.Helper()
	c, err := NewClient(base, 2*time.Second, maxSeries, maxNames, batch)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestFetchSeriesFullScan(t *testing.T) {
	ps := &promServer{names: []string{"m1", "m2"}}
	srv := httptest.NewServer(ps.handler())
	defer srv.Close()

	c := newTestClient(t, srv.URL, 0, 0, 50)
	series, warns, err := c.FetchSeries(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if ps.namesHits != 1 {
		t.Errorf("expected metric-name enumeration once, got %d", ps.namesHits)
	}
	if len(series) != 2 {
		t.Fatalf("series = %d, want 2", len(series))
	}
	got := map[string]bool{}
	for _, s := range series {
		got[s.MetricName] = true
		if s.Value != 0 {
			t.Errorf("value = %v, want 0 (metadata-oriented)", s.Value)
		}
		if _, ok := s.Labels["__name__"]; ok {
			t.Errorf("__name__ must be stripped from labels: %v", s.Labels)
		}
		if s.Labels["job"] != "demo" {
			t.Errorf("labels = %v", s.Labels)
		}
	}
	if !got["m1"] || !got["m2"] {
		t.Errorf("missing metrics: %v", got)
	}
}

func TestFetchSeriesFilteredSkipsEnumeration(t *testing.T) {
	ps := &promServer{names: []string{"m1"}}
	srv := httptest.NewServer(ps.handler())
	defer srv.Close()

	c := newTestClient(t, srv.URL, 0, 0, 50)
	_, _, err := c.FetchSeries(context.Background(), Options{Matchers: []string{`{__name__="only"}`}})
	if err != nil {
		t.Fatal(err)
	}
	if ps.namesHits != 0 {
		t.Errorf("filtered scan must NOT enumerate metric names, got %d hits", ps.namesHits)
	}
	if ps.seriesHits == 0 {
		t.Errorf("expected a /series call")
	}
}

func TestPostSeriesUsesPOSTWithMatchBody(t *testing.T) {
	var method, ctype string
	var matchers []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		ctype = r.Header.Get("Content-Type")
		_ = r.ParseForm()
		matchers = r.PostForm["match[]"]
		fmt.Fprint(w, `{"status":"success","data":[]}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, 0, 0, 50)
	_, _, err := c.FetchSeries(context.Background(), Options{Matchers: []string{`{job="a"}`, `{job="b"}`}})
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost {
		t.Errorf("method = %s, want POST", method)
	}
	if !strings.HasPrefix(ctype, "application/x-www-form-urlencoded") {
		t.Errorf("content-type = %q", ctype)
	}
	if len(matchers) != 2 {
		t.Errorf("match[] body = %v, want 2 selectors", matchers)
	}
}

func TestMaxSeriesExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"success","data":[{"__name__":"a"},{"__name__":"b"},{"__name__":"c"}]}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, 2, 0, 50)
	_, _, err := c.FetchSeries(context.Background(), Options{Matchers: []string{`{__name__=~".+"}`}})
	if err == nil || !strings.Contains(err.Error(), "max-series") {
		t.Fatalf("expected max-series error, got %v", err)
	}
}

func TestMaxMetricNamesExceededBeforeSeries(t *testing.T) {
	ps := &promServer{names: []string{"a", "b", "c"}}
	srv := httptest.NewServer(ps.handler())
	defer srv.Close()

	c := newTestClient(t, srv.URL, 0, 2, 50)
	_, _, err := c.FetchSeries(context.Background(), Options{})
	if err == nil || !strings.Contains(err.Error(), "max-metric-names") {
		t.Fatalf("expected max-metric-names error, got %v", err)
	}
	if ps.seriesHits != 0 {
		t.Errorf("must fail before fetching any series, got %d series hits", ps.seriesHits)
	}
}

func TestSeriesMissingNameWarns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"success","data":[{"job":"x"},{"__name__":"ok"}]}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, 0, 0, 50)
	series, warns, err := c.FetchSeries(context.Background(), Options{Matchers: []string{`{__name__=~".+"}`}})
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].MetricName != "ok" {
		t.Errorf("series = %+v", series)
	}
	if len(warns) != 1 || !strings.Contains(warns[0].Reason, "__name__") {
		t.Errorf("expected one missing-__name__ warning, got %v", warns)
	}
}

func TestNon2xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL, 0, 0, 50)
	if _, _, err := c.FetchSeries(context.Background(), Options{Matchers: []string{`{a="b"}`}}); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestStatusErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"error","errorType":"bad_data","error":"oops"}`)
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL, 0, 0, 50)
	_, _, err := c.FetchSeries(context.Background(), Options{Matchers: []string{`{a="b"}`}})
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestValidateBaseURL(t *testing.T) {
	for _, ok := range []string{"http://localhost:9090", "http://10.0.0.5:9090", "https://prom.internal", "http://192.168.1.2"} {
		if err := ValidateBaseURL(ok); err != nil {
			t.Errorf("ValidateBaseURL(%q) = %v, want nil (internal allowed)", ok, err)
		}
	}
	for _, bad := range []string{"", "ftp://prom", "http://user:pass@prom:9090", "://broken"} {
		if err := ValidateBaseURL(bad); err == nil {
			t.Errorf("ValidateBaseURL(%q) = nil, want error", bad)
		}
	}
}

func TestRedirectSameHostOnly(t *testing.T) {
	// Cross-host redirect must be refused.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"success","data":[]}`)
	}))
	defer other.Close()
	crossSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+pathSeries, http.StatusFound)
	}))
	defer crossSrv.Close()
	c := newTestClient(t, crossSrv.URL, 0, 0, 50)
	if _, _, err := c.FetchSeries(context.Background(), Options{Matchers: []string{`{a="b"}`}}); err == nil {
		t.Errorf("cross-host redirect must be refused")
	}

	// Same-host redirect must be followed.
	mux := http.NewServeMux()
	mux.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, pathSeries, http.StatusFound) })
	mux.HandleFunc(pathSeries, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"status":"success","data":[]}`) })
	sameSrv := httptest.NewServer(mux)
	defer sameSrv.Close()
	c2 := newTestClient(t, sameSrv.URL, 0, 0, 50)
	// Hitting /series directly (no redirect) just confirms same-host requests work.
	if _, _, err := c2.FetchSeries(context.Background(), Options{Matchers: []string{`{a="b"}`}}); err != nil {
		t.Errorf("same-host request failed: %v", err)
	}
}

func TestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		fmt.Fprint(w, `{"status":"success","data":[]}`)
	}))
	defer srv.Close()
	c, _ := NewClient(srv.URL, 20*time.Millisecond, 0, 0, 50)
	if _, _, err := c.FetchSeries(context.Background(), Options{Matchers: []string{`{a="b"}`}}); err == nil {
		t.Fatal("expected timeout error")
	}
}
