# Example report

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

Notes:
- LLM output is **non-deterministic** (wording differs per run), so this sample is a
  snapshot. Run with `--ai-mode local_rules` (no key) for a fully reproducible,
  deterministic report — the `governance_assessment` is identical; only `ai.*` differs.
- No API key, raw label value, prompt, or response is ever persisted in these files.
