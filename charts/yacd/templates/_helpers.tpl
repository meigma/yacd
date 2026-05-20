{{- define "yacd.name" -}}
{{- default "yacd" .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "yacd.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default "yacd" .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "yacd.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "yacd.selectorLabels" -}}
app.kubernetes.io/name: {{ include "yacd.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "yacd.labels" -}}
helm.sh/chart: {{ include "yacd.chart" . }}
{{ include "yacd.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end -}}

{{- define "yacd.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "yacd.controllerManagerName" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "yacd.suffixedName" -}}
{{- $context := .context -}}
{{- $suffix := .suffix -}}
{{- $maxBaseLength := int (sub 62 (len $suffix)) -}}
{{- if lt $maxBaseLength 1 -}}
{{- fail (printf "suffix %q leaves no room for a resource name prefix" $suffix) -}}
{{- end -}}
{{- if and $context.Values.fullnameOverride (gt (len $context.Values.fullnameOverride) $maxBaseLength) -}}
{{- fail (printf "fullnameOverride must be %d characters or fewer when suffixed with %q" $maxBaseLength $suffix) -}}
{{- end -}}
{{- printf "%s-%s" ((include "yacd.fullname" $context) | trunc $maxBaseLength | trimSuffix "-") $suffix -}}
{{- end -}}

{{- define "yacd.controllerManagerName" -}}
{{- include "yacd.suffixedName" (dict "context" . "suffix" "controller-manager") -}}
{{- end -}}

{{- define "yacd.managerRoleName" -}}
{{- include "yacd.suffixedName" (dict "context" . "suffix" "manager-role") -}}
{{- end -}}

{{- define "yacd.managerRoleBindingName" -}}
{{- include "yacd.suffixedName" (dict "context" . "suffix" "manager-rolebinding") -}}
{{- end -}}

{{- define "yacd.leaderElectionRoleName" -}}
{{- include "yacd.suffixedName" (dict "context" . "suffix" "leader-election-role") -}}
{{- end -}}

{{- define "yacd.leaderElectionRoleBindingName" -}}
{{- include "yacd.suffixedName" (dict "context" . "suffix" "leader-election-rolebinding") -}}
{{- end -}}

{{- define "yacd.metricsAuthRoleName" -}}
{{- include "yacd.suffixedName" (dict "context" . "suffix" "metrics-auth-role") -}}
{{- end -}}

{{- define "yacd.metricsAuthRoleBindingName" -}}
{{- include "yacd.suffixedName" (dict "context" . "suffix" "metrics-auth-rolebinding") -}}
{{- end -}}

{{- define "yacd.metricsReaderRoleName" -}}
{{- include "yacd.suffixedName" (dict "context" . "suffix" "metrics-reader") -}}
{{- end -}}

{{- define "yacd.metricsServiceName" -}}
{{- include "yacd.suffixedName" (dict "context" . "suffix" "controller-manager-metrics-service") -}}
{{- end -}}

{{- define "yacd.kyvernoImagePolicyName" -}}
{{- default (include "yacd.suffixedName" (dict "context" . "suffix" "verify-image")) .Values.kyverno.imageVerification.name -}}
{{- end -}}

{{- define "yacd.image" -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}
{{- end -}}

{{- define "yacd.validateValues" -}}
{{- $reservedLabels := list "app.kubernetes.io/name" "app.kubernetes.io/instance" "app.kubernetes.io/managed-by" "app.kubernetes.io/version" "helm.sh/chart" "control-plane" -}}
{{- range $source, $labels := dict "commonLabels" .Values.commonLabels "podLabels" .Values.podLabels -}}
{{- range $key, $_ := $labels -}}
{{- if has $key $reservedLabels -}}
{{- fail (printf "%s must not set reserved chart label %q" $source $key) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
