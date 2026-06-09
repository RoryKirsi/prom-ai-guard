# prom-ai-guard doctor report

## Query (AND of provided selectors)
- metric:  http_user_requests_total
- label:   -
- service: -

## Report
- scan_id:   20260609T054134Z-scan
- scan_time: 2026-06-09T05:41:34Z
- source:    file

## Matches (1)

### http_user_requests_total
- risk: severe (90)
- invalid_types: high_cardinality
- rule_signals: label:user_id:high_cardinality
- risk_reason: -
- root_cause: High-cardinality or forbidden label creates unbounded time series.
- recommendations: Remove high-cardinality labels (e.g. user_id) from the metric., Use logs or tracing for per-entity investigation., TSDB storage optimization: reduce label "user_id" (3 distinct) — ~13 estimated index entries (heuristic); use recording rules or drop high-cardinality labels via metric_relabel_configs.
- owner / service / namespace: platform-observability / payment-api / payments
- relabel_candidate: true   relabel_proposal_possible: true

## Notes
- relabel_proposal_possible is an estimate derived from analysis_report.json (relabel_candidate + an actionable invalid_type); it is NOT a lookup of relabel_rules.yaml.
