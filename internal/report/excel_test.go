package report

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestWriteExcelSheetsAndStyling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analysis_report.xlsx")
	if err := WriteExcel(sampleReport(), path); err != nil {
		t.Fatal(err)
	}

	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatalf("reopen xlsx: %v", err)
	}
	defer f.Close()

	// Exact final sheet names, in order.
	want := []string{"Summary", "Invalid Metrics", "Top Risk", "Top Violation Labels", "Warnings", "Storage Impact"}
	if got := f.GetSheetList(); !reflect.DeepEqual(got, want) {
		t.Fatalf("sheet list = %v, want %v", got, want)
	}

	// Header row present on the Invalid Metrics sheet.
	if v, _ := f.GetCellValue("Invalid Metrics", "A1"); v != "metric_name" {
		t.Errorf("Invalid Metrics A1 = %q, want metric_name", v)
	}
	// A sample data cell.
	if v, _ := f.GetCellValue("Invalid Metrics", "A2"); v != "http_user_requests_total" {
		t.Errorf("Invalid Metrics A2 = %q, want http_user_requests_total", v)
	}

	// Frozen header row on each sheet (Freeze=true, YSplit=1).
	for _, sheet := range want {
		panes, err := f.GetPanes(sheet)
		if err != nil {
			t.Fatalf("GetPanes(%q): %v", sheet, err)
		}
		if !panes.Freeze || panes.YSplit != 1 {
			t.Errorf("sheet %q panes = %+v, want frozen header row", sheet, panes)
		}
	}

	// Top Risk header sanity.
	if v, _ := f.GetCellValue("Top Risk", "A1"); v != "metric_name" {
		t.Errorf("Top Risk A1 = %q", v)
	}
	// Warnings header sanity.
	if v, _ := f.GetCellValue("Warnings", "A1"); v != "line" {
		t.Errorf("Warnings A1 = %q", v)
	}
	// Storage Impact sheet header + first data row.
	if v, _ := f.GetCellValue("Storage Impact", "A1"); v != "metric_name" {
		t.Errorf("Storage Impact A1 = %q", v)
	}
	if v, _ := f.GetCellValue("Storage Impact", "G1"); v != "impact_level" {
		t.Errorf("Storage Impact G1 = %q", v)
	}
	if v, _ := f.GetCellValue("Storage Impact", "A2"); v != "http_user_requests_total" {
		t.Errorf("Storage Impact A2 = %q", v)
	}
}

func TestWriteExcelEmptyReport(t *testing.T) {
	// A report with no invalids/warnings must still produce all five sheets.
	r := sampleReport()
	r.InvalidMetrics = nil
	r.TopRiskMetrics = nil
	r.TopViolationLabels = nil
	r.Warnings = nil

	path := filepath.Join(t.TempDir(), "empty.xlsx")
	if err := WriteExcel(r, path); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := []string{"Summary", "Invalid Metrics", "Top Risk", "Top Violation Labels", "Warnings", "Storage Impact"}
	if got := f.GetSheetList(); !reflect.DeepEqual(got, want) {
		t.Errorf("sheet list = %v, want %v", got, want)
	}
}
