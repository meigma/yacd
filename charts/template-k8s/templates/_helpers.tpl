{{- define "template-k8s.name" -}}
{{- default "template-k8s" .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "template-k8s.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default "template-k8s" .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "template-k8s.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "template-k8s.selectorLabels" -}}
app.kubernetes.io/name: {{ include "template-k8s.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "template-k8s.labels" -}}
helm.sh/chart: {{ include "template-k8s.chart" . }}
{{ include "template-k8s.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end -}}

{{- define "template-k8s.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "template-k8s.controllerManagerName" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "template-k8s.suffixedName" -}}
{{- $context := .context -}}
{{- $suffix := .suffix -}}
{{- $maxBaseLength := int (sub 62 (len $suffix)) -}}
{{- if lt $maxBaseLength 1 -}}
{{- fail (printf "suffix %q leaves no room for a resource name prefix" $suffix) -}}
{{- end -}}
{{- if and $context.Values.fullnameOverride (gt (len $context.Values.fullnameOverride) $maxBaseLength) -}}
{{- fail (printf "fullnameOverride must be %d characters or fewer when suffixed with %q" $maxBaseLength $suffix) -}}
{{- end -}}
{{- printf "%s-%s" ((include "template-k8s.fullname" $context) | trunc $maxBaseLength | trimSuffix "-") $suffix -}}
{{- end -}}

{{- define "template-k8s.controllerManagerName" -}}
{{- include "template-k8s.suffixedName" (dict "context" . "suffix" "controller-manager") -}}
{{- end -}}

{{- define "template-k8s.managerRoleName" -}}
{{- include "template-k8s.suffixedName" (dict "context" . "suffix" "manager-role") -}}
{{- end -}}

{{- define "template-k8s.managerRoleBindingName" -}}
{{- include "template-k8s.suffixedName" (dict "context" . "suffix" "manager-rolebinding") -}}
{{- end -}}

{{- define "template-k8s.leaderElectionRoleName" -}}
{{- include "template-k8s.suffixedName" (dict "context" . "suffix" "leader-election-role") -}}
{{- end -}}

{{- define "template-k8s.leaderElectionRoleBindingName" -}}
{{- include "template-k8s.suffixedName" (dict "context" . "suffix" "leader-election-rolebinding") -}}
{{- end -}}

{{- define "template-k8s.metricsAuthRoleName" -}}
{{- include "template-k8s.suffixedName" (dict "context" . "suffix" "metrics-auth-role") -}}
{{- end -}}

{{- define "template-k8s.metricsAuthRoleBindingName" -}}
{{- include "template-k8s.suffixedName" (dict "context" . "suffix" "metrics-auth-rolebinding") -}}
{{- end -}}

{{- define "template-k8s.metricsReaderRoleName" -}}
{{- include "template-k8s.suffixedName" (dict "context" . "suffix" "metrics-reader") -}}
{{- end -}}

{{- define "template-k8s.metricsServiceName" -}}
{{- include "template-k8s.suffixedName" (dict "context" . "suffix" "controller-manager-metrics-service") -}}
{{- end -}}

{{- define "template-k8s.kyvernoImagePolicyName" -}}
{{- default (include "template-k8s.suffixedName" (dict "context" . "suffix" "verify-image")) .Values.kyverno.imageVerification.name -}}
{{- end -}}

{{- define "template-k8s.nginxDeploymentAdminRoleName" -}}
{{- include "template-k8s.suffixedName" (dict "context" . "suffix" "nginxdeployment-admin-role") -}}
{{- end -}}

{{- define "template-k8s.nginxDeploymentEditorRoleName" -}}
{{- include "template-k8s.suffixedName" (dict "context" . "suffix" "nginxdeployment-editor-role") -}}
{{- end -}}

{{- define "template-k8s.nginxDeploymentViewerRoleName" -}}
{{- include "template-k8s.suffixedName" (dict "context" . "suffix" "nginxdeployment-viewer-role") -}}
{{- end -}}

{{- define "template-k8s.image" -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}
{{- end -}}

{{- define "template-k8s.validateValues" -}}
{{- $reservedLabels := list "app.kubernetes.io/name" "app.kubernetes.io/instance" "app.kubernetes.io/managed-by" "app.kubernetes.io/version" "helm.sh/chart" "control-plane" -}}
{{- range $source, $labels := dict "commonLabels" .Values.commonLabels "podLabels" .Values.podLabels -}}
{{- range $key, $_ := $labels -}}
{{- if has $key $reservedLabels -}}
{{- fail (printf "%s must not set reserved chart label %q" $source $key) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
