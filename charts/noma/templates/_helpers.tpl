{{/* Labels every object carries. */}}
{{- define "noma.labels" -}}
app.kubernetes.io/name: noma
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end }}

{{/*
The value of key in the existing Secret name, or the generated default.
Both are base64, as in a Secret's data.
*/}}
{{- define "noma.keep" -}}
{{- $s := lookup "v1" "Secret" .ns .name -}}
{{- if and $s (index $s.data .key) -}}
{{ index $s.data .key }}
{{- else -}}
{{ .default | b64enc }}
{{- end -}}
{{- end }}
