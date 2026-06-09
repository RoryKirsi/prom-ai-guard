# Example reports

Real artifacts produced by `prom-ai-guard` (not hand-written), from
`fixtures/demo_metrics.prom`, which intentionally covers all 7 invalid metric types.

## `examples/` — deterministic (`--ai-mode local_rules`, no API key)

```bash
prom-ai-guard scan \
  --input fixtures/demo_metrics.prom \
  --config configs \
  --out examples \
  --ai-mode local_rules
```

Reproducible, key-free. Includes:
- `analysis_report.json` — machine contract, with `summary.governance_assessment`
  (deterministic whole-batch governance report: maturity grade/score, top systemic
  issues, prioritized actions, recommended norms) and per-metric `storage_impact`.
- `analysis_report.md` / `.xlsx` — Markdown + Excel (incl. the `Governance` sheet).
- `ai_input_preview.json` — the redacted profiles that would be sent to the LLM.
- `scan.log.jsonl` — audit log: scan lifecycle **+ one `metric_classified` event per
  invalid metric** (how each metric was labelled: `invalid_types`, `risk`, `rule_signals`).

`ai.status` is `disabled` and `ai.governance_summary` is empty here — those require the LLM.

## `examples/llm_fullscan/` — with a live LLM (`--ai-mode llm_fullscan`)

```bash
export LLM_API_KEY=...   # your provider key
prom-ai-guard scan \
  --input fixtures/demo_metrics.prom \
  --config configs \
  --out examples/llm_fullscan \
  --ai-mode llm_fullscan --ai-scope all --ai-batch-size 5
```

Same artifacts, but `ai.status=success` and the `ai` block adds:
- LLM-enriched per-metric findings (`root_cause`, `recommendations`, `analysis_sources: ["local_rules","llm"]`).
- **`ai.governance_summary`** — the advisory **whole-batch** monitoring-governance narrative,
  synthesized from the aggregated deterministic data (the deterministic
  `summary.governance_assessment` remains authoritative).

LLM output is **non-deterministic** (wording differs per run), so this sample is not
byte-reproducible like the `local_rules` one — that is expected for an AI sample. No
API key, raw label value, prompt, or response is ever persisted in these files.
