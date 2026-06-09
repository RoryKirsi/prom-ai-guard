# Gate 示例 / Gate example

## 中文

`gate` 子命令把 `analysis_report.json` 按 `policy.yaml` 做**确定性准入判定**，用于 CI/CD 拦截
（需求 §四.5）。它**只读取确定性字段**，因此 LLM 不稳定或缺失都不影响门禁结果。

退出码：

| exit | 含义 |
|---|---|
| `0` | 通过 |
| `1` | 工具 / schema 错误 |
| `2` | 策略失败（被拦截） |

本示例（`fixtures/demo_metrics.prom` + `configs/policy.yaml`）→ `exit 2`，`gate_result.json`：

```json
{ "passed": false, "exit_code": 2,
  "policy_hits": [
    { "policy_id": "max_severe", "severity": "severe", "message": "severe=1 exceeds max 0" },
    { "policy_id": "max_invalid_ratio", "severity": "warning", "message": "invalid_ratio 0.6364 exceeds max 0.3000" },
    { "policy_id": "forbidden_label_keys", "severity": "severe", "message": "forbidden label key(s) present: user_id" } ] }
```

复现（确定性，无需 key；`--json` 只把 GateResult 打到 stdout，CI 友好）：

```bash
prom-ai-guard scan --input fixtures/demo_metrics.prom --config configs --out reports --ai-mode local_rules
prom-ai-guard gate --report reports/analysis_report.json --policy configs/policy.yaml --json > examples/gate/gate_result.json
echo "exit: $?"   # 2 = 被策略拦截
```

---

## English

The `gate` subcommand makes a **deterministic** CI/CD admission decision on
`analysis_report.json` against `policy.yaml` (requirement §四.5). It reads **only deterministic
fields**, so a flaky or absent LLM cannot change the gate result.

Exit codes:

| exit | meaning |
|---|---|
| `0` | pass |
| `1` | tool / schema error |
| `2` | policy failure (blocked) |

This example (`fixtures/demo_metrics.prom` + `configs/policy.yaml`) → `exit 2`, `gate_result.json`:

```json
{ "passed": false, "exit_code": 2,
  "policy_hits": [
    { "policy_id": "max_severe", "severity": "severe", "message": "severe=1 exceeds max 0" },
    { "policy_id": "max_invalid_ratio", "severity": "warning", "message": "invalid_ratio 0.6364 exceeds max 0.3000" },
    { "policy_id": "forbidden_label_keys", "severity": "severe", "message": "forbidden label key(s) present: user_id" } ] }
```

Reproduce (deterministic, no key; `--json` emits only the GateResult to stdout, CI-safe):

```bash
prom-ai-guard scan --input fixtures/demo_metrics.prom --config configs --out reports --ai-mode local_rules
prom-ai-guard gate --report reports/analysis_report.json --policy configs/policy.yaml --json > examples/gate/gate_result.json
echo "exit: $?"   # 2 = blocked by policy
```
