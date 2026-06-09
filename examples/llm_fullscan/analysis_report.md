# prom-ai-guard analysis report

## Scan

- scan_id: `20260609T034757Z-scan`
- scan_time: 2026-06-09T03:47:57Z
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
- ai_mode: llm_fullscan (scope: all)
- status: success
- analyzed_metric_count: 11
- ai_summary: Single metric with an empty label value on 'env' suggests label governance gaps. Prioritize enforcing label hygiene to avoid silent data loss or misinterpretation. Ensure that all mandatory labels are populated or use relabeling to fill defaults.

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
- risk_reason: Label name 'route:path' contains colon, which is not a valid Prometheus label identifier.
- root_cause: Misconfiguration in metric instrumentation using colon in label name.
- recommendations: Rename label 'route:path' to 'route_path' to comply with Prometheus label naming rules.
- owner/service/namespace: orders-team / order-api / orders
- series_count: 1
- analysis_sources: local_rules, llm
- relabel_candidate: true

### debug_trace_count

- invalid_types: meaningless_metric
- risk: minor (score 30)
- risk_reason: Metric 'debug_trace_count' with single series offers no diagnostic value and clutters the metric namespace.
- root_cause: Leftover debug metric or unused instrumentation.
- recommendations: Remove if not needed, or convert to a toggle-able debug endpoint.
- owner/service/namespace: orders-team / order-api / orders
- series_count: 1
- analysis_sources: local_rules, llm
- relabel_candidate: true

### dup_orders_total

- invalid_types: duplicate_metric
- risk: warning (score 55)
- risk_reason: Duplicate series detected: two identical label sets produce the same metric, causing ingestion overhead and potential data inconsistency.
- root_cause: Multiple scrapes of same endpoints or misconfigured exporters emitting duplicate data.
- recommendations: Investigate sources emitting duplicate series; deduplicate or drop duplicates via relabeling.
- owner/service/namespace: orders-team / order-api / orders
- series_count: 2
- analysis_sources: local_rules, llm
- relabel_candidate: false

### ghost_exporter_up

- invalid_types: orphan_metric
- risk: minor (score 35)
- risk_reason: Metric originates from service 'ghost-api' which appears to be decommissioned or not actively scraped.
- root_cause: Service decommission without cleaning up associated metrics.
- recommendations: Remove metric if service is decommissioned; or verify its purpose.
- owner/service/namespace: unknown / ghost-api / -
- series_count: 1
- analysis_sources: local_rules, llm
- relabel_candidate: false

### http_user_requests_total

- invalid_types: high_cardinality
- risk: severe (score 90)
- risk_reason: Label 'user_id' has high cardinality potential; currently 3 values but may grow unbounded with user count.
- root_cause: Per-user metrics without aggregation; each user creates new series.
- recommendations: Aggregate user metrics by user segment or remove user_id label if not essential; Implement label whitelisting to prevent unbounded cardinality; Consider using summary/histogram for latency instead of per-user counter; TSDB storage optimization: reduce label "user_id" (3 distinct) — ~13 estimated index entries (heuristic); use recording rules or drop high-cardinality labels via metric_relabel_configs.
- owner/service/namespace: platform-observability / payment-api / payments
- series_count: 3
- analysis_sources: local_rules, llm
- relabel_candidate: true

### order_legacy_latency_seconds

- invalid_types: deprecated_metric
- risk: warning (score 50)
- risk_reason: Metric is marked as deprecated, indicating it should be replaced or removed.
- root_cause: Legacy metric from previous instrumentation; not actively used.
- recommendations: Replace with new latency metric (e.g., order_latency_seconds) if still needed; Remove metric if no consumers exist; Ensure transition plan and monitoring of new metric
- owner/service/namespace: orders-team / order-api / orders
- series_count: 1
- analysis_sources: local_rules, llm
- relabel_candidate: true

### queue_depth

- invalid_types: empty_label_value
- risk: warning (score 55)
- risk_reason: The label 'env' has an empty value, which can cause confusion in queries and aggregation, and may indicate a configuration error or missing metadata.
- root_cause: Misconfiguration or missing environment label during metric emission.
- recommendations: Enforce non-empty label values for 'env' via instrumentation or relabeling rules; consider dropping the label if it is not needed, or set a default value like 'unknown'.
- owner/service/namespace: orders-team / order-api / orders
- series_count: 1
- analysis_sources: local_rules, llm
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

## Governance assessment

- maturity: grade D (score 51) — heuristic
- invalid_ratio: 0.6364 (total_invalid 7)
- risk_distribution: severe=1 warning=4 minor=2

> Note: Heuristic governance-prioritization signal only — NOT an SLO, compliance score, or production-maturity certification. score = 100 − round(invalid_ratio×40) − 10×severe − 3×warning − 1×minor − 5×high_storage − 2×medium_storage, clamped to 0–100; grade A≥90 B≥75 C≥60 D≥40 F<40.

### Top systemic issues

| invalid_type | metric_count | max_risk | max_score |
|---|---|---|---|
| high_cardinality | 1 | severe | 90 |
| invalid_label_name | 1 | warning | 60 |
| duplicate_metric | 1 | warning | 55 |
| empty_label_value | 1 | warning | 55 |
| deprecated_metric | 1 | warning | 50 |
| orphan_metric | 1 | minor | 35 |
| meaningless_metric | 1 | minor | 30 |

### Prioritized actions

1. Reduce label cardinality on 1 high-cardinality metric(s): drop identity labels (user_id/session_id/…) or use recording rules / logs.
1. Fix 1 metric(s) with non-conforming label names ([a-zA-Z_][a-zA-Z0-9_]*).
1. Deduplicate 1 metric(s): merge duplicate series and fix double scraping.
1. Stop emitting empty label values on 1 metric(s).
1. Remove 1 deprecated/legacy metric(s), or add metric_relabel drop rules until removal.
1. Map 1 orphan metric(s) to service_inventory.yaml (assign owner/service).
1. Remove 1 debug/test/temp metric(s) from production exposition.

### Recommended governance norms

- Set per-metric label-cardinality budgets; forbid identity labels (user_id/session_id/trace_id/request_id).
- Enforce a metric naming convention; ban deprecated/legacy/debug/test/temp tokens in production.
- Validate label names ([a-zA-Z_][a-zA-Z0-9_]*) and forbid empty label values at instrumentation.
- Prevent duplicate series: one exporter per metric; avoid double scraping / merged jobs.
- Require owner/service labels and map every scrape job in service_inventory.yaml.
- Review the generated relabel_rules.yaml via a GitOps PR before applying; gate CI on policy.yaml.

### AI governance narrative (advisory)

> The overall monitoring governance assessment for this batch is concerning. With a maturity grade of D (score 51) and an invalid metric ratio of 63.6%, the observability posture is poor and requires immediate attention. The dominant systemic issue is high cardinality, classified as “severe,” driven by identity labels such as user_id or session_id. This risk alone undermines the stability and cost-efficiency of your Prometheus infrastructure. Additionally, invalid label names, duplicate metrics, and empty label values each represent warn-level issues that erode data quality and reliability. Deprecated, orphan, and meaningless metrics contribute to overall noise, though at lower risk. To remediate, focus first on reducing label cardinality by dropping identity labels or using recording rules. Next, enforce label naming conventions, eliminate duplicates and empty values, and retire deprecated or test metrics. Normalizing the metric inventory by requiring service ownership and validating label budgets will prevent recurrence. A CI gate with policy checks is recommended to enforce these norms at merge time. The majority of risks are warnings, but the single severe cardinality issue must be treated as a top priority to avoid operational degradation.

## Parse warnings

- line 39: invalid value "is"
- line 40: expected '"' for label value
- line 41: invalid value "not_a_number"

## Report files

- analysis_report.json (machine contract)
- analysis_report.md (this report)
- analysis_report.xlsx
- ai_input_preview.json (redacted AI input)
