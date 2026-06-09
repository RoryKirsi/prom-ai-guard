# prom-ai-guard analysis report

## Scan

- scan_id: `20260609T020105Z-scan`
- scan_time: 2026-06-09T02:01:05Z
- tool_version: 0.1.0
- config_hash: `sha256:2b485520da1e4a3ff00e4cea3a206aa8bc8c92827f59398938d3819dc9bd3823`

## Source

- source_type: file
- input_ref: `fixtures/demo_metrics.prom`
- scan_scope: all
- series_count: 16
- metric_name_count: 11

## AI analysis

- provider: deepseek
- model: deepseek-v4-flash
- ai_mode: local_rules (scope: all)
- status: disabled
- analyzed_metric_count: 0
- ai_summary: Local rule analysis: 7 invalid metric(s).

> Note: the AI summary is advisory. The counts below (Summary, Risk distribution, Invalid type counts) are the authoritative deterministic results.

## Summary

- total_series: 16
- total_metric_names: 11
- valid_metric_names: 4
- invalid_metric_names: 7
- invalid_ratio: 0.6364

## Risk distribution

- severe: 1
- warning: 4
- minor: 2

## Invalid type counts

- deprecated_metric: 1
- duplicate_metric: 1
- empty_label_value: 1
- high_cardinality: 1
- invalid_label_name: 1
- meaningless_metric: 1
- orphan_metric: 1

## Top risk metrics

| metric_name | risk_level | risk_score | invalid_types |
|---|---|---|---|
| http_user_requests_total | severe | 90 | high_cardinality |
| cache_hits_total | warning | 60 | invalid_label_name |
| dup_orders_total | warning | 55 | duplicate_metric |
| queue_depth | warning | 55 | empty_label_value |
| order_legacy_latency_seconds | warning | 50 | deprecated_metric |
| ghost_exporter_up | minor | 35 | orphan_metric |
| debug_trace_count | minor | 30 | meaningless_metric |

## Top violation labels

| label_key | invalid_type | risk_level | risk_score | metric_count | series_count | sample_metric_names |
|---|---|---|---|---|---|---|
| user_id | high_cardinality | severe | 90 | 1 | 3 | http_user_requests_total |
| route:path | invalid_label_name | warning | 60 | 1 | 1 | cache_hits_total |
| env | empty_label_value | warning | 55 | 1 | 1 | queue_depth |

## Invalid metric details

### cache_hits_total

- invalid_types: invalid_label_name
- risk: warning (score 60)
- root_cause: Label name violates the Prometheus naming convention.
- recommendations: Rename the label to match [a-zA-Z_][a-zA-Z0-9_]*.; Drop the non-conforming label via metric_relabel_configs.
- owner/service/namespace: orders-team / order-api / orders
- series_count: 1
- analysis_sources: local_rules
- relabel_candidate: true

### debug_trace_count

- invalid_types: meaningless_metric
- risk: minor (score 30)
- root_cause: Metric name matches a debug/test/temp pattern.
- recommendations: Remove debug/test metrics from production exposition.; Gate temporary metrics behind a flag.
- owner/service/namespace: orders-team / order-api / orders
- series_count: 1
- analysis_sources: local_rules
- relabel_candidate: true

### dup_orders_total

- invalid_types: duplicate_metric
- risk: warning (score 55)
- root_cause: Duplicate fingerprint: identical metric name and label set reported more than once.
- recommendations: Deduplicate the exporter output.; Check for double scraping or merged jobs producing identical series.
- owner/service/namespace: orders-team / order-api / orders
- series_count: 2
- analysis_sources: local_rules
- relabel_candidate: false

### ghost_exporter_up

- invalid_types: orphan_metric
- risk: minor (score 35)
- root_cause: Metric's service/job does not resolve to any entry in service_inventory.yaml.
- recommendations: Add the service to service_inventory.yaml or fix the job/service label.; Confirm the owning team and assign remediation.
- owner/service/namespace: unknown / ghost-api / -
- series_count: 1
- analysis_sources: local_rules
- relabel_candidate: false

### http_user_requests_total

- invalid_types: high_cardinality
- risk: severe (score 90)
- root_cause: High-cardinality or forbidden label creates unbounded time series.
- recommendations: Remove high-cardinality labels (e.g. user_id) from the metric.; Use logs or tracing for per-entity investigation.
- owner/service/namespace: platform-observability / payment-api / payments
- series_count: 3
- analysis_sources: local_rules
- relabel_candidate: true

### order_legacy_latency_seconds

- invalid_types: deprecated_metric
- risk: warning (score 50)
- root_cause: Metric name matches a deprecated/legacy naming pattern.
- recommendations: Confirm the metric is unused and remove the instrumentation.; Add a metric_relabel drop rule if removal is delayed.
- owner/service/namespace: orders-team / order-api / orders
- series_count: 1
- analysis_sources: local_rules
- relabel_candidate: true

### queue_depth

- invalid_types: empty_label_value
- risk: warning (score 55)
- root_cause: Metric carries a label with an empty value.
- recommendations: Stop emitting empty labels at the source.; Use metric_relabel_configs to drop empty labels.
- owner/service/namespace: orders-team / order-api / orders
- series_count: 1
- analysis_sources: local_rules
- relabel_candidate: true

## Storage impact (heuristic)

- impact metrics: high=0 medium=0 low=7
- estimated_invalid_series: 10
- estimated_invalid_index_entries: 40

> Note: estimated_index_entries is a heuristic proxy for inverted-index postings, not real TSDB bytes; no disk-size guarantee.
> Scope: computed for invalid metrics only; valid-metric storage impact is out of scope (analysis_report.json does not retain full valid-metric detail).

| metric_name | series_count | label_count | max_label_cardinality | estimated_index_entries | impact_level |
|---|---|---|---|---|---|
| cache_hits_total | 1 | 2 | 1 | 5 | low |
| debug_trace_count | 1 | 1 | 1 | 3 | low |
| dup_orders_total | 2 | 2 | 1 | 8 | low |
| ghost_exporter_up | 1 | 1 | 1 | 3 | low |
| http_user_requests_total | 3 | 2 | 3 | 13 | low |
| order_legacy_latency_seconds | 1 | 1 | 1 | 3 | low |
| queue_depth | 1 | 2 | 1 | 5 | low |

## Parse warnings

- line 39: invalid value "is"
- line 40: expected '"' for label value
- line 41: invalid value "not_a_number"

## Report files

- analysis_report.json (machine contract)
- analysis_report.md (this report)
- analysis_report.xlsx
- ai_input_preview.json (redacted AI input)
