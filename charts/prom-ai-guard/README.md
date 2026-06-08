# prom-ai-guard Helm chart

Runs `prom-ai-guard` as a Kubernetes **Job** (default) or **CronJob**. It is a
batch CLI — there is **no Service or Ingress**. The default render is fully
self-contained: it scans a mounted demo metrics file (`source: file`) with AI
disabled (`local_rules`), so it needs no in-cluster Prometheus and no API key.

The tool is read-only with respect to your cluster: it needs **no Kubernetes API
access** (RBAC is off by default, the ServiceAccount token is not mounted) and it
**never applies relabel rules or mutates Prometheus/Kubernetes**.

## Quick reference

| Value | Default | Notes |
|---|---|---|
| `workload.kind` | `Job` | or `CronJob` (`workload.schedule`) |
| `scan.source` | `file` | or `prometheus_api` (`scan.promURL`, `scan.match`) |
| `scan.aiMode` | `local_rules` | `llm_fullscan` requires `llm.existingSecret` |
| `image.repository` / `image.tag` | `prom-ai-guard` / `""`→appVersion | set your registry |
| `reports.persistence.enabled` | `false` | emptyDir by default; PVC for durable reports |
| `llm.existingSecret` | `""` | pre-created Secret holding `LLM_API_KEY` (no key in values) |
| `rbac.create` | `false` | tool needs no K8s API |

## K3s deployment test (run only after review — these are NOT executed by the chart)

```sh
# 1. Build the image (multi-arch optional for arm64 K3s).
docker build -t <registry>/prom-ai-guard:0.1.0 .
#    arm64 example:
#    docker buildx build --platform linux/arm64 -t <registry>/prom-ai-guard:0.1.0 .

# 2. Make it available to K3s — either push to a registry, or import into containerd:
docker save <registry>/prom-ai-guard:0.1.0 | sudo k3s ctr images import -

# 3. Render and review before installing.
helm template prom-ai-guard charts/prom-ai-guard \
  --set image.repository=<registry>/prom-ai-guard --set image.tag=0.1.0

# 4. (Optional) enable the AI without committing a secret.
kubectl create secret generic pag-llm --from-literal=LLM_API_KEY=sk-... -n monitoring

# 5. Install / upgrade.
helm install prom-ai-guard charts/prom-ai-guard -n monitoring --create-namespace \
  --set image.repository=<registry>/prom-ai-guard --set image.tag=0.1.0
#   Prometheus API mode + AI:
#   --set scan.source=prometheus_api \
#   --set scan.promURL=http://prometheus-server.monitoring.svc:9090 \
#   --set scan.aiMode=llm_fullscan --set llm.existingSecret=pag-llm

# 6. Logs (the console summary) and reports.
kubectl logs -n monitoring job/prom-ai-guard -f
#   For durable reports, set reports.persistence.enabled=true and kubectl cp from a
#   helper pod, or read the JSON/MD/XLSX from the PVC.

# 7. Uninstall.
helm uninstall prom-ai-guard -n monitoring
```

## Optional future hardening

A `NetworkPolicy` is intentionally **not** created (no restrictive policy by
default). If you want to constrain egress to only your Prometheus / LLM endpoint,
add one out-of-band as an environment-specific policy.
