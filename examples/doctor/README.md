# Doctor / inspect 示例 / Doctor example

## 中文

`doctor`（别名 `inspect`）是**只读的单指标聚焦诊断**：按 `--metric` / `--label` / `--service`
选择器（多个为 AND）在 `analysis_report.json` 里筛选无效指标（需求 §四.4）。它不重新扫描、不调用 LLM。

本示例 `--metric http_user_requests_total` 生成 `doctor_report.md`（`--out`，人类可读）与
`doctor_report.json`（`--json`，`match_count: 1`）：字段含 `query`（用了哪些选择器）、`report`
（scan 元信息）、`matches`（命中的无效指标详情，含 `rule_signals`/`root_cause`/`recommendations`）、`notes`。

**重要语义**：`doctor` 只匹配 `invalid_metrics`——**“查不到” ≠ “健康”**（该指标可能本就有效，或不在本次
扫描范围）。`relabel_proposal_possible` 是基于报告的保守估计（`relabel_candidate` + 可执行的
invalid_type），**不是**去读 `relabel_rules.yaml`。

复现（确定性，无需 key）：

```bash
prom-ai-guard scan   --input fixtures/demo_metrics.prom --config configs --out reports --ai-mode local_rules
prom-ai-guard doctor --report reports/analysis_report.json --metric http_user_requests_total \
  --out examples/doctor/doctor_report.md --json examples/doctor/doctor_report.json
# 也可按标签 / 服务：--label user_id   或   --service order-api
```

---

## English

`doctor` (alias `inspect`) is a **read-only, focused single-metric diagnosis**: it filters the
invalid metrics in `analysis_report.json` by `--metric` / `--label` / `--service` selectors (AND of
all provided) — requirement §四.4. It does not re-scan and does not call the LLM.

This example, `--metric http_user_requests_total`, produces `doctor_report.md` (`--out`, human-readable)
and `doctor_report.json` (`--json`, `match_count: 1`): fields include `query` (which selectors were used),
`report` (scan metadata), `matches` (the matched invalid metrics, with `rule_signals`/`root_cause`/
`recommendations`), and `notes`.

**Important semantics:** `doctor` only matches `invalid_metrics` — **"not found" ≠ "healthy"** (the
metric may be valid, or simply outside this scan's scope). `relabel_proposal_possible` is a
conservative estimate derived from the report (`relabel_candidate` + an actionable invalid_type) —
**not** a lookup of `relabel_rules.yaml`.

Reproduce (deterministic, no key):

```bash
prom-ai-guard scan   --input fixtures/demo_metrics.prom --config configs --out reports --ai-mode local_rules
prom-ai-guard doctor --report reports/analysis_report.json --metric http_user_requests_total \
  --out examples/doctor/doctor_report.md --json examples/doctor/doctor_report.json
# also by label / service:  --label user_id   or   --service order-api
```
