{{- define "prom-ai-guard.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "prom-ai-guard.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "prom-ai-guard.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "prom-ai-guard.selectorLabels" -}}
app.kubernetes.io/name: {{ include "prom-ai-guard.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "prom-ai-guard.labels" -}}
helm.sh/chart: {{ include "prom-ai-guard.chart" . }}
{{ include "prom-ai-guard.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.labels }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{- define "prom-ai-guard.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "prom-ai-guard.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "prom-ai-guard.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}

{{/*
Build the container args. A non-empty .Values.args is used verbatim; otherwise
the scan invocation is assembled from .Values.scan.*, then extraArgs appended.
*/}}
{{- define "prom-ai-guard.args" -}}
{{- if .Values.args -}}
{{- toYaml .Values.args -}}
{{- else -}}
- {{ .Values.scan.subcommand }}
{{- if eq .Values.scan.subcommand "scan" }}
- --config=/etc/prom-ai-guard
- --out={{ .Values.reports.mountPath }}
- --ai-mode={{ .Values.scan.aiMode }}
- --source={{ .Values.scan.source }}
{{- if eq .Values.scan.source "file" }}
- --input={{ .Values.scan.input }}
{{- else if eq .Values.scan.source "prometheus_api" }}
- --prom-url={{ required "scan.promURL is required when scan.source=prometheus_api" .Values.scan.promURL }}
{{- range .Values.scan.match }}
- --match={{ . }}
{{- end }}
{{- end }}
{{- end }}
{{- range .Values.extraArgs }}
- {{ . }}
{{- end }}
{{- end -}}
{{- end -}}

{{/*
Shared pod spec body (no leading "spec:") used by both Job and CronJob. Render
with the appropriate nindent at the call site.
*/}}
{{- define "prom-ai-guard.podSpec" -}}
restartPolicy: Never
serviceAccountName: {{ include "prom-ai-guard.serviceAccountName" . }}
automountServiceAccountToken: {{ .Values.serviceAccount.automountServiceAccountToken }}
{{- with .Values.imagePullSecrets }}
imagePullSecrets:
  {{- toYaml . | nindent 2 }}
{{- end }}
securityContext:
  {{- toYaml .Values.podSecurityContext | nindent 2 }}
containers:
  - name: {{ .Chart.Name }}
    image: {{ include "prom-ai-guard.image" . }}
    imagePullPolicy: {{ .Values.image.pullPolicy }}
    args:
      {{- include "prom-ai-guard.args" . | nindent 6 }}
    {{- if or .Values.llm.existingSecret .Values.extraEnv }}
    env:
      {{- if .Values.llm.existingSecret }}
      - name: {{ .Values.llm.secretKey }}
        valueFrom:
          secretKeyRef:
            name: {{ .Values.llm.existingSecret }}
            key: {{ .Values.llm.secretKey }}
      {{- end }}
      {{- with .Values.extraEnv }}
      {{- toYaml . | nindent 6 }}
      {{- end }}
    {{- end }}
    securityContext:
      {{- toYaml .Values.securityContext | nindent 6 }}
    resources:
      {{- toYaml .Values.resources | nindent 6 }}
    volumeMounts:
      - name: config
        mountPath: /etc/prom-ai-guard
        readOnly: true
      - name: reports
        mountPath: {{ .Values.reports.mountPath }}
      - name: tmp
        mountPath: /tmp
      {{- if .Values.config.demoMetrics.enabled }}
      - name: demo-metrics
        mountPath: /data
        readOnly: true
      {{- end }}
      {{- with .Values.extraVolumeMounts }}
      {{- toYaml . | nindent 6 }}
      {{- end }}
volumes:
  - name: config
    configMap:
      name: {{ include "prom-ai-guard.fullname" . }}-config
  - name: tmp
    emptyDir: {}
  - name: reports
    {{- if .Values.reports.persistence.enabled }}
    persistentVolumeClaim:
      claimName: {{ .Values.reports.persistence.existingClaim | default (printf "%s-reports" (include "prom-ai-guard.fullname" .)) }}
    {{- else }}
    emptyDir: {}
    {{- end }}
  {{- if .Values.config.demoMetrics.enabled }}
  - name: demo-metrics
    configMap:
      name: {{ include "prom-ai-guard.fullname" . }}-metrics
  {{- end }}
  {{- with .Values.extraVolumes }}
  {{- toYaml . | nindent 2 }}
  {{- end }}
{{- end -}}
