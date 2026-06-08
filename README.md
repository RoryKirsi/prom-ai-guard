# prom-ai-guard

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](go.mod) ![Interface](https://img.shields.io/badge/interface-CLI-555) ![Prometheus](https://img.shields.io/badge/Prometheus-metric%20governance-E6522C?logo=prometheus&logoColor=white) ![AI](https://img.shields.io/badge/AI-OpenAI--compatible-412991?logo=openai&logoColor=white)

**AI-assisted Prometheus invalid-metric governance** — deterministic local rules are
authoritative; the LLM is advisory (adds/upgrades findings, never downgrades).

**AI 辅助的 Prometheus 无效指标治理** —— 确定性本地规则为权威；LLM 仅为辅助（只新增 / 升级
发现，绝不降级）。

[中文](#中文) | [English](#english)

---

## 中文

`prom-ai-guard` 是一个用于 **Prometheus 无效指标治理** 的 Go CLI：扫描本地 Prometheus
文本文件或只读 Prometheus HTTP API，用确定性规则引擎识别无效 / 高成本指标，可选地用
**provider-neutral 的 LLM** 增强分析，并产出可审计报告与 CI/CD 准入门禁。

**确定性本地规则是权威**；LLM 仅为辅助：只能新增发现或升级严重度，**绝不降级**确定性结论。

### 项目概览

- 数据源：本地 Prometheus 文本文件（默认），或只读 Prometheus HTTP API。
- 分析：确定性七类规则 + 风险分（权威），可选 LLM 增强（辅助）。
- 产物：JSON（机器契约）/ Markdown / Excel 报告 + 脱敏 LLM 输入预览 + 安全审计日志。
- 治理：CI/CD 门禁、relabel 提案（不应用）、历史对比、单指标只读诊断。
- 交付：distroless 镜像 + Helm（Job/CronJob）chart。

### 解决的问题

高基数标签、orphan 指标、无意义 / 错标指标、重复、废弃名、空 / 非法标签会放大 TSDB 索引
压力和告警噪声。本工具识别并风险分级、估算索引压力影响、解释根因、**生成（但不应用）**
relabel 清理建议、对比历史扫描、并在 CI 中按策略门禁——全部基于脱敏元数据，且绝不修改
Prometheus。

### 已交付功能

- **`scan`**：解析 → 七类无效规则 + 风险分 → 报告。
- **Prometheus API 数据源**：只读、metadata-oriented（`--source prometheus_api`）。
- **LLM 分析**：`llm_fullscan`（provider-neutral，OpenAI-compatible）或 `local_rules`。
- **MetricProfile 脱敏**：外发前移除敏感标签键 / 值。
- **存储影响**：对无效指标的 TSDB 索引压力启发式评分（非磁盘字节）。
- **报告**：JSON（机器契约）/ Markdown / Excel。
- **审计日志** `scan.log.jsonl`：安全的结构化 JSONL（每次覆盖）。
- **`gate`**：确定性门禁，退出码 `0` / `1` / `2`。
- **`relabel`**：仅生成提案 `relabel_rules.yaml`，永不应用。
- **`diff`**：两份报告的确定性对比。
- **`doctor` / `inspect`**：对指标 / 标签 / 服务的只读聚焦诊断。
- **Docker + Helm**：distroless 镜像 + Job/CronJob chart。

| 命令 | 用途 | 关键参数 | 退出码 |
|---|---|---|---|
| `scan` | 扫描数据源，跑规则 + AI，产出报告 | `--source` `--input` `--prom-url` `--ai-mode` `--ai-batch-size` `--out` | 0 成功 / 1 错误 |
| `gate` | 对报告应用策略 | `--report` `--policy` `--json` | 0 通过 / 1 工具错误 / 2 策略失败 |
| `relabel` | 生成 relabel 提案 | `--report` `--out` | 0 成功 / 1 错误 |
| `diff` | 对比两份报告 | `--previous` `--current` `--out` `--json` | 0 成功 / 1 工具或 schema 错误 |
| `doctor`（`inspect`） | 只读聚焦诊断 | `--report` `--metric` `--label` `--service` `--json` | 0 成功 / 1 错误 |

### 安装

```bash
git clone https://github.com/RoryKirsi/prom-ai-guard.git
cd prom-ai-guard
go build ./cmd/prom-ai-guard        # 产出 ./prom-ai-guard（需 Go 1.24+）
# 或安装到 $GOBIN：
go install ./cmd/prom-ai-guard
```

也可用容器运行（见下方 Docker 一节）。

### 快速开始：本地文件扫描

```bash
go build ./cmd/prom-ai-guard

./prom-ai-guard scan \
  --input fixtures/demo_metrics.prom \
  --out reports \
  --ai-mode local_rules
```

控制台摘要（节选）：

```
prom-ai-guard scan
  source:             file fixtures/demo_metrics.prom
  total_series:       16
  total_metric_names: 11
  valid / invalid:    4 / 7
  risk_distribution:  severe=1 warning=4 minor=2
  ai:                 disabled (mode=local_rules)
  report (json):      reports/analysis_report.json
```

随后在 CI 中对报告做门禁：

```bash
./prom-ai-guard gate --report reports/analysis_report.json --policy configs/policy.yaml
echo "exit: $?"   # 0 通过, 2 策略失败, 1 工具错误
```

### Prometheus API 扫描

只读、metadata-oriented：通过 `/api/v1/label/__name__/values` 枚举指标名，并分批
`POST /api/v1/series` 取标签；Series API 不返回样本值，故 series 值以 `0` 占位；绝不修改
Prometheus。`--max-series` / `--max-metric-names` 为守卫，超限即报错且不写部分报告。

```bash
./prom-ai-guard scan \
  --source prometheus_api \
  --prom-url http://prometheus.monitoring.svc:9090 \
  --match '{job="prometheus"}' \
  --out reports \
  --ai-mode local_rules \
  --max-series 3000 --max-metric-names 1000 --prom-timeout-seconds 30
```

### LLM 配置

LLM 为 **provider-neutral**（任意 OpenAI-compatible Chat Completions 端点），DeepSeek 仅为
默认演示。API key **只**从 `api_key_env`（默认 `LLM_API_KEY`）环境变量读取，绝不写入文件 /
报告 / 日志。

`configs/ai.yaml`：

```yaml
provider: deepseek            # 默认演示 provider；任意 OpenAI-compatible 均可
mode: llm_fullscan            # llm_fullscan | local_rules
model: deepseek-v4-flash
base_url: https://api.deepseek.com
api_key_env: LLM_API_KEY      # key 仅从该环境变量读取
max_attempts: 2               # 每个 batch 的总尝试次数（首次尝试 + max_attempts=2 时最多一次重试）
max_payload_bytes: 262144     # 单 batch payload 上限
timeout_seconds: 30
batch_size: 50                # FullScan 批大小；0 -> 默认 50
```

运行 LLM 扫描（key 经环境变量传入，绝不出现在命令行）：

```bash
export LLM_API_KEY=...        # 你的 provider key
./prom-ai-guard scan \
  --input fixtures/demo_metrics.prom \
  --out reports \
  --ai-mode llm_fullscan --ai-scope all \
  --model deepseek-v4-flash --base-url https://api.deepseek.com \
  --ai-batch-size 5
```

模式：

- **`llm_fullscan`**：把脱敏 MetricProfile 发给 LLM。`ai.status` 为 `success`（全部 batch 成功）/
  `partial`（部分 batch 失败，`partial_fallback_used`）/ `fallback`（无 batch 成功或无 key，`fallback_used`）。
- **`local_rules`**：仅确定性，不调用 LLM（`ai.status: disabled`）。

**批大小（`--ai-batch-size`）**：FullScan 按 metric 名排序分批调用 LLM，每 batch 独立重试，默认
`batch_size=50`。批越小，返回 JSON 越可靠。**模型相关调优很重要**：K3s 实测中，273 指标宽口径
扫描在 `deepseek-v4-flash` 上 `batch_size=50` 返回不可解析 JSON 而 fallback，`batch_size=5`
则全部成功。模型返回 `invalid_response` 时请调小 `--ai-batch-size`。

### 报告产物

均写在 `--out`（默认 `reports/`，git-ignored）：

| 文件 | 说明 |
|---|---|
| `analysis_report.json` | 机器契约：`summary`、`invalid_metrics`、`ai` 块、`summary.storage_impact`。 |
| `analysis_report.md` | 人类可读 Markdown 报告。 |
| `analysis_report.xlsx` | Excel 报告（摘要、Top 指标、风险分布）。 |
| `ai_input_preview.json` | 会发给 LLM 的**脱敏**画像（与实际外发一致）。 |
| `scan.log.jsonl` | 安全的结构化 JSONL 审计日志（每行一个事件，每次扫描覆盖）。 |

`scan.log.jsonl` 仅含安全字段——绝不含 API key、Authorization 头、原始 prompt、原始 LLM 响应、
原始 MetricProfile 或原始标签样本；`prom_url` 脱敏为 `scheme://host`。
`summary.storage_impact`（及 `invalid_metrics[].storage_impact`）为索引压力**启发式**评分
（`series_count * (label_count + 1) + Σ label_cardinality`），是倒排索引压力代理值，**非**磁盘字节。

### Docker

镜像为多阶段、distroless 非 root（uid `65532`），不内置任何 config / secret，运行时挂载。

基础扫描（本地文件，仅确定性，不需 key）：

```bash
docker build -t prom-ai-guard:local .

mkdir -p reports && chmod 0777 reports   # 需可被 uid 65532 写入

docker run --rm \
  -v "$PWD/fixtures:/data:ro" \
  -v "$PWD/configs:/configs:ro" \
  -v "$PWD/reports:/reports" \
  prom-ai-guard:local \
  scan --input /data/demo_metrics.prom --config /configs --out /reports --ai-mode local_rules
```

可选 LLM 扫描（需要 AI 时）——用 `-e LLM_API_KEY` 从宿主环境透传（不打印）：

```bash
docker run --rm \
  -v "$PWD/fixtures:/data:ro" \
  -v "$PWD/configs:/configs:ro" \
  -v "$PWD/reports:/reports" \
  -e LLM_API_KEY \
  prom-ai-guard:local \
  scan --input /data/demo_metrics.prom --config /configs --out /reports \
       --ai-mode llm_fullscan --ai-scope all --ai-batch-size 5
```

### Helm

Chart：`charts/prom-ai-guard/`（Kubernetes `Job`，可选 `CronJob`）。无 Service / Ingress，默认
关闭 RBAC，只读根文件系统。完整 values 见 `charts/prom-ai-guard/README.md`。

默认安装——**文件演示 + `local_rules`**（不需 key）：

```bash
helm install pag charts/prom-ai-guard -n prom-ai-guard --create-namespace
```

Prometheus API + LLM 模式——key 仍由你带外提供，`values.yaml` 永不含 key；它来自预创建的
Secret（`llm.existingSecret`）。下面用临时文件创建 Secret，避免 key 进入 shell 历史 / argv：

```bash
# 假设 $LLM_API_KEY 已在你的环境中带外设置
tmp="$(mktemp)"
printf '%s' "$LLM_API_KEY" > "$tmp"
kubectl create secret generic pag-llm \
  --from-file=LLM_API_KEY="$tmp" -n prom-ai-guard
shred -u "$tmp" || rm -f "$tmp"
```

```yaml
# values-prom.yaml
scan:
  source: prometheus_api
  aiMode: llm_fullscan
  promURL: "http://prometheus.monitoring.svc:9090"
  match: ['{job="prometheus"}']
llm:
  existingSecret: pag-llm     # key 仅从该 Secret 注入
  secretKey: LLM_API_KEY
extraArgs:
  - --ai-scope=all
  - --ai-batch-size=5
  - --max-series=3000
  - --max-metric-names=1000
```

```bash
helm install pag charts/prom-ai-guard -n prom-ai-guard -f values-prom.yaml
```

### 安全边界

- **不记录任何密钥**：API key / Authorization 头 / 原始 prompt / 原始 LLM 响应 / 原始
  MetricProfile / 原始标签样本绝不出现在任何报告或 `scan.log.jsonl`（审计 schema 为 typed、封闭）。
- **脱敏**：外发前把敏感标签键 / 值脱敏进 MetricProfile；`ai_input_preview.json` 展示实际外发内容。
- **Prometheus 只读**：仅用 `GET` 标签值与 `POST /series`（读查询），绝不修改 Prometheus 配置。
- **relabel 仅提案、永不应用**：`relabel_rules.yaml` 标记 `review_required`，`labeldrop` 规则带
  scope-wide 警告；工具绝不把规则应用到 Prometheus。
- **门禁确定性**：gate 只读确定性报告字段，LLM 不稳定或缺失都不会改变门禁结果。

### K3s 冒烟结果

在 K3s 上对临时 Prometheus + 真实 provider 做了端到端验证：

- narrow run（小 matcher）达到 `ai.status=success`；
- broad run（273 指标）`batch_size=50` 以 `invalid_response` fallback（`deepseek-v4-flash`）；
- 同样 broad run `batch_size=5` 成功，**273/273** 已分析；
- LLM 在真实数据上**新增了语义发现**（确定性基线仍为权威）。

冒烟环境随后已清理。

---

## English

A Go CLI for **AI-assisted Prometheus invalid-metric governance**. It scans a local
Prometheus text file or a read-only Prometheus HTTP API, flags invalid / high-cost
metrics with a deterministic rule engine, optionally enriches the findings with a
provider-neutral LLM, and produces auditable reports plus a CI/CD gate.

Deterministic local rules are **authoritative**. The LLM is **advisory**: it may add
new findings or upgrade severity, but it can **never downgrade** a deterministic
finding.

### What problem it solves

Prometheus setups accumulate metrics that inflate TSDB index pressure and alerting
noise: high-cardinality labels, orphan metrics with no owner, meaningless or
mislabeled metrics, duplicates, deprecated names, and empty/invalid labels.
`prom-ai-guard` finds these, risk-ranks them, estimates their index-pressure impact,
explains them, **proposes** (never applies) relabel cleanup, compares scans over
time, and fails a CI gate on policy violations — all from redacted metadata, without
ever mutating Prometheus.

### Features

- **`scan`** — parse → deterministic rules (7 invalid types) + risk score → reports.
- **Prometheus API source** — read-only, metadata-oriented (`--source prometheus_api`).
- **LLM analysis** — `llm_fullscan` (provider-neutral, OpenAI-compatible) or `local_rules`.
- **MetricProfile redaction** — sensitive label keys/values stripped before any LLM send.
- **Storage impact** — heuristic TSDB index-pressure score per invalid metric.
- **Reports** — JSON (machine contract), Markdown, Excel.
- **Audit log** — `scan.log.jsonl`, a safe structured JSONL trail.
- **`gate`** — deterministic CI/CD policy gate (exit `0` / `1` / `2`).
- **`relabel`** — generates a relabel **proposal** (`relabel_rules.yaml`), never applied.
- **`diff`** — deterministic comparison of two reports.
- **`doctor` / `inspect`** — read-only focused diagnosis of a metric / label / service.
- **Docker + Helm** — distroless image and a Job/CronJob chart.

| Command | Purpose | Notable flags | Exit codes |
|---|---|---|---|
| `scan` | Scan a source, run rules + AI, write reports | `--source`, `--input`, `--prom-url`, `--ai-mode`, `--ai-batch-size`, `--out` | 0 ok / 1 error |
| `gate` | Apply a policy to a report | `--report`, `--policy`, `--json` | 0 pass / 1 tool error / 2 policy fail |
| `relabel` | Generate a relabel proposal | `--report`, `--out` | 0 ok / 1 error |
| `diff` | Compare two reports | `--previous`, `--current`, `--out`, `--json` | 0 success / 1 tool or schema error |
| `doctor` (`inspect`) | Read-only focused diagnosis | `--report`, `--metric`, `--label`, `--service`, `--json` | 0 ok / 1 error |

### Install

```bash
git clone https://github.com/RoryKirsi/prom-ai-guard.git
cd prom-ai-guard
go build ./cmd/prom-ai-guard        # produces ./prom-ai-guard (Go 1.24+)
# or install into $GOBIN:
go install ./cmd/prom-ai-guard
```

Or run it as a container (see the Docker section below).

### Quickstart — local file scan

```bash
go build ./cmd/prom-ai-guard

./prom-ai-guard scan \
  --input fixtures/demo_metrics.prom \
  --out reports \
  --ai-mode local_rules
```

Then gate the report in CI:

```bash
./prom-ai-guard gate --report reports/analysis_report.json --policy configs/policy.yaml
echo "exit: $?"   # 0 pass, 2 policy fail, 1 tool error
```

### Prometheus API scan

The Prometheus source is **read-only** and **metadata-oriented**: it enumerates metric
names and series labels via `/api/v1/label/__name__/values` and batched `POST
/api/v1/series`. The Series API does not return sample values, so series values are
`0` placeholders. Prometheus is **never** modified.

```bash
./prom-ai-guard scan \
  --source prometheus_api \
  --prom-url http://prometheus.monitoring.svc:9090 \
  --match '{job="prometheus"}' \
  --out reports \
  --ai-mode local_rules \
  --max-series 3000 --max-metric-names 1000 --prom-timeout-seconds 30
```

`--max-series` / `--max-metric-names` are guardrails: if the fetch would exceed them
the scan exits with an error and writes no partial report.

### LLM configuration

The LLM is **provider-neutral** (any OpenAI-compatible Chat Completions endpoint).
DeepSeek is only the default demo provider. The API key is read **only** from the
environment variable named by `api_key_env` (default `LLM_API_KEY`) and is never
written to a file, report, or log.

```yaml
# configs/ai.yaml
provider: deepseek            # default demo provider; any OpenAI-compatible works
mode: llm_fullscan            # llm_fullscan | local_rules
model: deepseek-v4-flash
base_url: https://api.deepseek.com
api_key_env: LLM_API_KEY      # key is read only from this env var
max_attempts: 2               # total attempts per batch (initial attempt + at most one retry when max_attempts=2)
max_payload_bytes: 262144     # per-batch payload cap
timeout_seconds: 30
batch_size: 50                # FullScan batch size; 0 -> default 50
```

```bash
export LLM_API_KEY=...        # your provider key
./prom-ai-guard scan \
  --input fixtures/demo_metrics.prom --out reports \
  --ai-mode llm_fullscan --ai-scope all \
  --model deepseek-v4-flash --base-url https://api.deepseek.com \
  --ai-batch-size 5
```

Modes: **`llm_fullscan`** sends redacted MetricProfiles to the LLM (`ai.status` =
`success` / `partial` (`partial_fallback_used`) / `fallback` (`fallback_used`));
**`local_rules`** is deterministic only, no LLM call (`ai.status: disabled`).

**Batch sizing (`--ai-batch-size`).** FullScan splits the redacted profiles into
deterministic batches (sorted by metric name) and calls the LLM once per batch with
per-batch retry. Default `batch_size` is `50`; smaller batches produce smaller, more
reliably parseable responses. In the K3s smoke test, a broad scan of 273 metrics with
`deepseek-v4-flash` at `batch_size=50` returned unparseable JSON and fell back, while
`batch_size=5` succeeded with all 273 metrics analyzed. If a model returns
`invalid_response`, lower `--ai-batch-size`.

### Report artifacts

All artifacts are written under `--out` (default `reports/`, git-ignored):
`analysis_report.json` (machine contract: `summary`, `invalid_metrics`, `ai` block,
`summary.storage_impact`), `analysis_report.md`, `analysis_report.xlsx`,
`ai_input_preview.json` (the exact **redacted** LLM input), and `scan.log.jsonl` (safe
JSONL audit, one event per line, overwritten per scan).

`scan.log.jsonl` contains only safe fields — never the API key, Authorization header,
raw prompt, raw LLM response, raw MetricProfile, or raw label samples; `prom_url` is
sanitized to `scheme://host`. `summary.storage_impact` is a **heuristic** index-pressure
score (`series_count * (label_count + 1) + Σ label_cardinality`) — **not** disk bytes.

### Docker

The image is multi-stage and runs as a non-root distroless user (uid `65532`), with no
baked configs or secrets. Basic scan (local file, deterministic only — no key):

```bash
docker build -t prom-ai-guard:local .

mkdir -p reports && chmod 0777 reports   # writable by uid 65532

docker run --rm \
  -v "$PWD/fixtures:/data:ro" \
  -v "$PWD/configs:/configs:ro" \
  -v "$PWD/reports:/reports" \
  prom-ai-guard:local \
  scan --input /data/demo_metrics.prom --config /configs --out /reports --ai-mode local_rules
```

Optional LLM scan — pass the key through from the host environment with `-e LLM_API_KEY`
(value is not printed):

```bash
docker run --rm \
  -v "$PWD/fixtures:/data:ro" \
  -v "$PWD/configs:/configs:ro" \
  -v "$PWD/reports:/reports" \
  -e LLM_API_KEY \
  prom-ai-guard:local \
  scan --input /data/demo_metrics.prom --config /configs --out /reports \
       --ai-mode llm_fullscan --ai-scope all --ai-batch-size 5
```

### Helm

Chart: `charts/prom-ai-guard/` (a Kubernetes `Job`, optionally `CronJob`). It exposes
no Service or Ingress, defaults RBAC off, and runs with a read-only root filesystem.
See `charts/prom-ai-guard/README.md` for the full values reference.

Default install — **file demo, `local_rules`** (no key required):

```bash
helm install pag charts/prom-ai-guard -n prom-ai-guard --create-namespace
```

Prometheus API mode with LLM — the key is still provided out-of-band and
`values.yaml` never contains it; it comes from a pre-created Secret
(`llm.existingSecret`). The example below creates the Secret from a temp file so the
key never lands in shell history / argv:

```bash
# assumes $LLM_API_KEY is already set out-of-band in your environment
tmp="$(mktemp)"
printf '%s' "$LLM_API_KEY" > "$tmp"
kubectl create secret generic pag-llm \
  --from-file=LLM_API_KEY="$tmp" -n prom-ai-guard
shred -u "$tmp" || rm -f "$tmp"
```

```yaml
# values-prom.yaml
scan:
  source: prometheus_api
  aiMode: llm_fullscan
  promURL: "http://prometheus.monitoring.svc:9090"
  match: ['{job="prometheus"}']
llm:
  existingSecret: pag-llm     # key injected from this Secret only
  secretKey: LLM_API_KEY
extraArgs:
  - --ai-scope=all
  - --ai-batch-size=5
  - --max-series=3000
  - --max-metric-names=1000
```

```bash
helm install pag charts/prom-ai-guard -n prom-ai-guard -f values-prom.yaml
```

### Security boundaries

- **No secret logging.** The API key / Authorization header / raw prompt / raw LLM
  response / raw MetricProfile / raw label samples never appear in any report or in
  `scan.log.jsonl` (the audit schema is typed and closed).
- **Redaction.** Sensitive label keys/values are redacted into MetricProfiles before
  anything is sent to the LLM; `ai_input_preview.json` shows exactly what would be sent.
- **Prometheus is read-only.** Only `GET` label values and `POST /series` (read query)
  are used; Prometheus configuration is never changed.
- **Relabel is a proposal, never applied.** `relabel_rules.yaml` is marked
  `review_required`, and `labeldrop` rules carry a scope-wide warning.
- **Gate is deterministic.** It reads only deterministic report fields, so a flaky or
  absent LLM cannot change a gate decision.

### K3s smoke-test summary

The tool was validated end-to-end on K3s against a temporary Prometheus and a real
provider:

- A **narrow** run reached `ai.status=success`.
- A **broad** run (273 metrics) with `batch_size=50` **fell back** with
  `invalid_response` for `deepseek-v4-flash`.
- The same **broad** run with `batch_size=5` **succeeded** with **273/273** analyzed.
- The LLM **added semantic findings on the live data** (the deterministic baseline
  remained authoritative).

The smoke-test environment was torn down afterward.

---

## Development

```bash
go build ./...
go test ./...
go vet ./...
gofmt -l .          # should print nothing
```

Configuration lives in `configs/` (`rules.yaml`, `policy.yaml`, `service_inventory.yaml`,
`ai.yaml`, `prometheus.yaml`); demo metrics are in `fixtures/`. Generated reports under
`reports/` are git-ignored.

## License / 许可

This repository does not currently include a `LICENSE` file, so no open-source license
is granted; please contact the repository owner before reuse.

本仓库当前未包含 `LICENSE` 文件，因此未授予任何开源许可；如需复用请先联系仓库所有者。
