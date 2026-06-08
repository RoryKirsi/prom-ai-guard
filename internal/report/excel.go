package report

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"prom-ai-guard/internal/model"
)

// Sheet names (exact, including spaces).
const (
	sheetSummary      = "Summary"
	sheetInvalid      = "Invalid Metrics"
	sheetTopRisk      = "Top Risk"
	sheetTopViolation = "Top Violation Labels"
	sheetWarnings     = "Warnings"
	sheetStorage      = "Storage Impact"
)

// xlStyles holds the reusable cell styles (header + per-risk fills).
type xlStyles struct {
	header int
	byRisk map[string]int
}

// WriteExcel renders the report.Report as a lightly-styled .xlsx workbook. It
// only reads r — no analysis is re-run. No macros are used.
func WriteExcel(r Report, path string) error {
	f := excelize.NewFile()
	defer f.Close()

	styles, err := newStyles(f)
	if err != nil {
		return fmt.Errorf("excel styles: %w", err)
	}

	// Rename the default sheet to Summary, then add the rest in order.
	if err := f.SetSheetName("Sheet1", sheetSummary); err != nil {
		return fmt.Errorf("excel sheet init: %w", err)
	}
	for _, name := range []string{sheetInvalid, sheetTopRisk, sheetTopViolation, sheetWarnings, sheetStorage} {
		if _, err := f.NewSheet(name); err != nil {
			return fmt.Errorf("excel new sheet %q: %w", name, err)
		}
	}
	f.SetActiveSheet(0)

	if err := writeSummarySheet(f, styles, r); err != nil {
		return err
	}
	if err := writeInvalidSheet(f, styles, r); err != nil {
		return err
	}
	if err := writeTopRiskSheet(f, styles, r); err != nil {
		return err
	}
	if err := writeTopViolationSheet(f, styles, r); err != nil {
		return err
	}
	if err := writeWarningsSheet(f, styles, r); err != nil {
		return err
	}
	if err := writeStorageSheet(f, styles, r); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	if err := f.SaveAs(path); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func newStyles(f *excelize.File) (xlStyles, error) {
	var s xlStyles
	var err error
	if s.header, err = f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#DDEBF7"}},
	}); err != nil {
		return s, err
	}
	s.byRisk = map[string]int{}
	for level, color := range map[string]string{"severe": "#FFC7CE", "warning": "#FFEB9C", "minor": "#D9D9D9"} {
		id, err := f.NewStyle(&excelize.Style{Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{color}}})
		if err != nil {
			return s, err
		}
		s.byRisk[level] = id
	}
	return s, nil
}

// header writes the bold header row, freezes it, and sets column widths.
func header(f *excelize.File, sheet string, cols []string, widths []float64, st xlStyles) error {
	for i, c := range cols {
		_ = f.SetCellValue(sheet, cell(i+1, 1), c)
	}
	if err := f.SetCellStyle(sheet, cell(1, 1), cell(len(cols), 1), st.header); err != nil {
		return err
	}
	for i, w := range widths {
		col, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(sheet, col, col, w); err != nil {
			return err
		}
	}
	// Freeze the header row.
	return f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
}

func writeSummarySheet(f *excelize.File, st xlStyles, r Report) error {
	if err := header(f, sheetSummary, []string{"Field", "Value"}, []float64{28, 80}, st); err != nil {
		return err
	}
	rows := [][2]string{
		{"scan_id", r.ScanID},
		{"scan_time", r.ScanTime},
		{"tool_version", r.ToolVersion},
		{"config_hash", r.ConfigHash},
		{"source_type", r.Source.SourceType},
		{"input_ref", r.Source.InputRef},
		{"scan_scope", r.Source.ScanScope},
		{"series_count", strconv.Itoa(r.Source.SeriesCount)},
		{"metric_name_count", strconv.Itoa(r.Source.MetricNameCount)},
	}
	if r.AI != nil {
		rows = append(rows,
			[2]string{"ai_provider", r.AI.Provider},
			[2]string{"ai_model", r.AI.Model},
			[2]string{"ai_mode", r.AI.AIMode},
			[2]string{"ai_status", r.AI.Status},
			[2]string{"ai_fallback_reason", r.AI.FallbackReason},
			[2]string{"ai_analyzed_metric_count", strconv.Itoa(r.AI.AnalyzedMetricCount)},
			[2]string{"ai_summary (advisory)", r.AI.Summary},
		)
	}
	rows = append(rows,
		[2]string{"NOTE", "AI summary is advisory; the counts below are the authoritative deterministic results."},
		[2]string{"total_series", strconv.Itoa(r.Summary.TotalSeries)},
		[2]string{"total_metric_names", strconv.Itoa(r.Summary.TotalMetricNames)},
		[2]string{"valid_metric_names", strconv.Itoa(r.Summary.ValidMetricNames)},
		[2]string{"invalid_metric_names", strconv.Itoa(r.Summary.InvalidMetricNames)},
		[2]string{"invalid_ratio", fmt.Sprintf("%.4f", r.Summary.InvalidRatio)},
	)
	for _, level := range riskOrder {
		rows = append(rows, [2]string{"risk_" + level, strconv.Itoa(r.Summary.RiskDistribution[level])})
	}
	for _, k := range sortedIntKeys(r.Summary.InvalidTypeCounts) {
		rows = append(rows, [2]string{"type_" + k, strconv.Itoa(r.Summary.InvalidTypeCounts[k])})
	}
	for i, kv := range rows {
		_ = f.SetCellValue(sheetSummary, cell(1, i+2), kv[0])
		_ = f.SetCellValue(sheetSummary, cell(2, i+2), kv[1])
	}
	return nil
}

func writeInvalidSheet(f *excelize.File, st xlStyles, r Report) error {
	cols := []string{"metric_name", "invalid_types", "risk_level", "risk_score", "risk_reason",
		"root_cause", "recommendations", "owner", "service", "namespace", "series_count",
		"analysis_sources", "relabel_candidate"}
	widths := []float64{30, 24, 12, 10, 30, 30, 36, 18, 18, 14, 12, 22, 16}
	if err := header(f, sheetInvalid, cols, widths, st); err != nil {
		return err
	}
	for i, m := range r.InvalidMetrics {
		row := i + 2
		_ = f.SetCellValue(sheetInvalid, cell(1, row), m.MetricName)
		_ = f.SetCellValue(sheetInvalid, cell(2, row), strings.Join(m.InvalidTypes, ", "))
		_ = f.SetCellValue(sheetInvalid, cell(3, row), m.RiskLevel)
		_ = f.SetCellValue(sheetInvalid, cell(4, row), m.RiskScore)
		_ = f.SetCellValue(sheetInvalid, cell(5, row), m.RiskReason)
		_ = f.SetCellValue(sheetInvalid, cell(6, row), m.RootCause)
		_ = f.SetCellValue(sheetInvalid, cell(7, row), strings.Join(m.Recommendations, "; "))
		_ = f.SetCellValue(sheetInvalid, cell(8, row), m.Owner)
		_ = f.SetCellValue(sheetInvalid, cell(9, row), m.Service)
		_ = f.SetCellValue(sheetInvalid, cell(10, row), m.Namespace)
		_ = f.SetCellValue(sheetInvalid, cell(11, row), m.SeriesCount)
		_ = f.SetCellValue(sheetInvalid, cell(12, row), strings.Join(m.AnalysisSources, ", "))
		_ = f.SetCellValue(sheetInvalid, cell(13, row), m.RelabelCandidate)
		applyRisk(f, st, sheetInvalid, 3, row, m.RiskLevel)
	}
	return nil
}

func writeTopRiskSheet(f *excelize.File, st xlStyles, r Report) error {
	cols := []string{"metric_name", "risk_level", "risk_score", "invalid_types"}
	if err := header(f, sheetTopRisk, cols, []float64{30, 12, 10, 28}, st); err != nil {
		return err
	}
	for i, m := range r.TopRiskMetrics {
		row := i + 2
		_ = f.SetCellValue(sheetTopRisk, cell(1, row), m.MetricName)
		_ = f.SetCellValue(sheetTopRisk, cell(2, row), m.RiskLevel)
		_ = f.SetCellValue(sheetTopRisk, cell(3, row), m.RiskScore)
		_ = f.SetCellValue(sheetTopRisk, cell(4, row), strings.Join(m.InvalidTypes, ", "))
		applyRisk(f, st, sheetTopRisk, 2, row, m.RiskLevel)
	}
	return nil
}

func writeTopViolationSheet(f *excelize.File, st xlStyles, r Report) error {
	cols := []string{"label_key", "invalid_type", "risk_level", "risk_score", "metric_count",
		"series_count", "sample_metric_names"}
	if err := header(f, sheetTopViolation, cols, []float64{20, 22, 12, 10, 14, 14, 40}, st); err != nil {
		return err
	}
	for i, v := range r.TopViolationLabels {
		row := i + 2
		_ = f.SetCellValue(sheetTopViolation, cell(1, row), v.LabelKey)
		_ = f.SetCellValue(sheetTopViolation, cell(2, row), v.InvalidType)
		_ = f.SetCellValue(sheetTopViolation, cell(3, row), v.RiskLevel)
		_ = f.SetCellValue(sheetTopViolation, cell(4, row), v.RiskScore)
		_ = f.SetCellValue(sheetTopViolation, cell(5, row), v.MetricCount)
		_ = f.SetCellValue(sheetTopViolation, cell(6, row), v.SeriesCount)
		_ = f.SetCellValue(sheetTopViolation, cell(7, row), strings.Join(v.SampleMetricNames, ", "))
		applyRisk(f, st, sheetTopViolation, 3, row, v.RiskLevel)
	}
	return nil
}

// writeStorageSheet writes the deterministic TSDB-index storage-impact
// simulation. estimated_index_entries is heuristic, not real TSDB bytes.
func writeStorageSheet(f *excelize.File, st xlStyles, r Report) error {
	cols := []string{"metric_name", "series_count", "label_count", "max_label_cardinality",
		"top_cardinality_labels", "estimated_index_entries", "impact_level", "reason"}
	widths := []float64{30, 14, 12, 22, 30, 24, 14, 44}
	if err := header(f, sheetStorage, cols, widths, st); err != nil {
		return err
	}
	row := 2
	for _, m := range r.InvalidMetrics {
		s := m.StorageImpact
		if s == nil {
			continue
		}
		_ = f.SetCellValue(sheetStorage, cell(1, row), m.MetricName)
		_ = f.SetCellValue(sheetStorage, cell(2, row), s.SeriesCount)
		_ = f.SetCellValue(sheetStorage, cell(3, row), s.LabelCount)
		_ = f.SetCellValue(sheetStorage, cell(4, row), s.MaxLabelCardinality)
		_ = f.SetCellValue(sheetStorage, cell(5, row), topLabelsString(s.TopCardinalityLabels))
		_ = f.SetCellValue(sheetStorage, cell(6, row), s.EstimatedIndexEntries)
		_ = f.SetCellValue(sheetStorage, cell(7, row), s.ImpactLevel)
		_ = f.SetCellValue(sheetStorage, cell(8, row), s.Reason)
		row++
	}
	return nil
}

func topLabelsString(refs []model.LabelCardinalityRef) string {
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		parts = append(parts, fmt.Sprintf("%s=%d", r.LabelKey, r.Cardinality))
	}
	return strings.Join(parts, ", ")
}

func writeWarningsSheet(f *excelize.File, st xlStyles, r Report) error {
	if err := header(f, sheetWarnings, []string{"line", "raw", "reason"}, []float64{8, 50, 40}, st); err != nil {
		return err
	}
	for i, w := range r.Warnings {
		row := i + 2
		_ = f.SetCellValue(sheetWarnings, cell(1, row), w.Line)
		_ = f.SetCellValue(sheetWarnings, cell(2, row), w.Raw)
		_ = f.SetCellValue(sheetWarnings, cell(3, row), w.Reason)
	}
	return nil
}

// applyRisk colors a risk_level cell by its level.
func applyRisk(f *excelize.File, st xlStyles, sheet string, col, row int, level string) {
	if id, ok := st.byRisk[level]; ok {
		_ = f.SetCellStyle(sheet, cell(col, row), cell(col, row), id)
	}
}

// cell converts 1-based (col,row) to an A1 reference.
func cell(col, row int) string {
	name, _ := excelize.CoordinatesToCellName(col, row)
	return name
}
