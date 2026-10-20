{{/* Names and labels. */}}

{{- define "exciter.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "exciter.fullname" -}}
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

{{- define "exciter.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "exciter.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: exciter
{{- end -}}

{{- define "exciter.selectorLabels" -}}
app.kubernetes.io/name: {{ include "exciter.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "exciter.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "exciter.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "exciter.secretName" -}}
{{- default (printf "%s-secrets" (include "exciter.fullname" .)) .Values.secrets.existingSecret -}}
{{- end -}}

{{- define "exciter.image" -}}
{{- $img := index .root.Values.images .name -}}
{{- printf "%s:%s" $img.repository (default .root.Chart.AppVersion $img.tag) -}}
{{- end -}}

{{/*
Where the database is. The CNPG cluster and the bundled Postgres name their own
services, so the answer depends on which one the chart brought.
*/}}

{{- define "exciter.cnpgName" -}}
{{- printf "%s-postgres" (include "exciter.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Writes, migrations and admin work: the CNPG -rw service, which always points
at the current primary.
*/}}
{{- define "exciter.dbHost" -}}
{{- if and .Values.cnpg.enabled .Values.postgresql.enabled -}}
{{- fail "cnpg.enabled and postgresql.enabled are both true. Turn one of them off: the workloads can only be pointed at one database." -}}
{{- end -}}
{{- if .Values.cnpg.enabled -}}
{{- printf "%s-rw" (include "exciter.cnpgName" .) -}}
{{- else if .Values.postgresql.enabled -}}
{{- printf "%s-postgresql" (include "exciter.fullname" .) -}}
{{- else -}}
{{- required "Set database.host, or enable cnpg or the bundled postgresql: the chart does not guess where the database is." .Values.database.host -}}
{{- end -}}
{{- end -}}

{{/*
Reads: the CNPG -ro service when there is a replica behind it. With one
instance -ro has no endpoints, so reads stay on -rw. Comma separated, because
a template can only return a string.
*/}}
{{- define "exciter.replicaHosts" -}}
{{- $hosts := .Values.database.replicaHosts | default list -}}
{{- if and .Values.cnpg.enabled .Values.cnpg.readReplicas (gt (int (.Values.cnpg.spec.instances | default 1)) 1) -}}
{{- $hosts = append $hosts (printf "%s-ro" (include "exciter.cnpgName" .)) -}}
{{- end -}}
{{- join "," $hosts -}}
{{- end -}}

{{- define "exciter.ownerRole" -}}
{{- $role := .Values.database.ownerRole -}}
{{- if and .Values.cnpg.enabled (eq $role "postgres") -}}
{{- fail "database.ownerRole is postgres, which CNPG keeps for its own superuser. Leave database.ownerRole empty to get exciter_owner, or set another name." -}}
{{- end -}}
{{- default (ternary "exciter_owner" "postgres" .Values.cnpg.enabled) $role -}}
{{- end -}}

{{/*
Valkey: the bundled one, yours, or none. Read-your-writes positions have to be
shared by every api pod once reads can reach a replica, so more than one api
pod with replicas and no Valkey is refused rather than quietly inconsistent.
*/}}
{{- define "exciter.valkeyHost" -}}
{{- if .Values.valkey.bundled -}}
{{- printf "%s-valkey" (include "exciter.fullname" .) -}}
{{- else -}}
{{- .Values.valkey.host -}}
{{- end -}}
{{- end -}}

{{- define "exciter.valkeyAuth" -}}
{{- if or .Values.valkey.bundled .Values.valkey.auth -}}true{{- end -}}
{{- end -}}

{{- define "exciter.checkValkey" -}}
{{- if and (include "exciter.replicaHosts" .) (gt (int .Values.api.replicas) 1) (not (include "exciter.valkeyHost" .)) -}}
{{- fail "Reads go to replicas and api.replicas is above 1, so read-your-writes needs a Valkey all api pods share. Set valkey.host, or valkey.bundled for a trial." -}}
{{- end -}}
{{- end -}}

{{/*
The basic-auth Secrets holding one password each. CNPG reads them in exactly
this shape, and the bundled database and the Jobs read the same ones.
*/}}
{{- define "exciter.credentialSecret" -}}
{{- $suffix := dict "dbOwner" "db-owner" "dbApp" "db-app" "dbAdmin" "db-admin" "valkey" "valkey" "secretKey" "secret-key" "bootstrapAdmin" "bootstrap-admin" "s3" "s3" "s3Read" "s3-read" -}}
{{- default (printf "%s-%s" (include "exciter.fullname" .root) (get $suffix .name)) (get .root.Values.secrets.names .name) -}}
{{- end -}}

{{/*
A password from its Secret, as an env var the URLs below expand with $(NAME).
Kubernetes only expands variables declared earlier in the same list.
*/}}
{{- define "exciter.passwordEnv" -}}
- name: {{ .var }}
  valueFrom:
    secretKeyRef:
      name: {{ include "exciter.credentialSecret" (dict "root" .root "name" .name) }}
      key: password
{{- end -}}

{{/* Generated passwords are alphanumeric, so they need no escaping here. */}}
{{- define "exciter.dbURL" -}}
{{- $v := .root.Values.database -}}
{{- printf "postgres://%s:$(%s)@%s:%d/%s?sslmode=%s" .role .var .host (int $v.port) $v.name $v.sslmode -}}
{{- end -}}

{{- define "exciter.valkeyEnv" -}}
{{- $host := include "exciter.valkeyHost" . -}}
{{- if $host -}}
{{- $port := int .Values.valkey.port -}}
{{- $db := int .Values.valkey.db -}}
{{- if include "exciter.valkeyAuth" . -}}
{{ include "exciter.passwordEnv" (dict "root" . "name" "valkey" "var" "VALKEY_PASSWORD") }}
- name: EXCITER_VALKEY_URL
  value: {{ printf "redis://:$(VALKEY_PASSWORD)@%s:%d/%d" $host $port $db | quote }}
{{- else -}}
- name: EXCITER_VALKEY_URL
  value: {{ printf "redis://%s:%d/%d" $host $port $db | quote }}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Annotations that order a hook the same way under Helm and under Argo CD. Each
tool reads only the half meant for it.
*/}}
{{- define "exciter.hookAnnotations" -}}
{{- if .helm -}}
helm.sh/hook: {{ .helm }}
helm.sh/hook-weight: {{ .weight | quote }}
helm.sh/hook-delete-policy: before-hook-creation
{{- end }}
{{- if .argo }}
argocd.argoproj.io/hook: {{ .argo }}
argocd.argoproj.io/hook-delete-policy: BeforeHookCreation
{{- end }}
argocd.argoproj.io/sync-wave: {{ .weight | quote }}
{{- end -}}

{{- define "exciter.appURL" -}}
{{- printf "%s://%s" (ternary "https" "http" .Values.ingress.tls.enabled) .Values.ingress.host -}}
{{- end -}}

{{/* public=reachable pairs from a map, as EXCITER_*_BACKCHANNEL reads them. */}}
{{- define "exciter.rewrites" -}}
{{- $pairs := list -}}
{{- range $public, $reachable := . -}}
{{- $pairs = append $pairs (printf "%s=%s" $public $reachable) -}}
{{- end -}}
{{- join "," $pairs -}}
{{- end -}}

{{/*
What outbound calls may reach inside the network: what is named, and every host a backchannel maps to, so wiring a
partner is one value.
*/}}
{{- define "exciter.outboundAllow" -}}
{{- $hosts := .Values.network.outboundAllow | default list -}}
{{- range (list .Values.armature.backchannel .Values.stator.backchannel .Values.auth.oidcBackchannel) -}}
{{- range $_, $reachable := . -}}
{{- $hosts = append $hosts (urlParse $reachable).hostname -}}
{{- end -}}
{{- end -}}
{{- join "," ($hosts | uniq) -}}
{{- end -}}

{{/*
The environment every workload shares, built once so a setting cannot reach
the api and not the worker: they load the same configuration. Credentials are
not here; exciter.secretEnv hands them over by reference.
*/}}
{{- define "exciter.env" -}}
{{- if and (or .Values.armature.url .Values.stator.url) (not .Values.jobs.bootstrap.enabled) -}}
{{- fail "armature.url and stator.url are connected by the bootstrap Job, which is off. Turn jobs.bootstrap.enabled on, or leave them blank and connect the partners under Settings." -}}
{{- end -}}
EXCITER_ENV: {{ .Values.env | quote }}
EXCITER_LOG_LEVEL: {{ .Values.logLevel | quote }}
EXCITER_APP_URL: {{ include "exciter.appURL" . | quote }}
EXCITER_HTTP_ADDR: {{ printf ":%d" (int .Values.api.port) | quote }}
EXCITER_METRICS_ADDR: {{ printf ":%d" (int .Values.telemetry.metricsPort) | quote }}
{{- with .Values.installation.name }}
EXCITER_NAME: {{ . | quote }}
{{- end }}
{{- with .Values.telemetry.otel.endpoint }}
EXCITER_OTEL_ENDPOINT: {{ . | quote }}
EXCITER_OTEL_SAMPLE_RATIO: {{ $.Values.telemetry.otel.sampleRatio | quote }}
{{- end }}
{{- with .Values.network.trustedProxies }}
EXCITER_TRUSTED_PROXIES: {{ join "," . | quote }}
{{- end }}
{{- with .Values.network.corsOrigins }}
EXCITER_CORS_ORIGINS: {{ join "," . | quote }}
{{- end }}
{{- with include "exciter.outboundAllow" . }}
EXCITER_OUTBOUND_ALLOW: {{ . | quote }}
{{- end }}
{{- with include "exciter.rewrites" .Values.armature.backchannel }}
EXCITER_ARMATURE_BACKCHANNEL: {{ . | quote }}
{{- end }}
{{- with include "exciter.rewrites" .Values.stator.backchannel }}
EXCITER_STATOR_BACKCHANNEL: {{ . | quote }}
{{- end }}
{{- if .Values.armature.tokenExchange }}
EXCITER_ARMATURE_TOKEN_EXCHANGE: "true"
{{- end }}
{{- if .Values.stator.tokenExchange }}
EXCITER_STATOR_TOKEN_EXCHANGE: "true"
{{- end }}
{{- with include "exciter.rewrites" .Values.auth.oidcBackchannel }}
EXCITER_OIDC_BACKCHANNEL: {{ . | quote }}
{{- end }}
EXCITER_SESSION_COOKIE: {{ .Values.auth.sessionCookie | quote }}
EXCITER_SECURE_COOKIES: {{ .Values.auth.secureCookies | quote }}

# The pools. The URLs carry credentials and come from the Secrets.
EXCITER_DB_PRIMARY_MAX_CONNS: {{ .Values.database.pool.primaryMaxConns | quote }}
EXCITER_DB_REPLICA_MAX_CONNS: {{ .Values.database.pool.replicaMaxConns | quote }}
EXCITER_DB_MIN_CONNS: {{ .Values.database.pool.minConns | quote }}
EXCITER_DB_CONN_MAX_LIFETIME: {{ .Values.database.pool.maxConnLifetime | quote }}
EXCITER_DB_HEALTH_INTERVAL: {{ .Values.database.pool.healthInterval | quote }}
EXCITER_DB_MAX_REPLICA_LAG: {{ .Values.database.pool.maxReplicaLag | quote }}
EXCITER_DB_REPLICA_LAG_SAMPLES: {{ .Values.database.pool.replicaLagSamples | quote }}
EXCITER_READ_YOUR_WRITES_TTL: {{ .Values.database.readYourWritesTTL | quote }}
EXCITER_UPLOAD_LIMIT: {{ .Values.attachments.uploadLimit | quote }}
{{- if .Values.render.enabled }}
EXCITER_RENDER_URL: {{ printf "http://%s-render:8090" (include "exciter.fullname" .) | quote }}
EXCITER_RENDER_TIMEOUT: {{ .Values.render.timeout | quote }}
EXCITER_RENDER_CONCURRENCY: {{ .Values.render.concurrency | quote }}
EXCITER_RENDER_MAX_SIZE: {{ .Values.render.maxSize | quote }}
{{- end }}
{{- if .Values.kroki.enabled }}
EXCITER_KROKI_URL: {{ printf "http://%s-kroki:8000" (include "exciter.fullname" .) | quote }}
EXCITER_KROKI_TIMEOUT: {{ .Values.kroki.timeout | quote }}
EXCITER_KROKI_CONCURRENCY: {{ .Values.kroki.concurrency | quote }}
{{- end }}
EXCITER_RETAIN_AUDIT: {{ .Values.retention.audit | quote }}
{{- with .Values.mail.smtpAddr }}
EXCITER_SMTP_ADDR: {{ . | quote }}
EXCITER_MAIL_FROM: {{ $.Values.mail.from | quote }}
{{- end }}

{{- if include "exciter.s3Enabled" . }}
EXCITER_S3_ENDPOINT: {{ include "exciter.s3Endpoint" . | quote }}
EXCITER_S3_BUCKET: {{ .Values.s3.bucket | quote }}
EXCITER_S3_REGION: {{ .Values.s3.region | quote }}
EXCITER_S3_USE_SSL: {{ ternary "false" (toString .Values.s3.useSSL) .Values.seaweedfs.enabled | quote }}
{{- end }}
{{- end -}}

{{/* The credentials, by reference. Never rendered into a pod spec. */}}
{{- define "exciter.secretEnv" -}}
{{- include "exciter.checkValkey" . -}}
{{- $v := .Values.database -}}
{{- $host := include "exciter.dbHost" . -}}
{{ include "exciter.passwordEnv" (dict "root" . "name" "dbApp" "var" "DB_APP_PASSWORD") }}
{{ include "exciter.passwordEnv" (dict "root" . "name" "dbAdmin" "var" "DB_ADMIN_PASSWORD") }}
{{ include "exciter.passwordEnv" (dict "root" . "name" "secretKey" "var" "EXCITER_SECRET_KEY") }}
- name: EXCITER_DB_PRIMARY_URL
  value: {{ include "exciter.dbURL" (dict "root" . "role" $v.appRole "var" "DB_APP_PASSWORD" "host" $host) | quote }}
- name: EXCITER_DB_ADMIN_URL
  value: {{ include "exciter.dbURL" (dict "root" . "role" $v.adminRole "var" "DB_ADMIN_PASSWORD" "host" $host) | quote }}
{{- with include "exciter.replicaHosts" . }}
- name: EXCITER_DB_REPLICA_URLS
  {{- $urls := list }}
  {{- range splitList "," . }}
  {{- $urls = append $urls (include "exciter.dbURL" (dict "root" $ "role" $v.appRole "var" "DB_APP_PASSWORD" "host" .)) }}
  {{- end }}
  value: {{ join "," $urls | quote }}
{{- end }}
{{- with include "exciter.valkeyEnv" . }}
{{ . }}
{{- end }}
{{- with include "exciter.s3KeyEnv" . }}
{{ . }}
{{- end }}
{{- end -}}

{{/* The bucket's keys, for every workload that reads or writes files. */}}
{{- define "exciter.s3KeyEnv" -}}
{{- if .Values.seaweedfs.enabled }}
- name: EXCITER_S3_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "exciter.credentialSecret" (dict "root" . "name" "s3") }}
      key: username
- name: EXCITER_S3_SECRET_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "exciter.credentialSecret" (dict "root" . "name" "s3") }}
      key: password
{{- else if .Values.s3.enabled }}
- name: EXCITER_S3_ACCESS_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "exciter.secretName" . }}
      key: s3-access-key
- name: EXCITER_S3_SECRET_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "exciter.secretName" . }}
      key: s3-secret-key
{{- end }}
{{- end -}}

{{/* The partners the bootstrap and seed Jobs connect when nothing is connected yet. */}}
{{- define "exciter.partnerEnv" -}}
{{- with .Values.armature.url }}
- name: EXCITER_BOOTSTRAP_ARMATURE_URL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.stator.url }}
- name: EXCITER_BOOTSTRAP_STATOR_URL
  value: {{ . | quote }}
{{- end }}
{{- end -}}

{{/* The administrator's address when none is set: a name at the chart's own host. */}}
{{- define "exciter.bootstrapEmail" -}}
{{- default (printf "admin@%s" .Values.ingress.host) .Values.bootstrap.admin.email -}}
{{- end -}}

{{/* Uploads work with an S3 of one's own or with the bundled SeaweedFS. */}}
{{- define "exciter.s3Enabled" -}}
{{- if or .Values.s3.enabled .Values.seaweedfs.enabled -}}true{{- end -}}
{{- end -}}

{{- define "exciter.s3Endpoint" -}}
{{- if .Values.seaweedfs.enabled -}}
{{- include "exciter.checkBundledS3" . -}}
{{- printf "%s-seaweedfs-s3:%d" .Release.Name (int .Values.seaweedfs.s3.port) -}}
{{- else -}}
{{- required "Set s3.endpoint, the host and port of the bucket's S3 API, or turn s3.enabled off." .Values.s3.endpoint -}}
{{- end -}}
{{- end -}}

{{/*
The subchart reads its keys from Secrets it names as literals, and the api and
the secrets Job use secrets.names. Nothing joins the two, so a change to one
alone would leave the store signing with keys the api does not have.
*/}}
{{- define "exciter.checkBundledS3" -}}
{{- $creds := (((.Values.seaweedfs.s3).credentials) | default dict) -}}
{{- $admin := $creds.admin | default dict -}}
{{- $read := $creds.read | default dict -}}
{{- if or (ne ($admin.existingSecret | default "") .Values.secrets.names.s3) (ne ($read.existingSecret | default "") .Values.secrets.names.s3Read) -}}
{{- fail (printf "seaweedfs.s3.credentials.admin.existingSecret and .read.existingSecret must be %q and %q, the same as secrets.names.s3 and secrets.names.s3Read: the store reads its keys from the first pair of names and the api from the second, so every upload would be refused as unsigned." .Values.secrets.names.s3 .Values.secrets.names.s3Read) -}}
{{- end -}}
{{- if or (ne ($admin.accessKeyKey | default "") "username") (ne ($admin.secretKeyKey | default "") "password") (ne ($read.accessKeyKey | default "") "username") (ne ($read.secretKeyKey | default "") "password") -}}
{{- fail "seaweedfs.s3.credentials.admin and .read must read the keys username and password, which is where the secrets Job puts the access key and the secret key." -}}
{{- end -}}
{{- end -}}
