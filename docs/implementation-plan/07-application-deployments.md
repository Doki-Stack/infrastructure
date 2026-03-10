# Application Deployments — Kustomize Base Manifests

This document covers Kustomize base manifests for all application services in the Doki Stack platform. Each service follows a consistent pattern with standard labels, security context, probes, and resource limits. Argo CD reconciles these manifests from the GitOps repository.

**References:**

- `00-overview.md` — Repository structure, namespace layout, phase mapping
- `02-data-services.md` — Data service endpoints (PostgreSQL, MinIO, Qdrant, Dragonfly, RabbitMQ, Vault)
- `03-security.md` — External Secrets Operator, Kyverno policies, Cilium network policies
- `05-llm-infrastructure.md` — Ollama service endpoint

---

## 1. Base Manifest Pattern

### 1.1 Directory Structure

Each service in `base/{service}/` gets:


| File                 | Purpose                                           |
| -------------------- | ------------------------------------------------- |
| `kustomization.yaml` | Kustomize build config, references all resources  |
| `deployment.yaml`    | Deployment spec (image, env, probes, resources)   |
| `service.yaml`       | ClusterIP Service for in-cluster discovery        |
| `configmap.yaml`     | Optional non-sensitive config                     |
| `hpa.yaml`           | Optional; prod overlay only for scalable services |
| `pdb.yaml`           | Pod Disruption Budget for availability            |


### 1.2 Standard Labels

All resources MUST include these labels for consistency, observability, and policy targeting:

```yaml
labels:
  app.kubernetes.io/name: {service}
  app.kubernetes.io/part-of: doki-stack
  app.kubernetes.io/component: {mcp|agent|platform|ee}
  app.kubernetes.io/managed-by: argocd
  doki.io/org-scope: "true"   # for tenant-scoped services (org_id isolation)
```

**Component values:**

- `mcp` — MCP servers (scanner, execution, policy, memory, registry)
- `agent` — LangGraph agents (orchestrator, automation, review, discovery, rollback)
- `platform` — Platform UI and API server
- `ee` — Enterprise Edition services (license, multi-tenancy, notifications, etc.)

### 1.3 Example kustomization.yaml

```yaml
# base/api-server/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: doki-platform

resources:
  - deployment.yaml
  - service.yaml
  - configmap.yaml
  - pdb.yaml
  # - hpa.yaml  # Added in overlays/prod only

commonLabels:
  app.kubernetes.io/name: api-server
  app.kubernetes.io/part-of: doki-stack
  app.kubernetes.io/component: platform
  app.kubernetes.io/managed-by: argocd
  doki.io/org-scope: "true"

images:
  - name: api-server
    newName: harbor.example.com/doki-stack/api-server
    newTag: main
```

---

## 2. CE Services (Phase 1–2)

### 2.1 Complete Example: api-server

Full YAML for `api-server` as the reference implementation. Other services follow the same pattern with service-specific values.

#### base/api-server/deployment.yaml

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
  labels:
    app.kubernetes.io/name: api-server
    app.kubernetes.io/part-of: doki-stack
    app.kubernetes.io/component: platform
    app.kubernetes.io/managed-by: argocd
    doki.io/org-scope: "true"
spec:
  replicas: 2
  selector:
    matchLabels:
      app.kubernetes.io/name: api-server
  template:
    metadata:
      labels:
        app.kubernetes.io/name: api-server
        app.kubernetes.io/part-of: doki-stack
        app.kubernetes.io/component: platform
    spec:
      terminationGracePeriodSeconds: 30
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        runAsGroup: 1000
        fsGroup: 1000
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: api-server
          image: api-server:main
          imagePullPolicy: IfNotPresent
          ports:
            - name: http
              containerPort: 3000
              protocol: TCP
          envFrom:
            - configMapRef:
                name: api-server-config
            - secretRef:
                name: api-server-secrets
                optional: true
          resources:
            requests:
              memory: 128Mi
              cpu: 100m
            limits:
              memory: 512Mi
              cpu: 500m
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 10
            periodSeconds: 10
            timeoutSeconds: 5
            failureThreshold: 3
          readinessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 5
            periodSeconds: 5
            timeoutSeconds: 3
            failureThreshold: 2
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            runAsNonRoot: true
            capabilities:
              drop:
                - ALL
```

#### base/api-server/service.yaml

```yaml
apiVersion: v1
kind: Service
metadata:
  name: api-server
  labels:
    app.kubernetes.io/name: api-server
    app.kubernetes.io/part-of: doki-stack
    app.kubernetes.io/component: platform
spec:
  type: ClusterIP
  ports:
    - name: http
      port: 3000
      targetPort: http
      protocol: TCP
  selector:
    app.kubernetes.io/name: api-server
```

#### base/api-server/configmap.yaml

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: api-server-config
  labels:
    app.kubernetes.io/name: api-server
    app.kubernetes.io/part-of: doki-stack
data:
  # Non-sensitive config; overlays override for env-specific values
  LOG_LEVEL: "info"
  # Sensitive values come from ExternalSecret → api-server-secrets
```

#### base/api-server/pdb.yaml

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: api-server
  labels:
    app.kubernetes.io/name: api-server
    app.kubernetes.io/part-of: doki-stack
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: api-server
```

#### ExternalSecret (reference)

Sensitive env vars are injected via External Secrets Operator. Example for `api-server`:

```yaml
# base/api-server/externalsecret.yaml (or in policies/external-secrets/)
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: api-server-secrets
  namespace: doki-platform
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: vault-doki-platform
    kind: SecretStore
  target:
    name: api-server-secrets
    creationPolicy: Owner
  data:
    - secretKey: DATABASE_URL
      remoteRef:
        key: secret/data/dev/doki-platform/api-server
        property: database-url
    - secretKey: DRAGONFLY_URL
      remoteRef:
        key: secret/data/dev/doki-platform/api-server
        property: dragonfly-url
    - secretKey: RABBITMQ_URL
      remoteRef:
        key: secret/data/dev/doki-platform/api-server
        property: rabbitmq-url
    - secretKey: AGENT_ORCHESTRATOR_URL
      remoteRef:
        key: secret/data/dev/doki-platform/api-server
        property: agent-orchestrator-url
```

---

### 2.2 platform-ui (Next.js)


| Property  | Value                   |
| --------- | ----------------------- |
| Namespace | `doki-platform`         |
| Port      | 3000                    |
| Replicas  | 2                       |
| Resources | 128Mi/100m → 256Mi/500m |
| Component | `platform`              |


**Environment variables (ConfigMap + ExternalSecret):**
- `NEXT_PUBLIC_API_URL` — Public API base URL (e.g. `https://api.doki.example.com`)
- `AUTH0_DOMAIN`, `AUTH0_CLIENT_ID`, `AUTH0_CLIENT_SECRET`, `AUTH0_AUDIENCE` — Auth0 (from Vault/ESO)
- `NEXTAUTH_URL` — Callback URL (e.g. `https://app.doki.example.com`)
- `NEXTAUTH_SECRET` — Session encryption (from Vault/ESO)

**Probes:** HTTP GET `/api/health`

**Notes:** Next.js runs in standalone mode. Ensure `output: 'standalone'` in `next.config.js` for containerized deployment.

---

### 2.3 api-server (Go)

| Property  | Value                   |
| --------- | ----------------------- |
| Namespace | `doki-platform`         |
| Port      | 3000                    |
| Replicas  | 2                       |
| Resources | 128Mi/100m → 512Mi/500m |
| Component | `platform`              |


**Environment variables (ExternalSecret):**

- `DATABASE_URL` — `postgres://app_service:PASSWORD@postgres-postgresql.doki-data.svc.cluster.local:5432/ai_automation?sslmode=disable`
- `DRAGONFLY_URL` — `redis://dragonfly.doki-data.svc.cluster.local:6379`
- `RABBITMQ_URL` — `amqp://user:pass@rabbitmq.doki-data.svc.cluster.local:5672/%2F`
- `AGENT_ORCHESTRATOR_URL` — `http://agent-orchestrator.doki-agents.svc.cluster.local:8000`

**Probes:** HTTP GET `/healthz`

**Full YAML:** See Section 2.1.

---

### 2.4 mcp-scanner (Rust)


| Property  | Value                    |
| --------- | ------------------------ |
| Namespace | `doki-mcp`               |
| Port      | 3000                     |
| Replicas  | 1                        |
| Resources | 128Mi/100m → 512Mi/1000m |
| Component | `mcp`                    |


**Environment variables (ExternalSecret):**

- `DATABASE_URL`, `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`
- `DRAGONFLY_URL`, `RABBITMQ_URL`, `OLLAMA_URL` — `http://ollama.ai.svc.cluster.local:11434`

**Probes:** HTTP GET `/health`

**Service endpoints:**

- MinIO: `minio.doki-data.svc.cluster.local:9000`
- Dragonfly: `dragonfly.doki-data.svc.cluster.local:6379`
- RabbitMQ: `rabbitmq.doki-data.svc.cluster.local:5672`

---

### 2.5 mcp-execution (Rust)


| Property       | Value                    |
| -------------- | ------------------------ |
| Namespace      | `doki-mcp`               |
| Port           | 3000                     |
| Replicas       | 1                        |
| Resources      | 128Mi/100m → 512Mi/1000m |
| Component      | `mcp`                    |
| ServiceAccount | `mcp-execution`          |


**Environment variables (ExternalSecret):**

- `DATABASE_URL`, `MINIO_ENDPOINT`, `DRAGONFLY_URL`
- `VAULT_ADDR` — `http://vault.doki-data.svc.cluster.local:8200`
- `VAULT_ROLE` — Kubernetes auth role for cloud credential access

**Probes:** HTTP GET `/health`

**ServiceAccount:** Create `mcp-execution` ServiceAccount and bind to Vault Kubernetes auth role for AWS/GCP/Azure credential retrieval. See `03-security.md` for Vault role setup.

---

### 2.6 mcp-policy (Go)


| Property  | Value                   |
| --------- | ----------------------- |
| Namespace | `doki-mcp`              |
| Port      | 3000                    |
| Replicas  | 2                       |
| Resources | 128Mi/100m → 512Mi/500m |
| Component | `mcp`                   |


**Environment variables (ExternalSecret):**

- `DATABASE_URL`, `QDRANT_URL` — `http://qdrant.doki-data.svc.cluster.local:6333`
- `DRAGONFLY_URL`, `OLLAMA_URL`

**Probes:** HTTP GET `/healthz`

**Fail-closed:** Policy MCP must be available. Agents block if policy context is unavailable (ADR-005).

---

### 2.7 agent-orchestrator (Python/FastAPI)


| Property  | Value                  |
| --------- | ---------------------- |
| Namespace | `doki-agents`          |
| Port      | 8000                   |
| Replicas  | 1                      |
| Resources | 256Mi/250m → 1Gi/1000m |
| Component | `agent`                |


**Environment variables (ExternalSecret):**

- `DATABASE_URL`, `RABBITMQ_URL`
- `MCP_SCANNER_URL` — `http://mcp-scanner.doki-mcp.svc.cluster.local:3000`
- `MCP_EXECUTION_URL` — `http://mcp-execution.doki-mcp.svc.cluster.local:3000`
- `MCP_POLICY_URL` — `http://mcp-policy.doki-mcp.svc.cluster.local:3000`
- `OLLAMA_URL` — `http://ollama.ai.svc.cluster.local:11434`

**Probes:** HTTP GET `/health`

---

### 2.8 agent-automation (Python)


| Property  | Value                  |
| --------- | ---------------------- |
| Namespace | `doki-agents`          |
| Port      | 8000                   |
| Replicas  | 1                      |
| Resources | 256Mi/250m → 1Gi/1000m |
| Component | `agent`                |


**Environment variables (ExternalSecret):**

- `DATABASE_URL`, `MINIO_ENDPOINT`
- `MCP_SCANNER_URL`, `MCP_EXECUTION_URL`, `OLLAMA_URL`

**Probes:** HTTP GET `/health`

---

### 2.9 agent-review (Python)


| Property  | Value                   |
| --------- | ----------------------- |
| Namespace | `doki-agents`           |
| Port      | 8000                    |
| Replicas  | 1                       |
| Resources | 128Mi/100m → 512Mi/500m |
| Component | `agent`                 |


**Environment variables (ExternalSecret):**

- `MCP_POLICY_URL`, `OLLAMA_URL`

**Probes:** HTTP GET `/health`

**Notes:** Lightweight agent; no database or MinIO dependency.

---

## 3. EE Services (Phase 3–4)

### 3.1 ee-license-server (Rust) — Phase 3


| Property         | Value                                         |
| ---------------- | --------------------------------------------- |
| Namespace        | `doki-ee`                                     |
| Port             | 3000                                          |
| Replicas         | 2                                             |
| Component        | `ee`                                          |
| **Deploy first** | Must be deployed before all other EE services |


**Environment variables (ExternalSecret):**

- `VAULT_ADDR` — `http://vault.doki-data.svc.cluster.local:8200`
- `DATABASE_URL`

**Probes:** HTTP GET `/health`

**Notes:** License validation gates all EE features. Other EE services call `LICENSE_SERVER_URL` to verify entitlements.

---

### 3.2 mcp-memory (Go) — Phase 3


| Property  | Value      |
| --------- | ---------- |
| Namespace | `doki-mcp` |
| Port      | 3000       |
| Replicas  | 2          |
| Component | `mcp`      |


**Environment variables (ExternalSecret):**

- `DATABASE_URL`, `QDRANT_URL`, `DRAGONFLY_URL`, `OLLAMA_URL`
- `LICENSE_SERVER_URL` — `http://ee-license-server.doki-ee.svc.cluster.local:3000`

**Probes:** HTTP GET `/healthz`

---

### 3.3 agent-discovery (Python) — Phase 3


| Property  | Value         |
| --------- | ------------- |
| Namespace | `doki-agents` |
| Port      | 8000          |
| Replicas  | 1             |
| Component | `agent`       |


**Environment variables (ExternalSecret):**

- `MINIO_ENDPOINT`, `DATABASE_URL`, `VAULT_ADDR`, `DRAGONFLY_URL`

**Probes:** HTTP GET `/health`

**Notes:** Infrastructure Discovery Agent (ADR-013). Uses Vault for cloud provider credentials. Requires `mcp-execution`-style ServiceAccount for cloud API access.

---

### 3.4 agent-rollback (Python) — Phase 3


| Property  | Value         |
| --------- | ------------- |
| Namespace | `doki-agents` |
| Port      | 8000          |
| Replicas  | 1             |
| Component | `agent`       |


**Environment variables (ExternalSecret):**

- `MCP_EXECUTION_URL`, `MINIO_ENDPOINT`, `DATABASE_URL`

**Probes:** HTTP GET `/health`

---

### 3.5 mcp-registry (Go) — Phase 4


| Property  | Value      |
| --------- | ---------- |
| Namespace | `doki-mcp` |
| Port      | 3000       |
| Replicas  | 2          |
| Component | `mcp`      |


**Environment variables (ExternalSecret):**

- `DATABASE_URL`, `VAULT_ADDR`, `LICENSE_SERVER_URL`

**Probes:** HTTP GET `/healthz`

**Notes:** MCP Registry for custom integrations (ADR-012).

---

### 3.6 EE Platform Services (Go) — Phase 4

All in namespace `doki-ee`, port 3000. Brief specs:


| Service              | Key Env Vars                                           | Dependencies        |
| -------------------- | ------------------------------------------------------ | ------------------- |
| **ee-multi-tenancy** | `DATABASE_URL`, `LICENSE_SERVER_URL`, `VAULT_ADDR`     | License server      |
| **ee-notifications** | `DATABASE_URL`, `RABBITMQ_URL`, `LICENSE_SERVER_URL`   | License, RabbitMQ   |
| **ee-compliance**    | `DATABASE_URL`, `QDRANT_URL`, `LICENSE_SERVER_URL`     | License, Qdrant     |
| **ee-governance**    | `DATABASE_URL`, `MCP_POLICY_URL`, `LICENSE_SERVER_URL` | License, Policy MCP |
| **ee-dashboards**    | `DATABASE_URL`, `DRAGONFLY_URL`, `LICENSE_SERVER_URL`  | License, Dragonfly  |


**Common:** All require `LICENSE_SERVER_URL`. Replicas: 2. Resources: 256Mi/250m → 512Mi/500m. Probes: HTTP `/healthz`.

---

## 4. Common Patterns

### 4.1 External Secrets for Sensitive Env Vars

All services use External Secrets Operator (ESO) to sync secrets from Vault into Kubernetes Secrets. Never store credentials in ConfigMaps or plain env vars.

- **SecretStore** per namespace: `vault-doki-platform`, `vault-doki-mcp`, `vault-doki-agents`, `vault-doki-ee`
- **ExternalSecret** per service: references Vault path `secret/data/{env}/{namespace}/{service}`
- **Target Secret** name: `{service}-secrets`
- **Deployment** uses `envFrom.secretRef` with `optional: true` so pods start even if ESO hasn't synced yet (dev); prod overlays may use `optional: false`

### 4.2 SecurityContext (Mandatory)

All containers MUST have:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  runAsGroup: 1000
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop:
      - ALL
```

Pod-level `runAsNonRoot` and `seccompProfile.type: RuntimeDefault` are required. Kyverno enforces these (see `03-security.md`). Exceptions (e.g. Vault) are namespace-scoped.

### 4.3 Liveness and Readiness Probes


| Probe         | Purpose                       | Typical Values                                                        |
| ------------- | ----------------------------- | --------------------------------------------------------------------- |
| **Liveness**  | Restart unhealthy pods        | `initialDelaySeconds: 10`, `periodSeconds: 10`, `failureThreshold: 3` |
| **Readiness** | Remove from Service endpoints | `initialDelaySeconds: 5`, `periodSeconds: 5`, `failureThreshold: 2`   |


**Paths:**

- Go services: `/healthz`
- Rust/FastAPI: `/health`
- Next.js: `/api/health`

### 4.4 Resource Limits (Kyverno Enforced)

Resource limits are mandatory. Kyverno policy rejects pods without `resources.limits`. Use consistent request/limit ratios:


| Tier     | Requests     | Limits       |
| -------- | ------------ | ------------ |
| Light    | 128Mi / 100m | 256Mi / 500m |
| Standard | 256Mi / 250m | 512Mi / 500m |
| Heavy    | 256Mi / 250m | 1Gi / 1000m  |


### 4.5 Graceful Shutdown

```yaml
spec:
  terminationGracePeriodSeconds: 30
```

All deployments include this. Services must handle SIGTERM and drain connections within the grace period.

### 4.6 Pod Disruption Budget (PDB)

Every deployment with `replicas >= 2` gets a PDB with `minAvailable: 1`. Single-replica services may use `maxUnavailable: 0` if desired, or omit PDB.

### 4.7 HPA (Prod Overlay Only)

HPA is added in `overlays/prod/` for scalable services (api-server, mcp-policy, platform-ui, ee-license-server). Base manifests do not include HPA.

---

## 5. Service Dependencies (Startup Order)

```mermaid
flowchart TB
    subgraph Data["Phase 0: Data Services"]
        PG[(PostgreSQL)]
        MinIO[(MinIO)]
        Qdrant[(Qdrant)]
        Dragonfly[(Dragonfly)]
        RabbitMQ[(RabbitMQ)]
        Vault[(Vault)]
    end

    subgraph MCP["Phase 1: MCP Servers"]
        Scanner[mcp-scanner]
        Execution[mcp-execution]
        Policy[mcp-policy]
    end

    subgraph Agents["Phase 2: Agents"]
        Orch[agent-orchestrator]
        Auto[agent-automation]
        Review[agent-review]
    end

    subgraph Platform["Phase 1: Platform"]
        API[api-server]
        UI[platform-ui]
    end

    subgraph EE["Phase 3-4: EE"]
        License[ee-license-server]
        Memory[mcp-memory]
        Registry[mcp-registry]
        Discovery[agent-discovery]
        Rollback[agent-rollback]
    end

    subgraph Gateway["API Gateway"]
        Kong[Kong]
    end

    PG --> Scanner
    PG --> Execution
    PG --> Policy
    PG --> API
    PG --> Orch
    PG --> Auto
    MinIO --> Scanner
    MinIO --> Execution
    MinIO --> Auto
    Qdrant --> Policy
    Dragonfly --> Scanner
    Dragonfly --> Execution
    Dragonfly --> Policy
    Dragonfly --> API
    RabbitMQ --> API
    RabbitMQ --> Orch
    Vault --> Execution

    Scanner --> Orch
    Execution --> Orch
    Policy --> Orch
    Execution --> Auto
    Policy --> Review

    Orch --> API

    API --> Kong
    UI --> Kong

    License --> Memory
    License --> Registry
    License --> Discovery
    License --> Rollback
```



**Sync order (Argo CD):**

1. **Wave 0:** Data services (PostgreSQL, MinIO, Qdrant, Dragonfly, RabbitMQ, Vault)
2. **Wave 1:** MCP servers (scanner, execution, policy)
3. **Wave 2:** Agents (orchestrator, automation, review)
4. **Wave 3:** Platform (api-server, platform-ui)
5. **Wave 4:** Kong (routes to platform + MCP)
6. **Wave 5 (EE):** ee-license-server first, then mcp-memory, agent-discovery, agent-rollback, mcp-registry, ee-* services

---

## 6. Implementation Order

### Phase 1 (CE MCP + Platform)

Create these `base/` directories in order:


| Order | Service       | Directory             |
| ----- | ------------- | --------------------- |
| 1     | platform-ui   | `base/platform-ui/`   |
| 2     | api-server    | `base/api-server/`    |
| 3     | mcp-scanner   | `base/mcp-scanner/`   |
| 4     | mcp-execution | `base/mcp-execution/` |
| 5     | mcp-policy    | `base/mcp-policy/`    |


**Deliverables:** kustomization.yaml, deployment.yaml, service.yaml, configmap.yaml (if needed), pdb.yaml, ExternalSecret references. Ensure SecretStores exist in doki-platform and doki-mcp.

### Phase 2 (Agents)


| Order | Service            | Directory                  |
| ----- | ------------------ | -------------------------- |
| 6     | agent-orchestrator | `base/agent-orchestrator/` |
| 7     | agent-automation   | `base/agent-automation/`   |
| 8     | agent-review       | `base/agent-review/`       |


**Deliverables:** Same pattern. SecretStore in doki-agents. Cilium policies for doki-agents → doki-mcp, doki-agents → ai.

### Phase 3 (EE Phase 3)


| Order | Service           | Directory                    |
| ----- | ----------------- | ---------------------------- |
| 9     | ee-license-server | `base/ee/ee-license-server/` |
| 10    | mcp-memory        | `base/ee/mcp-memory/`        |
| 11    | agent-discovery   | `base/ee/agent-discovery/`   |
| 12    | agent-rollback    | `base/ee/agent-rollback/`    |


**Deliverables:** ee-license-server MUST deploy first. Create doki-ee namespace and SecretStore. License validation gates other EE services.

### Phase 4 (EE Phase 4)


| Order | Service          | Directory                   |
| ----- | ---------------- | --------------------------- |
| 13    | mcp-registry     | `base/ee/mcp-registry/`     |
| 14    | ee-multi-tenancy | `base/ee/ee-multi-tenancy/` |
| 15    | ee-notifications | `base/ee/ee-notifications/` |
| 16    | ee-compliance    | `base/ee/ee-compliance/`    |
| 17    | ee-governance    | `base/ee/ee-governance/`    |
| 18    | ee-dashboards    | `base/ee/ee-dashboards/`    |


**Deliverables:** All EE platform services. HPA overlays for prod.

---

## 7. Overlay Considerations

- **overlays/dev:** Lower replicas (1 for agents), smaller resources, optional ESO (dev secrets in plain Secret for local testing)
- **overlays/staging:** Production-like, 2 replicas for platform/MCP, full ESO
- **overlays/prod:** HPA for api-server, mcp-policy, platform-ui, ee-license-server; higher resource limits; `optional: false` for secretRef

---

## 8. Checklist for New Service

When adding a new service:

1. [ ] Create `base/{service}/` with kustomization.yaml, deployment.yaml, service.yaml, pdb.yaml
2. [ ] Apply standard labels (app.kubernetes.io/*, doki.io/org-scope)
3. [ ] Set SecurityContext (runAsNonRoot, readOnlyRootFilesystem, drop ALL)
4. [ ] Add liveness and readiness probes
5. [ ] Set resource requests and limits
6. [ ] Create ExternalSecret for sensitive env vars
7. [ ] Add Cilium network policy if new namespace or new egress
8. [ ] Update this document with service spec
9. [ ] Add to Argo CD Application sync wave
10. [ ] Verify health check passes in `./health-check.sh`

