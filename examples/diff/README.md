# 历史对比示例 / History diff example

## 中文

`diff` 子命令对**两次扫描**（两份 `analysis_report.json`）做**确定性历史对比**（不调用 LLM），
用于跟踪无效指标随时间的变化趋势（需求 §四.2）。

本示例的 T1 → T2 变化：

| 指标 | T1（previous） | T2（current） | diff 归类 |
|---|---|---|---|
| `legacy_orders_total` | deprecated，**warning(50)** | deprecated + high_cardinality，**severe(95)** | **仍无效 + 风险上升 + 类型变化** |
| `http_requests_total` | high_cardinality，severe | （已清理） | **已解决** |
| `debug_buffer_size` | （不存在） | meaningless，minor | **新增** |

复现（确定性，无需 key）：

```bash
prom-ai-guard scan --input examples/diff/previous.prom --config configs --out /tmp/p --ai-mode local_rules
prom-ai-guard scan --input examples/diff/current.prom  --config configs --out /tmp/c --ai-mode local_rules
prom-ai-guard diff \
  --previous /tmp/p/analysis_report.json \
  --current  /tmp/c/analysis_report.json \
  --out examples/diff/diff_report.md \
  --json examples/diff/diff_report.json
```

产物：`diff_report.md`（人类可读，含 Summary delta / 新增 / 已解决 / 仍无效 / 风险上升 / 风险下降 /
类型变化）与 `diff_report.json`（机器字段：`summary_delta`、`added_invalid`、`resolved_invalid`、
`still_invalid`、`risk_increased`、`risk_decreased`、`type_changes`、`config_changed`）。严格校验两份
报告；成功退出码 0，工具 / schema 错误退出码 1。适合在 CI 中与 `gate` 一起跟踪治理趋势。

---

## English

The `diff` subcommand performs a **deterministic** history comparison (no LLM) of **two scans**
(two `analysis_report.json` files), to track how invalid metrics change over time (requirement §四.2).

The T1 → T2 change in this example:

| Metric | T1 (previous) | T2 (current) | diff classification |
|---|---|---|---|
| `legacy_orders_total` | deprecated, **warning(50)** | deprecated + high_cardinality, **severe(95)** | **Still invalid + Risk increased + Type change** |
| `http_requests_total` | high_cardinality, severe | (cleaned up) | **Resolved** |
| `debug_buffer_size` | (absent) | meaningless, minor | **Added** |

Reproduce (deterministic, no key):

```bash
prom-ai-guard scan --input examples/diff/previous.prom --config configs --out /tmp/p --ai-mode local_rules
prom-ai-guard scan --input examples/diff/current.prom  --config configs --out /tmp/c --ai-mode local_rules
prom-ai-guard diff \
  --previous /tmp/p/analysis_report.json \
  --current  /tmp/c/analysis_report.json \
  --out examples/diff/diff_report.md \
  --json examples/diff/diff_report.json
```

Artifacts: `diff_report.md` (human-readable: Summary delta / Added / Resolved / Still invalid / Risk
increased / Risk decreased / Type changes) and `diff_report.json` (machine fields: `summary_delta`,
`added_invalid`, `resolved_invalid`, `still_invalid`, `risk_increased`, `risk_decreased`,
`type_changes`, `config_changed`). Both reports are strictly validated; exit 0 on success, exit 1 on a
tool/schema error. Pairs well with `gate` in CI to track governance trends.
