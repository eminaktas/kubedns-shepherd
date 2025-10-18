{{- define "chart.name" -}}
{{- if .Chart }}
  {{- if .Chart.Name }}
    {{- .Chart.Name | trunc 63 | trimSuffix "-" }}
  {{- else if .Values.nameOverride }}
    {{ .Values.nameOverride | trunc 63 | trimSuffix "-" }}
  {{- else }}
    kubedns-shepherd
  {{- end }}
{{- else }}
  kubedns-shepherd
{{- end }}
{{- end }}


{{- define "chart.labels" -}}
{{- if .Chart.AppVersion -}}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{- if .Chart.Version }}
helm.sh/chart: {{ .Chart.Version | quote }}
{{- end }}
app.kubernetes.io/name: {{ include "chart.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}


{{- define "chart.selectorLabels" -}}
app.kubernetes.io/name: {{ include "chart.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}


{{- define "chart.hasMutatingWebhooks" -}}
{{- $hasMutating := false }}
{{- range . }}
  {{- if eq .type "mutating" }}
    $hasMutating = true }}{{- end }}
{{- end }}
{{ $hasMutating }}}}{{- end }}


{{- define "chart.hasValidatingWebhooks" -}}
{{- $hasValidating := false }}
{{- range . }}
  {{- if eq .type "validating" }}
    $hasValidating = true }}{{- end }}
{{- end }}
{{ $hasValidating }}}}{{- end }}

{{/*
Leader Election
*/}}
{{- define "kubedns-shepherd.leaderElection" -}}
{{- if .Values.container.leaderElection -}}
- --leader-elect={{ .Values.container.leaderElection.enable }}
{{- if .Values.container.leaderElection.leaseDuration }}
- --leader-elect-lease-duration={{ .Values.container.leaderElection.leaseDuration }}
{{- end }}
{{- if .Values.container.leaderElection.renewDeadline }}
- --leader-elect-renew-deadline={{ .Values.container.leaderElection.renewDeadline }}
{{- end }}
{{- if .Values.container.leaderElection.retryPeriod }}
- --leader-elect-retry-period={{ .Values.container.leaderElection.retryPeriod }}
{{- end }}
{{- if .Values.container.leaderElection.resourceLock }}
- --leader-elect-resource-lock={{ .Values.container.leaderElection.resourceLock }}
{{- end }}
{{- if .Values.container.leaderElection.resourceName }}
- --leader-elect-resource-name={{ .Values.container.leaderElection.resourceName }}
{{- end }}
- --leader-elect-resource-namespace={{ .Release.Namespace }}
{{- end }}
{{- end }}
