# prom-ai-guard diff report

## Reports compared

- previous: scan_id=20260609T051128Z-scan scan_time=2026-06-09T05:11:28Z tool_version=0.1.0 source=file
- current:  scan_id=20260609T051129Z-scan scan_time=2026-06-09T05:11:29Z tool_version=0.1.0 source=file

> Note: **Risk increased**, **Risk decreased**, and **Invalid type changes** are subsets of **Still invalid metrics** and may overlap (a metric can appear in several).

## Summary delta

| metric | previous | current | change |
|---|---|---|---|
| invalid_metric_names | 2 | 2 | +0 |
| total_metric_names | 2 | 2 | +0 |
| severe | 1 | 1 | +0 |
| warning | 1 | 0 | -1 |
| minor | 0 | 1 | +1 |
| invalid_ratio | 1.0000 | 1.0000 | +0.0000 |

## Added invalid metrics

| metric_name | risk_level | risk_score | invalid_types |
|---|---|---|---|
| debug_buffer_size | minor | 30 | meaningless_metric |

## Resolved invalid metrics

| metric_name | previous risk_level | previous risk_score | previous invalid_types |
|---|---|---|---|
| http_requests_total | severe | 90 | high_cardinality |

## Still invalid metrics

| metric_name | risk_level (prev→curr) | risk_score (prev→curr) |
|---|---|---|
| legacy_orders_total | warning→severe | 50→95 |

## Risk increased

| metric_name | risk_score (prev→curr) | risk_level (prev→curr) |
|---|---|---|
| legacy_orders_total | 50→95 | warning→severe |

## Risk decreased

none

## Invalid type changes

| metric_name | added_types | removed_types |
|---|---|---|
| legacy_orders_total | high_cardinality | - |

