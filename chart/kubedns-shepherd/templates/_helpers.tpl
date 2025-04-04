{{/*
Expand the name of the chart.
*/}}
{{- define "chart.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "chart.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "chart.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "chart.labels" -}}
helm.sh/chart: {{ include "chart.chart" . }}
{{ include "chart.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "chart.selectorLabels" -}}
app.kubernetes.io/name: {{ include "chart.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "chart.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "chart.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Leader Election
*/}}
{{- define "kubedns-shepherd.leaderElection" -}}
{{- if .Values.leaderElection -}}
- --leader-elect={{ .Values.leaderElection.enabled }}
{{- if .Values.leaderElection.leaseDuration }}
- --leader-elect-lease-duration={{ .Values.leaderElection.leaseDuration }}
{{- end }}
{{- if .Values.leaderElection.renewDeadline }}
- --leader-elect-renew-deadline={{ .Values.leaderElection.renewDeadline }}
{{- end }}
{{- if .Values.leaderElection.retryPeriod }}
- --leader-elect-retry-period={{ .Values.leaderElection.retryPeriod }}
{{- end }}
{{- if .Values.leaderElection.resourceLock }}
- --leader-elect-resource-lock={{ .Values.leaderElection.resourceLock }}
{{- end }}
{{- if .Values.leaderElection.resourceName }}
- --leader-elect-resource-name={{ .Values.leaderElection.resourceName }}
{{- end }}
- --leader-elect-resource-namespace={{ .Release.Namespace }}
{{- end }}
{{- end }}

{{/*
Prometheus
*/}}
{{- define "prometheus.enabledCheck" -}}
{{- if and (not .Values.prometheus.enabled) .Values.prometheus.serviceMonitor.enabled -}}
{{- printf "Warning: To enable metric scraping, please set prometheus.enabled to true, as serviceMonitor.enabled is currently set to true." }}
{{- end -}}
{{- end -}}
