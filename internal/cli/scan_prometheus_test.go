package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// promMux serves a mock Prometheus: /label/__name__/values from names, and
// /series returning one series per match[]={__name__="X"} selector.
func promMux(names []string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/label/__name__/values", func(w http.ResponseWriter, r *http.Request) {
		quoted := make([]string, len(names))
		for i, n := range names {
			quoted[i] = fmt.Sprintf("%q", n)
		}
		fmt.Fprintf(w, `{"status":"success","data":[%s]}`, strings.Join(quoted, ","))
	})
	mux.HandleFunc("/api/v1/series", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		var items []string
		for _, m := range r.PostForm["match[]"] {
			name := strings.Trim(strings.TrimPrefix(strings.TrimSuffix(m, "}"), "{__name__="), `"`)
			items = append(items, fmt.Sprintf(`{"__name__":%q,"job":"demo"}`, name))
		}
		fmt.Fprintf(w, `{"status":"success","data":[%s]}`, strings.Join(items, ","))
	})
	return mux
}

type reportSource struct {
	Source struct {
		SourceType string `json:"source_type"`
		PromURL    string `json:"prom_url"`
		ScanScope  string `json:"scan_scope"`
	} `json:"source"`
	Summary struct {
		TotalSeries      int `json:"total_series"`
		TotalMetricNames int `json:"total_metric_names"`
	} `json:"summary"`
}

func readReportSource(t *testing.T, outDir string) reportSource {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, "analysis_report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rs reportSource
	if err := json.Unmarshal(data, &rs); err != nil {
		t.Fatal(err)
	}
	return rs
}

func TestScanPrometheusAPISource(t *testing.T) {
	srv := httptest.NewServer(promMux([]string{"m1", "m2"}))
	defer srv.Close()
	cfg := setupConfigDir(t)
	outDir := t.TempDir()

	out, err := runScanCmd(t, "--source", "prometheus_api", "--prom-url", srv.URL,
		"--config", cfg, "--out", outDir, "--ai-mode", "local_rules")
	if err != nil {
		t.Fatalf("scan failed: %v\n%s", err, out)
	}
	rs := readReportSource(t, outDir)
	if rs.Source.SourceType != "prometheus_api" {
		t.Errorf("source_type = %q, want prometheus_api", rs.Source.SourceType)
	}
	if rs.Source.PromURL != srv.URL {
		t.Errorf("prom_url = %q, want %q", rs.Source.PromURL, srv.URL)
	}
	if rs.Source.ScanScope != "all" {
		t.Errorf("scan_scope = %q, want all (no --match)", rs.Source.ScanScope)
	}
	if rs.Summary.TotalSeries != 2 {
		t.Errorf("total_series = %d, want 2", rs.Summary.TotalSeries)
	}
	if !strings.Contains(out, "metadata-oriented") {
		t.Errorf("console should note metadata-oriented mode: %s", out)
	}
}

func TestScanPrometheusFilteredScope(t *testing.T) {
	srv := httptest.NewServer(promMux([]string{"m1"}))
	defer srv.Close()
	cfg := setupConfigDir(t)
	outDir := t.TempDir()

	if _, err := runScanCmd(t, "--source", "prometheus_api", "--prom-url", srv.URL,
		"--config", cfg, "--out", outDir, "--ai-mode", "local_rules",
		"--match", `{__name__="m1"}`); err != nil {
		t.Fatal(err)
	}
	if rs := readReportSource(t, outDir); rs.Source.ScanScope != "filtered" {
		t.Errorf("scan_scope = %q, want filtered (--match present)", rs.Source.ScanScope)
	}
}

func TestScanPrometheusMissingURL(t *testing.T) {
	cfg := setupConfigDir(t)
	outDir := t.TempDir()
	_, err := runScanCmd(t, "--source", "prometheus_api", "--config", cfg, "--out", outDir, "--ai-mode", "local_rules")
	if err == nil {
		t.Fatal("expected error when --prom-url is missing")
	}
	if _, statErr := os.Stat(filepath.Join(outDir, "analysis_report.json")); statErr == nil {
		t.Errorf("no report must be written when source config is invalid")
	}
}

func TestScanPrometheusMaxSeriesExit1NoPartialReport(t *testing.T) {
	// /series returns 3 series regardless; --max-series 1 must fail with no report.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/series") {
			fmt.Fprint(w, `{"status":"success","data":[{"__name__":"a"},{"__name__":"b"},{"__name__":"c"}]}`)
			return
		}
		fmt.Fprint(w, `{"status":"success","data":["a"]}`)
	}))
	defer srv.Close()
	cfg := setupConfigDir(t)
	outDir := t.TempDir()

	_, err := runScanCmd(t, "--source", "prometheus_api", "--prom-url", srv.URL,
		"--config", cfg, "--out", outDir, "--ai-mode", "local_rules",
		"--match", `{__name__=~".+"}`, "--max-series", "1")
	if err == nil {
		t.Fatal("expected max-series error")
	}
	if !strings.Contains(err.Error(), "max-series") {
		t.Errorf("error = %v, want max-series breach", err)
	}
	if _, statErr := os.Stat(filepath.Join(outDir, "analysis_report.json")); statErr == nil {
		t.Errorf("no partial report must be written on max-series breach")
	}
}
