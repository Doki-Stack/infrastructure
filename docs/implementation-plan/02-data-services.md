# Data Services — doki-data Namespace

This document covers all stateful data services deployed in the `doki-data` namespace. These services form the foundation of the Doki Stack platform and must be deployed in Phase 0 before any application services.

**References:**
- ADR-006: Raw K8s manifests for RabbitMQ (not Helm)
- ADR-009: OCI Helm for Dragonfly with explicit `--version`
- `db-schemas/docs/implementation-plan/06-non-pg-data-stores.md` — Qdrant, Dragonfly, MinIO, RabbitMQ schemas
- `db-schemas/docs/implementation-plan/02-rls-and-multi-tenancy.md` — PostgreSQL RLS

---

## 1. PostgreSQL

### Overview

| Property | Value |
|----------|-------|
| Helm chart | `bitnami/postgresql` |
| Namespace | `doki-data` |
| Connection | `postgres-postgresql.doki-data.svc.cluster.local:5432` |
| Databases | `ai_automation`, `terraform_states` |
| Schemas | `public`, `langgraph`, `auth` |

### Helm Values — Dev

File: `helm-values/postgresql.yaml`

```yaml
# Bitnami PostgreSQL — dev configuration
# Helm: helm install postgres bitnami/postgresql -n doki-data -f helm-values/postgresql.yaml

auth:
  postgresPassword: ""  # Set via auth.existingSecret in prod; use placeholder for dev
  database: ai_automation
  username: app_admin    # Chart creates app_admin; initdb creates app_service
  password: ""          # Set via auth.existingSecret in prod
  existingSecret: ""    # e.g. postgres-credentials (Vault/ESO in prod)

architecture: standalone

primary:
  persistence:
    enabled: true
    size: 10Gi
    storageClass: ""  # Use cluster default (standard for kind)

  resources:
    requests:
      memory: 256Mi
      cpu: 250m
    limits:
      memory: 1Gi
      cpu: "1000m"

  initdb:
    scripts:
      # Create additional database
      01-create-terraform-db.sql: |
        CREATE DATABASE terraform_states;
      # Create schemas in ai_automation
      02-create-schemas.sql: |
        \c ai_automation
        CREATE SCHEMA IF NOT EXISTS langgraph;
        CREATE SCHEMA IF NOT EXISTS auth;
      # app_admin created by chart via auth.username; grant schema access + CREATEDB
      03-grant-app-admin.sql: |
        \c ai_automation
        ALTER ROLE app_admin CREATEDB;
        GRANT ALL ON SCHEMA public TO app_admin;
        GRANT ALL ON SCHEMA langgraph TO app_admin;
        GRANT ALL ON SCHEMA auth TO app_admin;
      # Create app_service role (runtime, RLS-restricted)
      04-create-app-service.sql: |
        \c ai_automation
        CREATE ROLE app_service LOGIN PASSWORD 'CHANGE_ME_APP_SERVICE';
        GRANT CONNECT ON DATABASE ai_automation TO app_service;
        GRANT USAGE ON SCHEMA public TO app_service;
        GRANT USAGE ON SCHEMA langgraph TO app_service;
        GRANT USAGE ON SCHEMA auth TO app_service;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public
          GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO app_service;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public
          GRANT USAGE ON SEQUENCES TO app_service;
        ALTER DEFAULT PRIVILEGES IN SCHEMA langgraph
          GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO app_service;
        ALTER DEFAULT PRIVILEGES IN SCHEMA langgraph
          GRANT USAGE ON SEQUENCES TO app_service;
```

**Note:** Replace `CHANGE_ME_APP_SERVICE` with actual password. `app_admin` is created by the chart via `auth.username`/`auth.password`. In production, use `auth.existingSecret` and inject credentials via Vault/ESO.

### RLS Setup

Tables are owned by `app_admin`. RLS applies to `app_service`:

- **app_admin** — Full access, used for migrations. Bypasses RLS (table owner).
- **app_service** — Used by all services at runtime. RLS enforces `org_id` isolation.

Every service must `SET LOCAL app.current_org_id = '<uuid>'` within a transaction before queries. See `db-schemas/docs/implementation-plan/02-rls-and-multi-tenancy.md`.

### Backup Strategy

| Environment | Strategy |
|-------------|----------|
| **Dev** | `pg_dump` via CronJob (daily); optional manual backup before destructive changes |
| **Prod** | WAL archival to MinIO (`scanner-artifacts` or dedicated `pg-wal` bucket); point-in-time recovery (PITR) enabled |

---

## 2. MinIO

### Overview

| Property | Value |
|----------|-------|
| Helm chart | `minio/minio` |
| Namespace | `doki-data` |
| API | `minio.doki-data.svc.cluster.local:9000` |
| Console | NodePort `30901` (dev) |

### Helm Values — Dev

File: `helm-values/minio.yaml`

```yaml
# MinIO — dev configuration
# Helm: helm install minio minio/minio -n doki-data -f helm-values/minio.yaml

mode: standalone
replicas: 1

rootUser: ""      # Set via existingSecret in prod
rootPassword: ""  # Set via existingSecret in prod
existingSecret: ""  # minio-credentials in prod

persistence:
  enabled: true
  size: 20Gi
  storageClass: ""

resources:
  requests:
    memory: 256Mi
    cpu: 250m
  limits:
    memory: 1Gi
    cpu: "1000m"

service:
  type: ClusterIP
  port: "9000"

consoleService:
  type: NodePort
  port: "9001"
  nodePort: 30901

buckets:
  - name: scanner-artifacts
    policy: none
    purge: false
    versioning: true
  - name: terraform-states
    policy: none
    purge: false
    versioning: true
  - name: execution-plans
    policy: none
    purge: false
    versioning: true
  - name: prompts
    policy: none
    purge: false
    versioning: true
```

### Prod Override

```yaml
persistence:
  size: 50Gi
```

### Bucket Path Convention

All tenant-scoped objects use prefix `org_id={org_id}/`:

```
scanner-artifacts/org_id={org_id}/{repo_name}/{commit_sha}/skill.md
terraform-states/org_id={org_id}/states/{workspace}/snapshot-{timestamp}.tfstate
execution-plans/org_id={org_id}/plans/{plan_id}/plan.json
prompts/automation/v{version}/system.md  # Platform-level, not org-scoped
```

### Lifecycle Policies

| Bucket | Policy |
|--------|--------|
| All | 90-day expiry for non-current versions (delete old versions) |

Configure via `mc ilm` or MinIO Console after bucket creation.

---

## 3. RabbitMQ

**IMPORTANT:** Use official Docker image `rabbitmq:4-management`, NOT Bitnami (paywalled since Aug 2025). Deploy via raw K8s manifests per ADR-006.

### Overview

| Property | Value |
|----------|-------|
| Image | `rabbitmq:4-management` |
| Namespace | `doki-data` |
| AMQP | `rabbitmq.doki-data.svc.cluster.local:5672` |
| Management | `rabbitmq.doki-data.svc.cluster.local:15672` |
| NodePort AMQP | `30672` |
| NodePort Management | `31672` |

### Manifests

File: `base/rabbitmq/` (raw K8s manifests)

```yaml
# base/rabbitmq/namespace.yaml (or use cluster/namespaces.yaml)
apiVersion: v1
kind: Namespace
metadata:
  name: doki-data
  labels:
    app.kubernetes.io/part-of: doki-stack
---
# base/rabbitmq/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: rabbitmq-config
  namespace: doki-data
data:
  rabbitmq.conf: |
    default_vhost = /
    management.tcp.port = 15672
    management.tcp.ip = 0.0.0.0
    loopback_users = none
---
# base/rabbitmq/secret.yaml (use Vault/ESO in prod)
apiVersion: v1
kind: Secret
metadata:
  name: rabbitmq-credentials
  namespace: doki-data
type: Opaque
stringData:
  default-pass: "CHANGE_ME_RABBITMQ_PASS"
---
# base/rabbitmq/statefulset.yaml
# PVC created via volumeClaimTemplates (data-rabbitmq-0)
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rabbitmq
  namespace: doki-data
spec:
  serviceName: rabbitmq
  replicas: 1
  selector:
    matchLabels:
      app: rabbitmq
  template:
    metadata:
      labels:
        app: rabbitmq
    spec:
      containers:
        - name: rabbitmq
          image: rabbitmq:4-management
          ports:
            - containerPort: 5672
              name: amqp
            - containerPort: 15672
              name: management
          env:
            - name: RABBITMQ_DEFAULT_USER
              value: doki
            - name: RABBITMQ_DEFAULT_PASS
              valueFrom:
                secretKeyRef:
                  name: rabbitmq-credentials
                  key: default-pass
          volumeMounts:
            - name: data
              mountPath: /var/lib/rabbitmq
            - name: config
              mountPath: /etc/rabbitmq/rabbitmq.conf
              subPath: rabbitmq.conf
          resources:
            requests:
              memory: 256Mi
              cpu: 250m
            limits:
              memory: 512Mi
              cpu: 500m
          livenessProbe:
            exec:
              command:
                - rabbitmq-diagnostics
                - check_running
            initialDelaySeconds: 60
            periodSeconds: 30
            timeoutSeconds: 10
          readinessProbe:
            exec:
              command:
                - rabbitmq-diagnostics
                - check_running
            initialDelaySeconds: 20
            periodSeconds: 10
            timeoutSeconds: 5
          startupProbe:
            exec:
              command:
                - rabbitmq-diagnostics
                - check_running
            initialDelaySeconds: 10
            periodSeconds: 10
            timeoutSeconds: 5
            failureThreshold: 30
      volumes:
        - name: config
          configMap:
            name: rabbitmq-config
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes:
          - ReadWriteOnce
        resources:
          requests:
            storage: 5Gi
        storageClassName: ""
---
# base/rabbitmq/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: rabbitmq
  namespace: doki-data
spec:
  type: ClusterIP
  selector:
    app: rabbitmq
  ports:
    - name: amqp
      port: 5672
      targetPort: 5672
    - name: management
      port: 15672
      targetPort: 15672
---
apiVersion: v1
kind: Service
metadata:
  name: rabbitmq-nodeport
  namespace: doki-data
spec:
  type: NodePort
  selector:
    app: rabbitmq
  ports:
    - name: amqp
      port: 5672
      targetPort: 5672
      nodePort: 30672
    - name: management
      port: 15672
      targetPort: 15672
      nodePort: 31672
```

### Exchange/Queue Topology

Defined at application level; documented here for reference:

| Exchange | Type | Durable | Purpose |
|----------|------|---------|---------|
| `agent.events` | topic | yes | Agent state updates for SSE fan-out |
| `scanner.webhooks` | direct | yes | Webhook ingestion for repo scan triggers |

| Queue | Exchange | Binding Key | Consumer | DLQ |
|-------|----------|-------------|----------|-----|
| `agent.events.api` | `agent.events` | `#` | api-server | `agent.events.api.dlq` |
| `scanner.webhooks.scanner` | `scanner.webhooks` | `scan` | mcp-scanner | `scanner.webhooks.scanner.dlq` |
| `agent.events.notifications` | `agent.events` | `#` | ee-notifications (EE) | — |

Routing keys: `{org_id}.{thread_id}` (e.g. `a0000000-0000-0000-0000-000000000001.550e8400-e29b-41d4-a716-446655440000`).

Topology setup script:

```bash
rabbitmqadmin declare exchange name=agent.events type=topic durable=true
rabbitmqadmin declare exchange name=scanner.webhooks type=direct durable=true

rabbitmqadmin declare queue name=agent.events.api durable=true \
  arguments='{"x-dead-letter-exchange": "", "x-dead-letter-routing-key": "agent.events.api.dlq"}'
rabbitmqadmin declare queue name=agent.events.api.dlq durable=true \
  arguments='{"x-message-ttl": 604800000}'

rabbitmqadmin declare queue name=scanner.webhooks.scanner durable=true \
  arguments='{"x-dead-letter-exchange": "", "x-dead-letter-routing-key": "scanner.webhooks.scanner.dlq"}'
rabbitmqadmin declare queue name=scanner.webhooks.scanner.dlq durable=true \
  arguments='{"x-message-ttl": 604800000}'

rabbitmqadmin declare binding source=agent.events destination=agent.events.api routing_key="#"
rabbitmqadmin declare binding source=scanner.webhooks destination=scanner.webhooks.scanner routing_key="scan"
```

---

## 4. Qdrant

### Overview

| Property | Value |
|----------|-------|
| Helm chart | `qdrant/qdrant` |
| Namespace | `doki-data` |
| REST | `qdrant.doki-data.svc.cluster.local:6333` |
| gRPC | `qdrant.doki-data.svc.cluster.local:6334` |

### Helm Values — Dev

File: `helm-values/qdrant.yaml`

```yaml
# Qdrant — dev configuration
# Helm: helm repo add qdrant https://qdrant.github.io/qdrant-helm
#       helm install qdrant qdrant/qdrant -n doki-data -f helm-values/qdrant.yaml

replicaCount: 1

persistence:
  enabled: true
  size: 5Gi
  storageClassName: ""

resources:
  requests:
    memory: 256Mi
    cpu: 250m
  limits:
    memory: 1Gi
    cpu: 500m

service:
  type: ClusterIP
  ports:
    - name: http
      port: 6333
      targetPort: 6333
    - name: grpc
      port: 6334
      targetPort: 6334

livenessProbe:
  enabled: true
  initialDelaySeconds: 10
  periodSeconds: 10
  timeoutSeconds: 5
  failureThreshold: 6

readinessProbe:
  enabled: true
  initialDelaySeconds: 5
  periodSeconds: 5
  timeoutSeconds: 5
  failureThreshold: 6

startupProbe:
  enabled: true
  initialDelaySeconds: 10
  periodSeconds: 5
  timeoutSeconds: 5
  failureThreshold: 30
```

### Collections

Created at application level (Policy MCP, Memory MCP):

| Collection | Dimensions | Distance | Phase |
|------------|------------|----------|-------|
| `policies` | 768 | Cosine | 1 |
| `agent_memories` | 768 | Cosine | 3 (EE) |

---

## 5. Dragonfly (Redis-compatible)

**IMPORTANT:** Use OCI chart `oci://ghcr.io/dragonflydb/dragonfly/helm/dragonfly` with explicit `--version` (ADR-009). Must set `--proactor_threads=2` for dev environments.

### Overview

| Property | Value |
|----------|-------|
| Helm chart | `oci://ghcr.io/dragonflydb/dragonfly/helm/dragonfly` |
| Namespace | `doki-data` |
| Host | `dragonfly.doki-data.svc.cluster.local:6379` |

### Install Command

```bash
helm install dragonfly oci://ghcr.io/dragonflydb/dragonfly/helm/dragonfly \
  --version v1.37.0 \
  -n doki-data \
  -f helm-values/dragonfly.yaml
```

### Helm Values — Dev

File: `helm-values/dragonfly.yaml`

```yaml
# Dragonfly — dev configuration
# MUST use --version flag (OCI chart)
# MUST set --proactor_threads=2 for dev (default 6 threads × 256MB ≈ 1.5GB)

# extraArgs passed to dragonfly binary
extraArgs:
  - "--proactor_threads=2"

resources:
  requests:
    memory: 256Mi
    cpu: 250m
  limits:
    memory: 2Gi
    cpu: "1000m"

# Persistence (if chart supports it)
persistence:
  enabled: true
  size: 5Gi
```

**Note:** Dragonfly OCI chart structure may vary. If `extraArgs` is nested under a different key (e.g. `dragonfly.extraArgs`), adjust accordingly. Consult the chart's `values.yaml` at install time.

### Key Patterns

| Key | TTL | Purpose |
|-----|-----|---------|
| `policy:{org_id}:{query_hash}` | 24h | Policy MCP cache |
| `scan:{org_id}:{repo}:{commit_sha}` | 24h | Scanner MCP cache |
| `api:{org_id}:{user_id}` | 60s | Rate limiting (API server) |

---

## 6. PVC Summary

| Service | PVC Name | Dev Size | Prod Size | Storage Class |
|---------|----------|----------|-----------|----------------|
| PostgreSQL | `data-postgres-postgresql-0` | 10Gi | 50Gi | `standard` (kind default) |
| MinIO | `export-minio-0` | 20Gi | 50Gi | `standard` |
| RabbitMQ | `data-rabbitmq-0` | 5Gi | 10Gi | `standard` |
| Qdrant | `qdrant-storage` | 5Gi | 20Gi | `standard` |
| Dragonfly | `dragonfly-data` (if applicable) | 5Gi | 10Gi | `standard` |

**Total dev:** ~45Gi. **Total prod:** ~140Gi.

---

## 7. Health Checks

| Service | Readiness | Liveness | Startup |
|---------|-----------|-----------|---------|
| **PostgreSQL** | `pg_isready -U postgres` | `pg_isready -U postgres` | `pg_isready` (initialDelay 30s) |
| **MinIO** | HTTP GET `/minio/health/live` | HTTP GET `/minio/health/live` | — |
| **RabbitMQ** | `rabbitmq-diagnostics check_running` | `rabbitmq-diagnostics check_running` | `rabbitmq-diagnostics check_running` (failureThreshold 30) |
| **Qdrant** | HTTP GET `:6333/readyz` | HTTP GET `:6333/healthz` | HTTP GET `:6333/readyz` (failureThreshold 30) |
| **Dragonfly** | `PING` | `PING` | — |

### Probe Configs (Reference)

```yaml
# PostgreSQL (Bitnami chart)
primary:
  readinessProbe:
    enabled: true
    initialDelaySeconds: 5
    periodSeconds: 10
    timeoutSeconds: 5
    failureThreshold: 6
  livenessProbe:
    enabled: true
    initialDelaySeconds: 30
    periodSeconds: 10
    timeoutSeconds: 5
    failureThreshold: 6
  startupProbe:
    enabled: true
    initialDelaySeconds: 30
    periodSeconds: 10
    timeoutSeconds: 5
    failureThreshold: 60

# MinIO (chart default)
# liveness/readiness: HTTP :9000/minio/health/live

# RabbitMQ (see StatefulSet above)
# readiness: rabbitmq-diagnostics check_running
# liveness: rabbitmq-diagnostics check_running
# startup: rabbitmq-diagnostics check_running, failureThreshold 30

# Qdrant (see helm-values above)
# readiness: HTTP :6333/readyz
# liveness: HTTP :6333/healthz
# startup: HTTP :6333/readyz, failureThreshold 30

# Dragonfly
# readiness: TCP 6379 or PING
# liveness: TCP 6379 or PING
```

---

## 8. Implementation Order

1. **Create doki-data namespace**
   ```bash
   kubectl create namespace doki-data
   ```

2. **Install PostgreSQL** (+ init schemas/roles)
   ```bash
   helm repo add bitnami https://charts.bitnami.com/bitnami
   helm install postgres bitnami/postgresql -n doki-data -f helm-values/postgresql.yaml
   # Wait for rollout; run migrations if needed
   ```

3. **Install MinIO** (+ create buckets)
   ```bash
   helm repo add minio https://helm.min.io/
   helm install minio minio/minio -n doki-data -f helm-values/minio.yaml
   # Buckets created via chart values; verify lifecycle policies if needed
   ```

4. **Deploy RabbitMQ** (StatefulSet)
   ```bash
   kubectl apply -f base/rabbitmq/ -n doki-data
   # Run topology setup script after RabbitMQ is ready
   ```

5. **Install Qdrant**
   ```bash
   helm repo add qdrant https://qdrant.github.io/qdrant-helm
   helm install qdrant qdrant/qdrant -n doki-data -f helm-values/qdrant.yaml
   ```

6. **Install Dragonfly**
   ```bash
   helm install dragonfly oci://ghcr.io/dragonflydb/dragonfly/helm/dragonfly \
     --version v1.37.0 -n doki-data -f helm-values/dragonfly.yaml
   ```

7. **Verify all services healthy**
   ```bash
   ./scripts/health-check.sh
   # Or: kubectl get pods -n doki-data
   ```

---

## Appendix: Secrets Management

**Dev:** Placeholder passwords in values/manifests are acceptable for local kind clusters. Rotate before any shared or staging use.

**Prod:** All credentials must come from Vault via External Secrets Operator (ESO). Create `ExternalSecret` resources that sync to `Secret` in `doki-data`. Never commit real passwords.

```yaml
# Example ExternalSecret (prod)
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: postgres-credentials
  namespace: doki-data
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: vault-backend
    kind: ClusterSecretStore
  target:
    name: postgres-credentials
  data:
    - secretKey: postgres-password
      remoteRef:
        key: secret/data/orgs/doki-stack/postgres
        property: postgres-password
```
