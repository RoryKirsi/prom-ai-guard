# Relabel 提案示例 / Relabel proposal example

## 中文

`relabel` 子命令从 `analysis_report.json` 的无效指标**确定性地生成 Prometheus relabel 提案**
（需求 §四.3）。本工具**只生成、永不应用**——交由 GitOps 评审后再落地。

本示例（输入 `fixtures/demo_metrics.prom`）生成 `relabel_rules.yaml`，含三类动作：

| action | 含义 | 示例 |
|---|---|---|
| `labeldrop` | 丢弃违规 / 高基数标签 | `route:path`（invalid_label_name）、`user_id`（high_cardinality）|
| `drop` | 丢弃整条无效指标 | 废弃 / 无意义指标 |
| `review` | 需人工判断，不自动给 drop | duplicate / orphan 等 |

**安全模型（重点）**：
- 每条规则 `review_required: true`；`labeldrop` 规则 `copy_paste_safe: false`，并带
  `scope_warning`——因为 Prometheus 的 labeldrop 作用于整个 scrape/metric_relabel scope，影响面可能
  **大于**报告中列出的指标。
- 顶部 `dry_run_summary` 给出 `by_action` 统计、`labels_dropped`、`estimated_min_affected_series`
  （下界估计）与 `note: Proposal only — never applied`。
- 每条规则内含可直接参考的 `metric_relabel_configs` 片段。

复现（确定性，无需 key）：

```bash
prom-ai-guard scan    --input fixtures/demo_metrics.prom --config configs --out reports --ai-mode local_rules
prom-ai-guard relabel --report reports/analysis_report.json --out examples/relabel/relabel_rules.yaml
```

---

## English

The `relabel` subcommand **deterministically generates a Prometheus relabel proposal** from the
invalid metrics in `analysis_report.json` (requirement §四.3). This tool **only generates, never
applies** — apply via GitOps review.

This example (input `fixtures/demo_metrics.prom`) produces `relabel_rules.yaml` with three actions:

| action | meaning | example |
|---|---|---|
| `labeldrop` | drop an offending / high-cardinality label | `route:path` (invalid_label_name), `user_id` (high_cardinality) |
| `drop` | drop the whole invalid metric | deprecated / meaningless metrics |
| `review` | needs a human decision (not auto-dropped) | duplicate / orphan, etc. |

**Safety model (important):**
- Every rule has `review_required: true`; `labeldrop` rules are `copy_paste_safe: false` and carry a
  `scope_warning` — Prometheus `labeldrop` applies to the **entire** scrape/metric_relabel scope, so the
  real impact can be **larger** than the metrics listed in the report.
- The top `dry_run_summary` gives `by_action` counts, `labels_dropped`,
  `estimated_min_affected_series` (a lower-bound estimate), and `note: Proposal only — never applied`.
- Each rule embeds a ready-to-reference `metric_relabel_configs` snippet.

Reproduce (deterministic, no key):

```bash
prom-ai-guard scan    --input fixtures/demo_metrics.prom --config configs --out reports --ai-mode local_rules
prom-ai-guard relabel --report reports/analysis_report.json --out examples/relabel/relabel_rules.yaml
```
