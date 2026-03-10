# Observability — monitoring Namespace

This document covers the full observability stack deployed in the `monitoring` namespace: Prometheus, Grafana, Loki, Tempo, Alertmanager, and log shipping (Promtail/Grafana Alloy). All Doki Stack services expose metrics at `/metrics` and structured JSON logs with `trace_id` for correlation.

**References:**
- `00-overview.md` — Namespace layout, Phase 0 scope
- `01-cluster-and-networking.md` — Port mappings (Grafana NodePort 30300)
- `03-security.md` — Cilium policies for Prometheus scraping
- `internal-docs/implementation-plan/04-phase2-agents-and-hitl.md` — Trace visibility requirements

---

## 1. Prometheus

### 1.1 Overview

| Property | Value |
|----------|-------|
| Helm chart | `prometheus-community/kube-prometheus-stack` |
| Namespace | `monitoring` |
| Includes | Prometheus, Alertmanager, Grafana, node-exporter, kube-state-metrics |
| Repo | `helm repo add prometheus-community https://prometheus-community.github.io/helm-charts` |

### 1.2 Helm Values

File: `helm-values/prometheus.yaml`

```yaml
# kube-prometheus-stack — dev configuration
# Helm: helm install monitoring prometheus-community/kube-prometheus-stack -n monitoring -f helm-values/prometheus.yaml

# --- Prometheus ---
prometheus:
  enabled: true
  prometheusSpec:
    retention: 15d  # 30d for prod overlay
    retentionSize: 10GB
    storageSpec:
      volumeClaimTemplate:
        spec:
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: 10Gi  # 50Gi for prod overlay
          storageClassName: ""
    resources:
      requests:
        memory: 256Mi
        cpu: 250m
      limits:
        memory: 512Mi
        cpu: 500m
    # Service monitors for all doki-* services
    serviceMonitorSelectorNilUsesHelmValues: false
    serviceMonitorSelector: {}
    serviceMonitorNamespaceSelector: {}
    podMonitorSelectorNilUsesHelmValues: false
    podMonitorSelector: {}
    podMonitorNamespaceSelector: {}
    # Additional scrape configs for custom services
    additionalScrapeConfigs:
      - job_name: 'doki-mcp'
        kubernetes_sd_configs:
          - role: endpoints
            namespaces:
              names: [doki-mcp]
        relabel_configs:
          - source_labels: [__meta_kubernetes_endpoint_port_name]
            action: keep
            regex: metrics
          - source_labels: [__meta_kubernetes_pod_ip]
            target_label: __address__
            replacement: ${1}:9090
          - source_labels: [__meta_kubernetes_namespace]
            target_label: namespace
          - source_labels: [__meta_kubernetes_pod_name]
            target_label: pod
          - source_labels: [__meta_kubernetes_pod_label_app]
            target_label: service
        metric_relabel_configs:
          - source_labels: [__name__]
            regex: 'go_.*|process_.*'
            action: drop
      - job_name: 'doki-platform'
        kubernetes_sd_configs:
          - role: endpoints
            namespaces:
              names: [doki-platform]
        relabel_configs:
          - source_labels: [__meta_kubernetes_endpoint_port_name]
            action: keep
            regex: metrics
          - source_labels: [__meta_kubernetes_pod_ip]
            target_label: __address__
            replacement: ${1}:9090
          - source_labels: [__meta_kubernetes_namespace]
            target_label: namespace
          - source_labels: [__meta_kubernetes_pod_name]
            target_label: pod
      - job_name: 'doki-agents'
        kubernetes_sd_configs:
          - role: endpoints
            namespaces:
              names: [doki-agents]
        relabel_configs:
          - source_labels: [__meta_kubernetes_endpoint_port_name]
            action: keep
            regex: metrics
          - source_labels: [__meta_kubernetes_pod_ip]
            target_label: __address__
            replacement: ${1}:9090
          - source_labels: [__meta_kubernetes_namespace]
            target_label: namespace
          - source_labels: [__meta_kubernetes_pod_name]
            target_label: pod
      - job_name: 'postgres-exporter'
        static_configs:
          - targets: ['postgres-exporter.doki-data.svc.cluster.local:9187']
        relabel_configs:
          - target_label: job
            replacement: postgresql
      - job_name: 'rabbitmq'
        # RabbitMQ 4 management plugin exposes Prometheus at :15692; or use rabbitmq_prometheus
        static_configs:
          - targets: ['rabbitmq.doki-data.svc.cluster.local:15692']
        relabel_configs:
          - target_label: job
            replacement: rabbitmq
      - job_name: 'qdrant'
        static_configs:
          - targets: ['qdrant.doki-data.svc.cluster.local:9090']
        relabel_configs:
          - target_label: job
            replacement: qdrant
      - job_name: 'minio'
        metrics_path: /minio/v2/metrics/cluster
        static_configs:
          - targets: ['minio.doki-data.svc.cluster.local:9000']
        scheme: http
        relabel_configs:
          - target_label: job
            replacement: minio
      - job_name: 'dragonfly'
        static_configs:
          - targets: ['dragonfly-exporter.doki-data.svc.cluster.local:9121']
        relabel_configs:
          - target_label: job
            replacement: dragonfly
```

**Note:** The scrape configs above assume:
- Doki services expose `/metrics` on port 9090 with a named port `metrics` in the Service.
- `postgres_exporter` is deployed as a sidecar or separate deployment (see PostgreSQL section).
- RabbitMQ exposes Prometheus metrics on port 15692 (management plugin).
- Qdrant exposes metrics on 9090.
- MinIO exposes metrics at `/minio/v2/metrics/cluster`.
- `redis_exporter` or `dragonfly_exporter` for Dragonfly (port 9121).

If a service uses a different port or path, adjust the scrape config accordingly.

### 1.3 Prod Override

File: `helm-values/prometheus-prod.yaml` (or overlay)

```yaml
prometheus:
  prometheusSpec:
    retention: 30d
    storageSpec:
      volumeClaimTemplate:
        spec:
          resources:
            requests:
              storage: 50Gi
```

### 1.4 Alertmanager

```yaml
# Add to helm-values/prometheus.yaml (same file)
alertmanager:
  enabled: true
  alertmanagerSpec:
    resources:
      requests:
        memory: 64Mi
        cpu: 100m
      limits:
        memory: 128Mi
        cpu: 200m
  config:
    global:
      resolve_timeout: 5m
    route:
      group_by: ['alertname', 'namespace', 'service']
      group_wait: 30s
      group_interval: 5m
      repeat_interval: 4h
      receiver: 'slack-dev'  # pagerduty-prod for prod
      routes:
        - match:
            severity: critical
          receiver: 'pagerduty-prod'
          continue: true
    receivers:
      - name: 'slack-dev'
        slack_configs:
          - api_url: '${SLACK_WEBHOOK_URL}'
            channel: '#doki-alerts'
            send_resolved: true
      - name: 'pagerduty-prod'
        pagerduty_configs:
          - service_key: '${PAGERDUTY_SERVICE_KEY}'
            send_resolved: true
```

**Secrets:** `SLACK_WEBHOOK_URL` and `PAGERDUTY_SERVICE_KEY` must be injected via Vault/ESO. Do not commit real values.

### 1.5 Node Exporter and Kube State Metrics

```yaml
# Add to helm-values/prometheus.yaml
nodeExporter:
  enabled: true

kubeStateMetrics:
  enabled: true
  resources:
    requests:
      memory: 64Mi
      cpu: 100m
    limits:
      memory: 128Mi
      cpu: 200m
```

---

## 2. Grafana

### 2.1 Overview

Grafana is **bundled** with `kube-prometheus-stack`. For standalone deployment (e.g., custom provisioning), use `grafana/grafana` chart.

| Property | Value |
|----------|-------|
| Source | Bundled with kube-prometheus-stack |
| Namespace | `monitoring` |
| NodePort | 30300 |
| Default user | admin |

### 2.2 Helm Values (Bundled)

Add to `helm-values/prometheus.yaml`:

```yaml
grafana:
  enabled: true
  adminUser: admin
  adminPassword: ""  # Set via existingSecret in prod; use default for dev
  # adminPassword: "prom-operator"  # Dev default; NEVER in prod
  persistence:
    enabled: true
    size: 5Gi
    storageClassName: ""
  service:
    type: NodePort
    nodePort: 30300
  resources:
    requests:
      memory: 128Mi
      cpu: 100m
    limits:
      memory: 256Mi
      cpu: 250m
  # Datasources: Prometheus, Loki, Tempo
  additionalDataSources:
    - name: Loki
      type: loki
      url: http://loki.monitoring.svc.cluster.local:3100
      access: proxy
      isDefault: false
    - name: Tempo
      type: tempo
      url: http://tempo.monitoring.svc.cluster.local:3100
      access: proxy
      isDefault: false
      jsonData:
        httpMethod: GET
        tracesToLogs:
          datasourceUid: loki
          filterByTraceID: true
          mapTagNamesEnabled: false
        tracesToMetrics:
          datasourceUid: prometheus
          spanStartTimeShift: -1h
          spanEndTimeShift: 1h
  # Dashboard provisioning from ConfigMaps
  sidecar:
    dashboards:
      enabled: true
      searchNamespace: ALL
      provider:
        foldersFromFilesStructure: true
    datasources:
      defaultDatasourceEnabled: true
```

### 2.3 Pre-Built Dashboards

Provision dashboards via ConfigMaps with label `grafana_dashboard: "1"`. The sidecar automatically discovers and loads them.

| Dashboard | Source | Purpose |
|-----------|--------|---------|
| Kubernetes cluster overview | `grafana/grafana` — built-in 15757, 15758, 15759 | Cluster, node, pod metrics |
| PostgreSQL | `prometheus-community/postgresql-dashboard` (id: 9628) | Via postgres_exporter |
| MinIO | `minio/minio-dashboard` or custom | MinIO cluster metrics |
| RabbitMQ | `rabbitmq/rabbitmq-dashboard` (id: 10991) | Queue depth, message rates |
| Dragonfly/Redis | `redis/redis-dashboard` (id: 11835) | Cache hit rate, memory |
| MCP request latency | Custom | `http_request_duration_seconds` by service |
| Agent task duration | Custom | `agent_task_duration_seconds` histogram |
| Approval SLA | Custom | `approval_pending_duration_seconds` |
| RLS validation | Custom | `rls_validation_total`, `rls_violation_total` |
| vLLM (prod) | Custom | Token throughput, queue depth, latency p99 |

**Custom dashboard UIDs:** Store in `base/grafana-dashboards/` as ConfigMaps. Example:

```yaml
# base/grafana-dashboards/mcp-latency.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-dashboard-mcp-latency
  namespace: monitoring
  labels:
    grafana_dashboard: "1"
data:
  mcp-latency.json: |
    {
      "title": "MCP Request Latency",
      "uid": "mcp-latency",
      "panels": [
        {
          "title": "p99 Latency by Service",
          "type": "timeseries",
          "targets": [{
            "expr": "histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{namespace=\"doki-mcp\"}[5m])) by (le, service))",
            "legendFormat": "{{service}}"
          }]
        }
      ]
    }
```

---

## 3. Loki (Log Aggregation)

### 3.1 Overview

| Property | Value |
|----------|-------|
| Helm chart | `grafana/loki` |
| Namespace | `monitoring` |
| Repo | `helm repo add grafana https://grafana.github.io/helm-charts` |

### 3.2 Helm Values

File: `helm-values/loki.yaml`

```yaml
# Grafana Loki — dev configuration
# Helm: helm install loki grafana/loki -n monitoring -f helm-values/loki.yaml

loki:
  auth_enabled: false
  commonConfig:
    replication_factor: 1  # Single-binary mode for dev
  storage:
    type: filesystem  # S3 for prod (MinIO)
    # Prod: type: s3
    # Prod: bucketNames:
    #   chunks: loki-chunks
    #   ruler: loki-ruler
    # Prod: s3:
    #   endpoint: minio.doki-data.svc.cluster.local:9000
    #   region: us-east-1
    #   secretAccessKey: (from secret)
    #   accessKeyId: (from secret)
  schemaConfig:
    configs:
      - from: "2024-01-01"
        store: tsdb
        object_store: filesystem  # s3 for prod
        schema: v13
        index:
          prefix: loki_index_
          period: 24h
  retention:
    enabled: true
    retention_period: 168h  # 7d dev; 720h (30d) for prod
  limits_config:
    retention_period: 168h
  resources:
    requests:
      memory: 128Mi
      cpu: 100m
    limits:
      memory: 256Mi
      cpu: 250m
  singleBinary:
    replicas: 1
```

### 3.3 Prod Override

```yaml
# helm-values/loki-prod.yaml
loki:
  commonConfig:
    replication_factor: 2
  storage:
    type: s3
    bucketNames:
      chunks: loki-chunks
      ruler: loki-ruler
    s3:
      endpoint: minio.doki-data.svc.cluster.local:9000
      region: us-east-1
      # accessKeyId, secretAccessKey from existingSecret
  schemaConfig:
    configs:
      - from: "2024-01-01"
        store: tsdb
        object_store: s3
        schema: v13
        index:
          prefix: loki_index_
          period: 24h
  retention:
    retention_period: 720h  # 30d
  limits_config:
    retention_period: 720h
  resources:
    requests:
      memory: 256Mi
      cpu: 250m
    limits:
      memory: 512Mi
      cpu: 500m
```

### 3.4 Log Shipping: Promtail or Grafana Alloy

#### Option A: Promtail

File: `helm-values/promtail.yaml`

```yaml
# grafana/promtail
# Helm: helm install promtail grafana/promtail -n monitoring -f helm-values/promtail.yaml

config:
  snippets:
    pipelineStages:
      - json:
          expressions:
            level: level
            message: message
            service: service
            org_id: org_id
            trace_id: trace_id
            span_id: span_id
      - labels:
          level:
          service:
          org_id:
      - output:
          source: message
  clients:
    - url: http://loki.monitoring.svc.cluster.local:3100/loki/api/v1/push
  positions:
    filename: /tmp/positions.yaml
  scrape_configs:
    - job_name: kubernetes-pods
      pipeline_stages:
        - cri: {}
        - json:
            expressions:
              level: level
              message: message
              service: service
              org_id: org_id
              trace_id: trace_id
              span_id: span_id
        - labels:
            level:
            service:
            org_id:
      kubernetes_sd_configs:
        - role: pod
      relabel_configs:
        - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
          action: keep
          regex: true
        - source_labels: [__meta_kubernetes_namespace]
          target_label: namespace
        - source_labels: [__meta_kubernetes_pod_name]
          target_label: pod
        - source_labels: [__meta_kubernetes_pod_container_name]
          target_label: container
        - replacement: /var/log/pods/$1/*.log
          separator: /
          source_labels:
            - __meta_kubernetes_pod_uid
            - __meta_kubernetes_pod_container_name
          target_label: __path__
```

**DaemonSet resources:**

```yaml
# Add to promtail values
resources:
  requests:
    memory: 64Mi
    cpu: 50m
  limits:
    memory: 128Mi
    cpu: 100m
```

#### Option B: Grafana Alloy

File: `helm-values/alloy.yaml`

```yaml
# grafana/alloy
# Helm: helm install alloy grafana/alloy -n monitoring -f helm-values/alloy.yaml

alloy:
  configMap:
    content: |
      discovery.kubernetes "pods" {
        role = "pod"
      }
      loki.source.kubernetes "pods" {
        targets    = discovery.kubernetes.pods.targets
        forward_to = [loki.write.local.receiver]
      }
      loki.process "extract_labels" {
        forward_to = [loki.write.local.receiver]
        stage.json {
          expressions = {
            level   = "level",
            message = "message",
            service = "service",
            org_id  = "org_id",
            trace_id = "trace_id",
            span_id  = "span_id",
          }
        }
        stage.labels {
          values = {
            level   = "",
            service = "",
            org_id  = "",
          }
        }
      }
      loki.write "local" {
        endpoint {
          url = "http://loki.monitoring.svc.cluster.local:3100/loki/api/v1/push"
        }
      }
resources:
  requests:
    memory: 64Mi
    cpu: 50m
  limits:
    memory: 128Mi
    cpu: 100m
```

**Label extraction:** `namespace`, `pod`, `container` from Kubernetes metadata; `org_id`, `trace_id`, `span_id` from structured JSON logs. Pipeline stages parse JSON and extract these fields as labels for efficient querying.

---

## 4. Tempo (Distributed Tracing)

### 4.1 Overview

| Property | Value |
|----------|-------|
| Helm chart | `grafana/tempo` |
| Namespace | `monitoring` |
| OTLP gRPC | 4317 |
| OTLP HTTP | 4318 |

### 4.2 Helm Values

File: `helm-values/tempo.yaml`

```yaml
# Grafana Tempo — dev configuration
# Helm: helm install tempo grafana/tempo -n monitoring -f helm-values/tempo.yaml

tempo:
  tempo:
    metrics_generator_enabled: true
    replication_factor: 1  # Single-binary mode for dev
    storage:
      trace:
        backend: local  # s3 for prod
        local:
          path: /var/tempo/traces
        # Prod:
        # backend: s3
        # s3:
        #   bucket: tempo-traces
        #   endpoint: minio.doki-data.svc.cluster.local:9000
        #   region: us-east-1
    receivers:
      otlp:
        protocols:
          grpc:
            endpoint: 0.0.0.0:4317
          http:
            endpoint: 0.0.0.0:4318
    resources:
      requests:
        memory: 128Mi
        cpu: 100m
      limits:
        memory: 256Mi
        cpu: 250m
  persistence:
    enabled: true
    size: 10Gi  # 50Gi for prod
```

### 4.3 Prod Override

```yaml
tempo:
  tempo:
    replication_factor: 2
    storage:
      trace:
        backend: s3
        s3:
          bucket: tempo-traces
          endpoint: minio.doki-data.svc.cluster.local:9000
          region: us-east-1
          # access_key, secret_key from existingSecret
  persistence:
    size: 50Gi
  resources:
    requests:
      memory: 256Mi
      cpu: 250m
    limits:
      memory: 512Mi
      cpu: 500m
```

### 4.4 OpenTelemetry Integration

All services must:

1. **Export traces via OTLP** to `tempo.monitoring.svc.cluster.local:4317` (gRPC) or `:4318` (HTTP).
2. **Propagate W3C Trace Context** (`traceparent`, `tracestate` headers).
3. **Include traceID in structured logs** for trace-to-log correlation in Grafana.
4. **Expose exemplars** in Prometheus metrics for trace-to-metric correlation.

Environment variables for OTLP:

```
OTEL_EXPORTER_OTLP_ENDPOINT=http://tempo.monitoring.svc.cluster.local:4317
OTEL_EXPORTER_OTLP_PROTOCOL=grpc
OTEL_SERVICE_NAME=<service-name>
```

---

## 5. Alerting Rules

### 5.1 PrometheusRule CRs

Create `base/prometheus-rules/` and apply via Kustomize or Argo CD.

File: `base/prometheus-rules/alerts.yaml`

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: doki-stack-alerts
  namespace: monitoring
  labels:
    prometheus: kube-prometheus
    role: alert-rules
spec:
  groups:
    - name: doki-stack
      interval: 30s
      rules:
        - alert: PodCrashLooping
          expr: |
            rate(kube_pod_container_status_restarts_total{namespace=~"doki-.*"}[5m]) * 60 * 5 > 3
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "Pod {{ $labels.namespace }}/{{ $labels.pod }} is crash looping"
            description: "Pod has restarted more than 3 times in the last 5 minutes."

        - alert: HighMemoryUsage
          expr: |
            (container_memory_usage_bytes{namespace=~"doki-.*"} / container_spec_memory_limit_bytes{namespace=~"doki-.*"}) > 0.9
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "Container {{ $labels.container }} in {{ $labels.pod }} using >90% memory limit"

        - alert: HighCPUUsage
          expr: |
            rate(container_cpu_usage_seconds_total{namespace=~"doki-.*"}[5m]) > 0.8
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "Container {{ $labels.container }} in {{ $labels.pod }} using >80% CPU for 5m"

        - alert: PostgreSQLDown
          expr: up{job="postgresql"} == 0
          for: 1m
          labels:
            severity: critical
          annotations:
            summary: "PostgreSQL target is down"

        - alert: QdrantDown
          expr: up{job="qdrant"} == 0
          for: 1m
          labels:
            severity: critical
          annotations:
            summary: "Qdrant is down — fail closed (policy context unavailable)"

        - alert: RabbitMQDLQDepth
          expr: rabbitmq_queue_messages{queue=~".*dlq.*"} > 0
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "RabbitMQ DLQ {{ $labels.queue }} has {{ $value }} messages"

        - alert: MinIODiskUsage
          expr: minio_bucket_usage_total_bytes / minio_bucket_usage_quota_bytes > 0.8
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "MinIO disk usage >80%"

        - alert: VaultSealed
          expr: vault_core_unsealed == 0
          for: 1m
          labels:
            severity: critical
          annotations:
            summary: "Vault is sealed"

        - alert: HighErrorRate
          expr: |
            (sum(rate(http_requests_total{status=~"5..", namespace=~"doki-.*"}[5m])) by (service)
            / sum(rate(http_requests_total{namespace=~"doki-.*"}[5m])) by (service)) > 0.01
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "Service {{ $labels.service }} has >1% 5xx error rate"

        - alert: SlowResponses
          expr: |
            histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{namespace="doki-mcp"}[5m])) by (le, service)) > 5
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "MCP endpoint {{ $labels.service }} p99 latency >5s"

        - alert: AgentStuck
          expr: |
            time() - agent_task_start_time_seconds{status="running"} > 1800
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "Agent task {{ $labels.task_id }} running >30m without progress"
```

**Note:** `agent_task_start_time_seconds` (gauge) and `agent_task_duration_seconds` (histogram) must be exposed by the agent orchestrator. Adjust metric names to match actual instrumentation. Alternative: use `langgraph_checkpoint_duration_seconds` or custom metrics from the LangGraph checkpoint store.

### 5.2 Alertmanager Routing

| Environment | Receiver | Use Case |
|-------------|----------|----------|
| Dev | Slack webhook | `#doki-alerts` channel |
| Prod / EE | PagerDuty | On-call escalation |

Configure receivers in `helm-values/prometheus.yaml` as shown in §1.4. Secrets for webhook URLs and PagerDuty keys must be stored in Vault and synced via ESO.

---

## 6. Structured Logging Standard

### 6.1 Required Fields

All services must emit JSON logs with at least:

| Field | Type | Description |
|-------|------|--------------|
| `timestamp` | ISO 8601 string | Log event time |
| `level` | string | `debug`, `info`, `warn`, `error` |
| `message` | string | Human-readable message |
| `service` | string | Service name (e.g., `mcp-policy`, `api-server`) |
| `org_id` | string (optional) | Tenant ID when request is org-scoped |
| `trace_id` | string | W3C trace ID for correlation with Tempo |
| `span_id` | string | W3C span ID |

### 6.2 Log Levels

| Level | Use |
|-------|-----|
| `debug` | Development only; disable in prod or sample |
| `info` | Normal operation, request completion |
| `warn` | Recoverable issues, retries, fallbacks |
| `error` | Failures requiring attention |

### 6.3 Sensitive Field Redaction

- **Vault transit:** Encrypt PII/credentials before logging; never log raw secrets.
- **Regex patterns:** Redact fields matching `password`, `secret`, `token`, `api_key`, `authorization`, `cookie`.
- **Structured logs:** Use explicit field names; avoid logging entire request/response bodies.

Example (pseudo-code):

```json
{
  "timestamp": "2025-03-10T12:00:00Z",
  "level": "info",
  "message": "Request completed",
  "service": "mcp-policy",
  "org_id": "doki-stack",
  "trace_id": "abc123def456",
  "span_id": "789xyz",
  "http_status": 200,
  "duration_ms": 45
}
```

---

## 7. Resource Budget

| Component | Memory (requests) | Memory (limits) | CPU (requests) | CPU (limits) |
|-----------|-------------------|-----------------|----------------|--------------|
| Prometheus | 256Mi | 512Mi | 250m | 500m |
| Grafana | 128Mi | 256Mi | 100m | 250m |
| Loki | 128Mi | 256Mi | 100m | 250m |
| Tempo | 128Mi | 256Mi | 100m | 250m |
| Alertmanager | 64Mi | 128Mi | 100m | 200m |
| Node Exporter | ~32Mi per node | ~64Mi | 50m | 100m |
| Kube State Metrics | 64Mi | 128Mi | 100m | 200m |
| Promtail/Alloy | ~64Mi per node | ~128Mi | 50m | 100m |

**Total (single-node dev):** ~1–1.5 GB RAM, ~1 CPU core.

**Storage:**

| Component | Dev | Prod |
|-----------|-----|------|
| Prometheus | 10Gi | 50Gi |
| Grafana | 5Gi | 5Gi |
| Loki | (filesystem) | S3 (MinIO) |
| Tempo | 10Gi | 50Gi (or S3) |

---

## 8. Implementation Order

Execute in this order to avoid dependency issues:

| Step | Task | Command / Action |
|------|------|-------------------|
| 1 | Install kube-prometheus-stack | `helm install monitoring prometheus-community/kube-prometheus-stack -n monitoring -f helm-values/prometheus.yaml` |
| 2 | Install Loki | `helm install loki grafana/loki -n monitoring -f helm-values/loki.yaml` |
| 3 | Install Promtail or Alloy | `helm install promtail grafana/promtail -n monitoring -f helm-values/promtail.yaml` |
| 4 | Install Tempo | `helm install tempo grafana/tempo -n monitoring -f helm-values/tempo.yaml` |
| 5 | Configure Grafana datasources | Via `additionalDataSources` in prometheus values; verify Loki and Tempo URLs |
| 6 | Import/provision dashboards | Apply ConfigMaps with `grafana_dashboard: "1"`; sidecar loads automatically |
| 7 | Apply alerting rules | `kubectl apply -f base/prometheus-rules/` |
| 8 | Test alert routing | Trigger test alert; verify Slack/PagerDuty delivery |

### Prerequisites

- `monitoring` namespace created
- Cilium network policy allowing Prometheus to scrape targets in `doki-*` namespaces (see `03-security.md`)
- MinIO bucket `loki-chunks` (and `loki-ruler`, `tempo-traces`) for prod
- postgres_exporter, redis/dragonfly_exporter deployed if using data-service dashboards

### Argo CD Application

```yaml
# argocd/applications/monitoring.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: monitoring
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/doki-stack/infrastructure
    path: helm-values
    helm:
      valueFiles:
        - prometheus.yaml
        - loki.yaml
        - promtail.yaml
        - tempo.yaml
  destination:
    server: https://kubernetes.default.svc
    namespace: monitoring
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

---

## Appendix: ServiceMonitor Example

For Doki services that use ServiceMonitor (Prometheus Operator), add:

```yaml
# base/mcp-scanner/servicemonitor.yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: mcp-scanner
  namespace: doki-mcp
  labels:
    release: monitoring
spec:
  selector:
    matchLabels:
      app: mcp-scanner
  endpoints:
    - port: metrics
      path: /metrics
      interval: 30s
```

Ensure the Service has a named port `metrics` pointing to the metrics port (e.g., 9090).
