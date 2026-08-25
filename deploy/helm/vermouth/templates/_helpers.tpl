{{- define "vermouth.labels" -}}
app.kubernetes.io/name: vermouth
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
{{- end }}

{{- define "vermouth.componentLabels" -}}
{{ include "vermouth.labels" .root }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{- define "vermouth.podSecurity" -}}
runAsNonRoot: true
seccompProfile:
  type: RuntimeDefault
{{- end }}

{{- define "vermouth.containerSecurity" -}}
allowPrivilegeEscalation: false
capabilities:
  drop:
    - ALL
readOnlyRootFilesystem: true
{{- end }}

{{- define "vermouth.requireImage" -}}
{{- if not .image -}}
{{- fail (printf "image for %s is required" .name) -}}
{{- end -}}
{{- .image -}}
{{- end }}
