# Example reports

These are **real artifacts produced by `prom-ai-guard`** (not hand-written), regenerated with:

```bash
prom-ai-guard scan \
  --input fixtures/demo_metrics.prom \
  --config configs \
  --out examples \
  --ai-mode local_rules
```

The input fixture `fixtures/demo_metrics.prom` intentionally covers all 7 invalid metric
types, so the reports exercise every section.

| File | Description |
|---|---|
| `analysis_report.json` | Machine contract (canonical output) |
| `analysis_report.md` | Human-readable Markdown report |
| `analysis_report.xlsx` | Excel report — sheets: Summary, Invalid Metrics, Top Risk, Top Violation Labels, Warnings, Storage Impact |
| `ai_input_preview.json` | Redacted profiles that would be sent to the LLM |
| `scan.log.jsonl` | Structured audit log (one JSON event per line) |

Generated with `--ai-mode local_rules` (deterministic, **no API key**); the `ai` section
shows `disabled`. Run with `--ai-mode llm_fullscan` (and `LLM_API_KEY` set) to populate the
AI analysis section.
