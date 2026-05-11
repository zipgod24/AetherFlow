{{- define "aetherflow.commonEnv" -}}
- name: RABBITMQ_URL
  value: {{ .Values.externalServices.rabbitmqURL | quote }}
- name: POSTGRES_DSN
  value: {{ .Values.externalServices.postgresDSN | quote }}
- name: OLLAMA_BASE_URL
  value: {{ .Values.externalServices.ollamaBaseURL | quote }}
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: {{ .Values.externalServices.otlpEndpoint | quote }}
- name: AETHER_LLM_PROVIDER
  value: {{ .Values.llm.provider | quote }}
- name: OLLAMA_CHAT_MODEL
  value: {{ .Values.llm.ollama.chatModel | quote }}
- name: OLLAMA_EMBED_MODEL
  value: {{ .Values.llm.ollama.embedModel | quote }}
- name: AETHER_MASTER_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.masterKeySecret | quote }}
      key: key
      optional: true
{{- end -}}

{{- define "aetherflow.deployment" -}}
{{- $name := .name -}}
{{- $component := .component -}}
{{- $root := .root -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: aetherflow-{{ $name }}
  labels:
    app.kubernetes.io/name: aetherflow
    app.kubernetes.io/component: {{ $name }}
spec:
  replicas: {{ $component.replicas | default 1 }}
  selector:
    matchLabels:
      app.kubernetes.io/name: aetherflow
      app.kubernetes.io/component: {{ $name }}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: aetherflow
        app.kubernetes.io/component: {{ $name }}
    spec:
      containers:
        - name: app
          image: "{{ $root.Values.image.registry }}/{{ $name }}:{{ $root.Values.image.tag }}"
          imagePullPolicy: {{ $root.Values.image.pullPolicy }}
          env:
            {{- include "aetherflow.commonEnv" $root | nindent 12 }}
          resources:
            {{- toYaml $root.Values.resources.default | nindent 12 }}
{{- end -}}
