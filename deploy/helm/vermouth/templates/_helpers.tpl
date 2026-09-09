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

{{- define "vermouth.imagePullSecrets" -}}
{{- with .Values.imagePullSecrets }}
imagePullSecrets:
{{- range . }}
  - name: {{ . }}
{{- end }}
{{- end }}
{{- end }}

{{- define "vermouth.storageGuard" -}}
{{- if .Values.storage.guard.enabled }}
{{- if not (regexMatch "^[0-9a-f]{64}$" .Values.storage.markerSHA256) }}
{{- fail "storage.markerSHA256 must be a lowercase SHA256 when the storage guard is enabled" }}
{{- end }}
initContainers:
  - name: verify-storage-root
    image: {{ include "vermouth.requireImage" (dict "name" "storageGuard" "image" .Values.images.storageGuard) }}
    imagePullPolicy: IfNotPresent
    command: [/bin/sh, -ec]
    args:
      - |
        actual="$(cat /var/run/vermouth/storage-marker)"
        [ "$actual" = {{ .Values.storage.markerSHA256 | quote }} ]
    securityContext:
      {{- include "vermouth.containerSecurity" . | nindent 6 }}
    resources:
      requests:
        cpu: 5m
        memory: 8Mi
      limits:
        cpu: 25m
        memory: 16Mi
    volumeMounts:
      - name: storage-marker
        mountPath: /var/run/vermouth/storage-marker
        readOnly: true
{{- end }}
{{- end }}

{{- define "vermouth.storageGuardVolume" -}}
{{- if .Values.storage.guard.enabled }}
- name: storage-marker
  hostPath:
    path: {{ printf "%s/.vermouth-storage" .Values.storage.rootPath | quote }}
    type: File
{{- end }}
{{- end }}
