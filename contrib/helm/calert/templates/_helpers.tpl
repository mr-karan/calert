{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "calert.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "calert.fullname" -}}
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

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "calert.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "calert.labels" -}}
app.kubernetes.io/name: {{ include "calert.name" . }}
helm.sh/chart: {{ include "calert.chart" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Render the config.toml content from .Values.app and .Values.providers.
Shared between the ConfigMap (default) and Secret (when config.asSecret=true) paths.
*/}}
{{- define "calert.configToml" -}}
# All timeouts and durations are in milliseconds.

[app]
address = {{ .Values.app.address | quote }}
server_timeout = {{ .Values.app.server_timeout | quote }}
enable_request_logs = {{ .Values.app.enable_request_logs | quote }}
log = {{ .Values.app.log | quote }}

{{- range $key, $value := .Values.providers }}
[providers.{{ $key }}]
type = {{ $value.type | default "google_chat" | quote }}
endpoint = {{ required "setting a endpoint for providers is required" $value.endpoint | quote }}
max_idle_conns = {{ $value.max_idle_conns | default 50 }}
timeout = {{ $value.timeout | default "30s" | quote }}
proxy_url = {{ $value.proxy_url | default "" | quote }}
template = {{ $value.template | default "static/message.tmpl" | quote }}
thread_ttl = {{ $value.thread_ttl | default "12h" | quote }}
threaded_replies = {{ $value.threaded_replies | default false }}
dry_run = {{ $value.dry_run | default false }}
retry_max = {{ $value.retry_max | default 3 }}
retry_wait_min = {{ $value.retry_wait_min | default "1s" | quote }}
retry_wait_max = {{ $value.retry_wait_max | default "5s" | quote }}
{{- end }}
{{- end -}}

{{/*
Resolve the name of the Secret or ConfigMap containing config.toml.
When config.asSecret=true and existingSecret.name is provided, returns that name.
Otherwise falls back to the generated <fullname>-config name.
*/}}
{{- define "calert.configSecretName" -}}
{{- if .Values.config.existingSecret.name -}}
{{- .Values.config.existingSecret.name -}}
{{- else -}}
{{- template "calert.fullname" . }}-config
{{- end -}}
{{- end -}}

{{/*
Resolve the key within the existing Secret that holds config.toml content.
*/}}
{{- define "calert.configSecretKey" -}}
{{- if .Values.config.existingSecret.name -}}
{{- .Values.config.existingSecret.key | default "config.toml" -}}
{{- else -}}
config.toml
{{- end -}}
{{- end -}}
