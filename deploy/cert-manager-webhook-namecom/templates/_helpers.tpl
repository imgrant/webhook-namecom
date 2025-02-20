{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "cert-manager-webhook-namecom.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "cert-manager-webhook-namecom.fullname" -}}
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
{{- define "cert-manager-webhook-namecom.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "cert-manager-webhook-namecom.selfSignedIssuer" -}}
{{ printf "%s-selfsign" (include "cert-manager-webhook-namecom.fullname" .) }}
{{- end -}}

{{- define "cert-manager-webhook-namecom.rootCAIssuer" -}}
{{ printf "%s-ca" (include "cert-manager-webhook-namecom.fullname" .) }}
{{- end -}}

{{- define "cert-manager-webhook-namecom.rootCACertificate" -}}
{{ printf "%s-ca" (include "cert-manager-webhook-namecom.fullname" .) }}
{{- end -}}

{{- define "cert-manager-webhook-namecom.servingCertificate" -}}
{{ printf "%s-webhook-tls" (include "cert-manager-webhook-namecom.fullname" .) }}
{{- end -}}
