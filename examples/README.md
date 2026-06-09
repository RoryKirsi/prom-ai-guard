# 示例报告 / Example report

## 中文

由 `prom-ai-guard` 真实生成的产物（非手写），输入为 `fixtures/demo_metrics.prom`（刻意覆盖全部 7
类无效指标）。本示例使用真实 LLM（`--ai-mode llm_fullscan`）生成：

```bash
export LLM_API_KEY=...   # 你的 provider key
prom-ai-guard scan \
  --input fixtures/demo_metrics.prom \
  --config configs \
  --out examples \
  --ai-mode llm_fullscan --ai-scope all --ai-batch-size 5
```

| 文件 | 说明 |
|---|---|
| `analysis_report.json` | 机器契约。`summary` 含 `storage_impact` 与确定性 `governance_assessment`；`ai` 块（`status: success`）含 LLM 增强发现（`root_cause`、`recommendations`、`analysis_sources: ["local_rules","llm"]`）与整批 `ai.governance_summary`。 |
| `analysis_report.md` | 人类可读 Markdown（含 Governance assessment 与 AI 叙述）。 |
| `analysis_report.xlsx` | Excel 报告（含 `Governance` 工作表）。 |
| `ai_input_preview.json` | 实际发给 LLM 的**脱敏**画像。 |
| `scan.log.jsonl` | 审计日志：扫描生命周期 **+ 每个无效指标一条 `metric_classified`**。 |

另见：
- **[diff/](diff/)** —— 历史对比（`diff`）示例：两次扫描的无效指标变化（新增 / 已解决 / 风险上升 / 类型变化）。
- **[relabel/](relabel/)** —— relabel 提案（`relabel`）示例：从无效指标生成 Prometheus relabel 规则（仅提案，永不应用）。

说明：
- LLM 输出**非确定性**（每次措辞不同），故本示例为快照。用 `--ai-mode local_rules`（无需 key）可得
  完全可复现的确定性报告——`governance_assessment` 相同，仅 `ai.*` 不同。
- 这些文件中绝不持久化 API key、原始标签值、prompt 或响应。

---

## English

Real artifacts produced by `prom-ai-guard` (not hand-written) from
`fixtures/demo_metrics.prom`, which intentionally covers all 7 invalid metric types.
Generated with a live LLM (`--ai-mode llm_fullscan`):

```bash
export LLM_API_KEY=...   # your provider key
prom-ai-guard scan \
  --input fixtures/demo_metrics.prom \
  --config configs \
  --out examples \
  --ai-mode llm_fullscan --ai-scope all --ai-batch-size 5
```

| File | Description |
|---|---|
| `analysis_report.json` | Machine contract. `summary` includes `storage_impact` + the deterministic `governance_assessment`; the `ai` block (`status: success`) includes LLM-enriched findings (`root_cause`, `recommendations`, `analysis_sources: ["local_rules","llm"]`) and the whole-batch `ai.governance_summary`. |
| `analysis_report.md` | Human-readable Markdown (incl. the Governance assessment + AI narrative). |
| `analysis_report.xlsx` | Excel report (incl. the `Governance` sheet). |
| `ai_input_preview.json` | The exact **redacted** profiles sent to the LLM. |
| `scan.log.jsonl` | Audit log: scan lifecycle **+ one `metric_classified` event per invalid metric**. |

See also:
- **[diff/](diff/)** — a history-diff (`diff`) example: how invalid metrics change between two scans (added / resolved / risk increased / type changes).
- **[relabel/](relabel/)** — a relabel-proposal (`relabel`) example: Prometheus relabel rules generated from the invalid metrics (proposal only, never applied).

Notes:
- LLM output is **non-deterministic** (wording differs per run), so this sample is a
  snapshot. Run with `--ai-mode local_rules` (no key) for a fully reproducible,
  deterministic report — the `governance_assessment` is identical; only `ai.*` differs.
- No API key, raw label value, prompt, or response is ever persisted in these files.
