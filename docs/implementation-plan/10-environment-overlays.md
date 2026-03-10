# Environment Overlays — Kustomize Strategy

This document covers Kustomize environment overlays for the Doki Stack platform. Base manifests in `base/` define the canonical service structure; overlays in `overlays/{env}/` customize per environment (dev, staging, prod). Argo CD reconciles each environment from its overlay path.

**References:**
- `00-overview.md` — Repository structure, phase mapping
- `07-application-deployments.md` — Base manifests, service specs, resource tiers
- `02-data-services.md` — PostgreSQL, MinIO, PVC sizes, Helm values
- `03-security.md` — External Secrets, Vault paths

---

## 1. Overlay Strategy

### 1.1 Principles

- **Base manifests** in `base/` define the service structure (deployment, service, configmap, pdb). Base is environment-agnostic and uses sensible defaults.
- **Overlays** in `overlays/{env}/` customize per environment via patches, resource overrides, and environment-specific resources.
- **Three environments:** dev, staging, prod. Each overlay has its own `kustomization.yaml` that references the base and applies patches.
- **No duplication:** Patches modify base resources; overlays do not redefine full manifests except for environment-only resources (e.g. HPA in prod, network policies).

### 1.2 Environment Summary

| Aspect | Dev | Staging | Prod |
|--------|-----|---------|------|
| Replicas | 1 (api-server: 2) | 2 | 2–3 + HPA |
| Resources | Minimal | Moderate | Full |
| Image pull | IfNotPresent | Always | Always |
| Log level | debug | info | warn |
| Vault | dev mode | prod HA | prod HA + auto-unseal |
| TLS | Self-signed | Let's Encrypt staging | Let's Encrypt prod |
| Kong auth | Optional bypass | Full | Full + rate limit |
| PostgreSQL PVC | 10Gi | 20Gi | 50Gi + HA |
| MinIO PVC | 20Gi | 50Gi | 100Gi + replication |
| Monitoring | Basic | Full + Slack | Full + PagerDuty |
| Network policies | Relaxed | Moderate | Strictest |

---

## 2. Directory Structure

```
overlays/
├── dev/
│   ├── kustomization.yaml
│   ├── patches/
│   │   ├── replicas.yaml      # 1 replica for most services
│   │   ├── resources.yaml     # Lower resource limits
│   │   └── env-config.yaml   # Dev-specific env vars
│   └── namespace-patches/
├── staging/
│   ├── kustomization.yaml
│   ├── patches/
│   │   ├── replicas.yaml      # 2 replicas
│   │   ├── resources.yaml     # Medium resources
│   │   └── env-config.yaml
│   └── secrets/               # ExternalSecret overrides
└── prod/
    ├── kustomization.yaml
    ├── patches/
    │   ├── replicas.yaml      # 2-3 replicas
    │   ├── resources.yaml     # Full resources
    │   ├── hpa.yaml           # HPA enabled
    │   └── env-config.yaml
    ├── secrets/
    └── network-policies/      # Stricter network policies
```

---

## 3. Dev Overlay

### 3.1 Dev kustomization.yaml

```yaml
# overlays/dev/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

# Omit namespace to preserve base namespaces (doki-platform, doki-mcp, doki-agents).
# For single-namespace dev, set namespace: doki-platform and ensure bases use it.

resources:
  - ../../base/platform-ui
  - ../../base/api-server
  - ../../base/mcp-scanner
  - ../../base/mcp-execution
  - ../../base/mcp-policy
  - ../../base/agent-orchestrator
  - ../../base/agent-automation
  - ../../base/agent-review

# Cross-namespace: include namespaced bases or use components
# For multi-namespace, use multiple kustomizations or Argo CD apps per namespace
# This example assumes a single kustomization; adjust for actual base structure

commonLabels:
  doki.io/environment: dev
  doki.io/managed-by: argocd

commonAnnotations:
  argocd.argoproj.io/sync-wave: "1"

patches:
  - path: patches/replicas.yaml
    target:
      kind: Deployment
  - path: patches/resources.yaml
    target:
      kind: Deployment
  - path: patches/env-config.yaml
    target:
      kind: ConfigMap

# Image transformer for dev registry (optional)
images:
  - name: api-server
    newName: harbor.example.com/doki-stack/api-server
    newTag: dev
  - name: platform-ui
    newName: harbor.example.com/doki-stack/platform-ui
    newTag: dev
  - name: mcp-scanner
    newName: harbor.example.com/doki-stack/mcp-scanner
    newTag: dev
  - name: mcp-execution
    newName: harbor.example.com/doki-stack/mcp-execution
    newTag: dev
  - name: mcp-policy
    newName: harbor.example.com/doki-stack/mcp-policy
    newTag: dev
  - name: agent-orchestrator
    newName: harbor.example.com/doki-stack/agent-orchestrator
    newTag: dev
  - name: agent-automation
    newName: harbor.example.com/doki-stack/agent-automation
    newTag: dev
  - name: agent-review
    newName: harbor.example.com/doki-stack/agent-review
    newTag: dev

namePrefix: ""   # No prefix for dev; staging/prod may use env prefix
nameSuffix: ""  # Or use -dev suffix if desired
```

**Note:** The base structure may use separate kustomizations per namespace. Adjust `resources` to match. For a flat base with all services, the above applies. If bases are per-namespace (e.g. `base/platform/`, `base/mcp/`, `base/agents/`), reference those instead.

### 3.2 Dev patches/replicas.yaml

```yaml
# overlays/dev/patches/replicas.yaml
# Strategic merge patch — applies to all Deployments
# Use patchStrategicMerge with multiple patches if per-service values differ
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: platform-ui
spec:
  replicas: 1
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
spec:
  replicas: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-scanner
spec:
  replicas: 1
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-execution
spec:
  replicas: 1
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-policy
spec:
  replicas: 1
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-orchestrator
spec:
  replicas: 1
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-automation
spec:
  replicas: 1
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-review
spec:
  replicas: 1
```

### 3.3 Dev patches/resources.yaml

```yaml
# overlays/dev/patches/resources.yaml
# Minimal resources for dev
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: platform-ui
spec:
  template:
    spec:
      containers:
        - name: platform-ui
          resources:
            requests:
              memory: 64Mi
              cpu: 50m
            limits:
              memory: 128Mi
              cpu: 200m
          imagePullPolicy: IfNotPresent
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
spec:
  template:
    spec:
      containers:
        - name: api-server
          resources:
            requests:
              memory: 128Mi
              cpu: 100m
            limits:
              memory: 256Mi
              cpu: 500m
          imagePullPolicy: IfNotPresent
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-scanner
spec:
  template:
    spec:
      containers:
        - name: mcp-scanner
          resources:
            requests:
              memory: 128Mi
              cpu: 100m
            limits:
              memory: 256Mi
              cpu: 500m
          imagePullPolicy: IfNotPresent
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-execution
spec:
  template:
    spec:
      containers:
        - name: mcp-execution
          resources:
            requests:
              memory: 128Mi
              cpu: 100m
            limits:
              memory: 256Mi
              cpu: 500m
          imagePullPolicy: IfNotPresent
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-policy
spec:
  template:
    spec:
      containers:
        - name: mcp-policy
          resources:
            requests:
              memory: 128Mi
              cpu: 100m
            limits:
              memory: 256Mi
              cpu: 500m
          imagePullPolicy: IfNotPresent
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-orchestrator
spec:
  template:
    spec:
      containers:
        - name: agent-orchestrator
          resources:
            requests:
              memory: 128Mi
              cpu: 100m
            limits:
              memory: 256Mi
              cpu: 500m
          imagePullPolicy: IfNotPresent
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-automation
spec:
  template:
    spec:
      containers:
        - name: agent-automation
          resources:
            requests:
              memory: 128Mi
              cpu: 100m
            limits:
              memory: 256Mi
              cpu: 500m
          imagePullPolicy: IfNotPresent
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-review
spec:
  template:
    spec:
      containers:
        - name: agent-review
          resources:
            requests:
              memory: 64Mi
              cpu: 50m
            limits:
              memory: 128Mi
              cpu: 200m
          imagePullPolicy: IfNotPresent
```

### 3.4 Dev patches/env-config.yaml

```yaml
# overlays/dev/patches/env-config.yaml
# Strategic merge patch for ConfigMaps
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: platform-ui-config
data:
  LOG_LEVEL: "debug"
  ENVIRONMENT: "dev"
  NEXT_PUBLIC_API_URL: "http://api.doki-dev.example.com"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: api-server-config
data:
  LOG_LEVEL: "debug"
  ENVIRONMENT: "dev"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: mcp-scanner-config
data:
  LOG_LEVEL: "debug"
  ENVIRONMENT: "dev"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: mcp-execution-config
data:
  LOG_LEVEL: "debug"
  ENVIRONMENT: "dev"
  VAULT_DEV_ROOT_TOKEN_ID: "devRootToken"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: mcp-policy-config
data:
  LOG_LEVEL: "debug"
  ENVIRONMENT: "dev"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: agent-orchestrator-config
data:
  LOG_LEVEL: "debug"
  ENVIRONMENT: "dev"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: agent-automation-config
data:
  LOG_LEVEL: "debug"
  ENVIRONMENT: "dev"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: agent-review-config
data:
  LOG_LEVEL: "debug"
  ENVIRONMENT: "dev"
```

### 3.5 Dev Data Service Overrides (Helm Values)

Data services use Helm; overlays apply via `helm-values/{service}-dev.yaml` or Argo CD Application `values` override. Summary:

| Service | Dev Setting |
|---------|-------------|
| **PostgreSQL** | 10Gi PVC, standalone |
| **MinIO** | 20Gi PVC, standalone |
| **Vault** | dev mode, `devRootToken`, single node |
| **TLS** | Self-signed (cert-manager self-signed issuer) |
| **Kong** | `KONG_PLUGINS=request-transformer` only; auth plugin disabled for internal testing |
| **Monitoring** | Prometheus + Grafana; no Alertmanager or Slack |

---

## 4. Staging Overlay

### 4.1 Staging kustomization.yaml

```yaml
# overlays/staging/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - ../../base/platform-ui
  - ../../base/api-server
  - ../../base/mcp-scanner
  - ../../base/mcp-execution
  - ../../base/mcp-policy
  - ../../base/agent-orchestrator
  - ../../base/agent-automation
  - ../../base/agent-review
  # Add EE bases when Phase 3 is active

commonLabels:
  doki.io/environment: staging
  doki.io/managed-by: argocd

commonAnnotations:
  argocd.argoproj.io/sync-wave: "1"

patches:
  - path: patches/replicas.yaml
    target:
      kind: Deployment
  - path: patches/resources.yaml
    target:
      kind: Deployment
  - path: patches/env-config.yaml
    target:
      kind: ConfigMap

# Staging-specific ExternalSecret overrides
patchesStrategicMerge:
  - secrets/externalsecret-staging-paths.yaml

images:
  - name: api-server
    newName: harbor.example.com/doki-stack/api-server
    newTag: staging
  - name: platform-ui
    newName: harbor.example.com/doki-stack/platform-ui
    newTag: staging
  # ... (same pattern for all services)
```

### 4.2 Staging patches/replicas.yaml

```yaml
# overlays/staging/patches/replicas.yaml
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: platform-ui
spec:
  replicas: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
spec:
  replicas: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-scanner
spec:
  replicas: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-execution
spec:
  replicas: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-policy
spec:
  replicas: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-orchestrator
spec:
  replicas: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-automation
spec:
  replicas: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-review
spec:
  replicas: 2
```

### 4.3 Staging patches/resources.yaml

```yaml
# overlays/staging/patches/resources.yaml
# Moderate resources
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: platform-ui
spec:
  template:
    spec:
      containers:
        - name: platform-ui
          resources:
            requests:
              memory: 128Mi
              cpu: 100m
            limits:
              memory: 256Mi
              cpu: 500m
          imagePullPolicy: Always
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
spec:
  template:
    spec:
      containers:
        - name: api-server
          resources:
            requests:
              memory: 256Mi
              cpu: 250m
            limits:
              memory: 512Mi
              cpu: 500m
          imagePullPolicy: Always
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-scanner
spec:
  template:
    spec:
      containers:
        - name: mcp-scanner
          resources:
            requests:
              memory: 256Mi
              cpu: 250m
            limits:
              memory: 512Mi
              cpu: 1000m
          imagePullPolicy: Always
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-execution
spec:
  template:
    spec:
      containers:
        - name: mcp-execution
          resources:
            requests:
              memory: 256Mi
              cpu: 250m
            limits:
              memory: 512Mi
              cpu: 1000m
          imagePullPolicy: Always
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-policy
spec:
  template:
    spec:
      containers:
        - name: mcp-policy
          resources:
            requests:
              memory: 256Mi
              cpu: 250m
            limits:
              memory: 512Mi
              cpu: 500m
          imagePullPolicy: Always
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-orchestrator
spec:
  template:
    spec:
      containers:
        - name: agent-orchestrator
          resources:
            requests:
              memory: 256Mi
              cpu: 250m
            limits:
              memory: 512Mi
              cpu: 1000m
          imagePullPolicy: Always
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-automation
spec:
  template:
    spec:
      containers:
        - name: agent-automation
          resources:
            requests:
              memory: 256Mi
              cpu: 250m
            limits:
              memory: 512Mi
              cpu: 1000m
          imagePullPolicy: Always
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-review
spec:
  template:
    spec:
      containers:
        - name: agent-review
          resources:
            requests:
              memory: 128Mi
              cpu: 100m
            limits:
              memory: 256Mi
              cpu: 500m
          imagePullPolicy: Always
```

### 4.4 Staging patches/env-config.yaml

```yaml
# overlays/staging/patches/env-config.yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: platform-ui-config
data:
  LOG_LEVEL: "info"
  ENVIRONMENT: "staging"
  NEXT_PUBLIC_API_URL: "https://api.staging.doki.example.com"
  FEATURE_FLAGS: "sse,hitl,audit"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: api-server-config
data:
  LOG_LEVEL: "info"
  ENVIRONMENT: "staging"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: mcp-scanner-config
data:
  LOG_LEVEL: "info"
  ENVIRONMENT: "staging"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: mcp-execution-config
data:
  LOG_LEVEL: "info"
  ENVIRONMENT: "staging"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: mcp-policy-config
data:
  LOG_LEVEL: "info"
  ENVIRONMENT: "staging"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: agent-orchestrator-config
data:
  LOG_LEVEL: "info"
  ENVIRONMENT: "staging"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: agent-automation-config
data:
  LOG_LEVEL: "info"
  ENVIRONMENT: "staging"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: agent-review-config
data:
  LOG_LEVEL: "info"
  ENVIRONMENT: "staging"
```

### 4.5 Staging Data Service Overrides

| Service | Staging Setting |
|---------|-----------------|
| **PostgreSQL** | 20Gi PVC, standalone |
| **MinIO** | 50Gi PVC, standalone |
| **Vault** | prod mode, HA (3 nodes) |
| **TLS** | Let's Encrypt staging (staging issuer) |
| **Kong** | Full auth enabled (key-auth, OAuth2) |
| **Monitoring** | Full Prometheus, Grafana, Loki, Tempo; Alertmanager → Slack |

---

## 5. Prod Overlay

### 5.1 Prod kustomization.yaml

```yaml
# overlays/prod/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - ../../base/platform-ui
  - ../../base/api-server
  - ../../base/mcp-scanner
  - ../../base/mcp-execution
  - ../../base/mcp-policy
  - ../../base/agent-orchestrator
  - ../../base/agent-automation
  - ../../base/agent-review
  # EE bases when Phase 4 is active

# Prod adds HPA (new resource) and network policies
resources:
  - patches/hpa.yaml

patches:
  - path: patches/replicas.yaml
    target:
      kind: Deployment
  - path: patches/resources.yaml
    target:
      kind: Deployment
  - path: patches/env-config.yaml
    target:
      kind: ConfigMap

patchesStrategicMerge:
  - patches/affinity.yaml
  - secrets/externalsecret-prod-paths.yaml

# Optional: add network policies as additional resources
# resources:
#   - network-policies/default-deny.yaml
#   - network-policies/allow-platform-egress.yaml

commonLabels:
  doki.io/environment: prod
  doki.io/managed-by: argocd

commonAnnotations:
  argocd.argoproj.io/sync-wave: "1"

images:
  - name: api-server
    newName: harbor.example.com/doki-stack/api-server
    newTag: main
  - name: platform-ui
    newName: harbor.example.com/doki-stack/platform-ui
    newTag: main
  # ... (all services, newTag: main or release version)
```

### 5.2 Prod patches/replicas.yaml

```yaml
# overlays/prod/patches/replicas.yaml
# 2-3 replicas; HPA will scale api-server, mcp-policy, platform-ui
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: platform-ui
spec:
  replicas: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
spec:
  replicas: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-scanner
spec:
  replicas: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-execution
spec:
  replicas: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-policy
spec:
  replicas: 3
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-orchestrator
spec:
  replicas: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-automation
spec:
  replicas: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-review
spec:
  replicas: 2
```

### 5.3 Prod patches/resources.yaml

```yaml
# overlays/prod/patches/resources.yaml
# Full resources
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: platform-ui
spec:
  template:
    spec:
      containers:
        - name: platform-ui
          resources:
            requests:
              memory: 256Mi
              cpu: 250m
            limits:
              memory: 512Mi
              cpu: 1000m
          imagePullPolicy: Always
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
spec:
  template:
    spec:
      containers:
        - name: api-server
          resources:
            requests:
              memory: 512Mi
              cpu: 500m
            limits:
              memory: 1Gi
              cpu: 1000m
          imagePullPolicy: Always
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-scanner
spec:
  template:
    spec:
      containers:
        - name: mcp-scanner
          resources:
            requests:
              memory: 512Mi
              cpu: 500m
            limits:
              memory: 1Gi
              cpu: 1000m
          imagePullPolicy: Always
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-execution
spec:
  template:
    spec:
      containers:
        - name: mcp-execution
          resources:
            requests:
              memory: 512Mi
              cpu: 500m
            limits:
              memory: 1Gi
              cpu: 1000m
          imagePullPolicy: Always
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-policy
spec:
  template:
    spec:
      containers:
        - name: mcp-policy
          resources:
            requests:
              memory: 512Mi
              cpu: 500m
            limits:
              memory: 1Gi
              cpu: 1000m
          imagePullPolicy: Always
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-orchestrator
spec:
  template:
    spec:
      containers:
        - name: agent-orchestrator
          resources:
            requests:
              memory: 512Mi
              cpu: 500m
            limits:
              memory: 1Gi
              cpu: 1000m
          imagePullPolicy: Always
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-automation
spec:
  template:
    spec:
      containers:
        - name: agent-automation
          resources:
            requests:
              memory: 512Mi
              cpu: 500m
            limits:
              memory: 1Gi
              cpu: 1000m
          imagePullPolicy: Always
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agent-review
spec:
  template:
    spec:
      containers:
        - name: agent-review
          resources:
            requests:
              memory: 256Mi
              cpu: 250m
            limits:
              memory: 512Mi
              cpu: 500m
          imagePullPolicy: Always
```

### 5.4 Prod patches/env-config.yaml

```yaml
# overlays/prod/patches/env-config.yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: platform-ui-config
data:
  LOG_LEVEL: "warn"
  ENVIRONMENT: "prod"
  NEXT_PUBLIC_API_URL: "https://api.doki.example.com"
  FEATURE_FLAGS: "sse,hitl,audit,governance"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: api-server-config
data:
  LOG_LEVEL: "warn"
  ENVIRONMENT: "prod"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: mcp-scanner-config
data:
  LOG_LEVEL: "warn"
  ENVIRONMENT: "prod"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: mcp-execution-config
data:
  LOG_LEVEL: "warn"
  ENVIRONMENT: "prod"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: mcp-policy-config
data:
  LOG_LEVEL: "warn"
  ENVIRONMENT: "prod"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: agent-orchestrator-config
data:
  LOG_LEVEL: "warn"
  ENVIRONMENT: "prod"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: agent-automation-config
data:
  LOG_LEVEL: "warn"
  ENVIRONMENT: "prod"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: agent-review-config
data:
  LOG_LEVEL: "warn"
  ENVIRONMENT: "prod"
```

### 5.5 Prod patches/hpa.yaml

```yaml
# overlays/prod/patches/hpa.yaml
# HPA for scalable stateless services
# Base does not include HPA; prod overlay adds it
# Include as additional resources, not patch
# This file documents the HPA spec; add to kustomization resources
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: api-server
  namespace: doki-platform
  labels:
    app.kubernetes.io/name: api-server
    doki.io/environment: prod
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: api-server
  minReplicas: 2
  maxReplicas: 6
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: platform-ui
  namespace: doki-platform
  labels:
    app.kubernetes.io/name: platform-ui
    doki.io/environment: prod
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: platform-ui
  minReplicas: 2
  maxReplicas: 4
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: mcp-policy
  namespace: doki-mcp
  labels:
    app.kubernetes.io/name: mcp-policy
    doki.io/environment: prod
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: mcp-policy
  minReplicas: 3
  maxReplicas: 8
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
```

### 5.6 Prod patches/affinity.yaml

```yaml
# overlays/prod/patches/affinity.yaml
# Pod anti-affinity: spread across nodes
# Apply to each Deployment via strategic merge
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
spec:
  template:
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchLabels:
                    app.kubernetes.io/name: api-server
                topologyKey: kubernetes.io/hostname
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: platform-ui
spec:
  template:
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchLabels:
                    app.kubernetes.io/name: platform-ui
                topologyKey: kubernetes.io/hostname
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-policy
spec:
  template:
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchLabels:
                    app.kubernetes.io/name: mcp-policy
                topologyKey: kubernetes.io/hostname
# Repeat for other multi-replica services
```

### 5.7 Prod Data Service Overrides

| Service | Prod Setting |
|---------|--------------|
| **PostgreSQL** | 50Gi PVC, HA (CloudNativePG or Bitnami HA) |
| **MinIO** | 100Gi PVC, replication (distributed mode) |
| **Vault** | prod mode, HA, auto-unseal (KMS) |
| **TLS** | Let's Encrypt production certificates |
| **Kong** | Full auth, strict rate limiting, IP allowlists |
| **Monitoring** | Full stack; Alertmanager → PagerDuty |
| **Network policies** | Strictest; default deny, explicit allow |

---

## 6. Resource Limits by Environment

| Service | Dev (req/lim) | Staging (req/lim) | Prod (req/lim) |
|---------|---------------|-------------------|----------------|
| platform-ui | 64Mi/128Mi | 128Mi/256Mi | 256Mi/512Mi |
| api-server | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |
| mcp-scanner | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |
| mcp-execution | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |
| mcp-policy | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |
| agent-orchestrator | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |
| agent-automation | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |
| agent-review | 64Mi/128Mi | 128Mi/256Mi | 256Mi/512Mi |
| ee-license-server | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |
| mcp-memory | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |
| agent-discovery | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |
| agent-rollback | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |
| mcp-registry | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |
| ee-multi-tenancy | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |
| ee-notifications | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |
| ee-compliance | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |
| ee-governance | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |
| ee-dashboards | 128Mi/256Mi | 256Mi/512Mi | 512Mi/1Gi |

**CPU (requests/limits):** Dev 50–100m/200–500m, Staging 100–250m/500–1000m, Prod 250–500m/500–1000m.

---

## 7. ConfigMap Overlays

### 7.1 Environment-Specific Keys

All services receive these via ConfigMap merge:

| Key | Dev | Staging | Prod |
|-----|-----|---------|------|
| LOG_LEVEL | debug | info | warn |
| ENVIRONMENT | dev | staging | prod |
| FEATURE_FLAGS | (empty or minimal) | sse,hitl,audit | sse,hitl,audit,governance |

### 7.2 Strategic Merge Patch Example

Base ConfigMap (`base/api-server/configmap.yaml`):

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: api-server-config
data:
  LOG_LEVEL: "info"
  ENVIRONMENT: "dev"
```

Overlay patch (`overlays/prod/patches/env-config.yaml`):

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: api-server-config
data:
  LOG_LEVEL: "warn"
  ENVIRONMENT: "prod"
  FEATURE_FLAGS: "sse,hitl,audit,governance"
```

Kustomize merges `data`; overlay values override base. Result:

```yaml
data:
  LOG_LEVEL: "warn"
  ENVIRONMENT: "prod"
  FEATURE_FLAGS: "sse,hitl,audit,governance"
```

### 7.3 JSON Patch Alternative (Not Recommended)

For single-key changes, JSON patch works but is less readable:

```yaml
# patches/json-patch-env.yaml
- op: replace
  path: /data/LOG_LEVEL
  value: "warn"
```

Prefer strategic merge for ConfigMap overlays.

---

## 8. Secrets Overlay

### 8.1 Vault Path Convention

ExternalSecret `remoteRef.key` varies by environment:

| Environment | Vault Path Pattern |
|-------------|---------------------|
| Dev | `secret/data/dev/{namespace}/{service}` |
| Staging | `secret/data/staging/{namespace}/{service}` |
| Prod | `secret/data/prod/{namespace}/{service}` |

Example namespace values: `doki-platform`, `doki-mcp`, `doki-agents`, `doki-ee`.

### 8.2 ExternalSecret Override Patch

Base ExternalSecret (in base or policies):

```yaml
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
```

Staging overlay patch (`overlays/staging/secrets/externalsecret-staging-paths.yaml`):

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: api-server-secrets
  namespace: doki-platform
spec:
  data:
    - secretKey: DATABASE_URL
      remoteRef:
        key: secret/data/staging/doki-platform/api-server
        property: database-url
    - secretKey: DRAGONFLY_URL
      remoteRef:
        key: secret/data/staging/doki-platform/api-server
        property: dragonfly-url
    - secretKey: RABBITMQ_URL
      remoteRef:
        key: secret/data/staging/doki-platform/api-server
        property: rabbitmq-url
    - secretKey: AGENT_ORCHESTRATOR_URL
      remoteRef:
        key: secret/data/staging/doki-platform/api-server
        property: agent-orchestrator-url
```

Prod overlay patch (`overlays/prod/secrets/externalsecret-prod-paths.yaml`):

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: api-server-secrets
  namespace: doki-platform
spec:
  refreshInterval: 15m
  data:
    - secretKey: DATABASE_URL
      remoteRef:
        key: secret/data/prod/doki-platform/api-server
        property: database-url
    # ... (same structure, prod path)
```

### 8.3 Deployment secretRef Optional Flag

| Environment | `envFrom.secretRef.optional` |
|-------------|------------------------------|
| Dev | true (pods can start before ESO sync) |
| Staging | false |
| Prod | false |

Prod overlay patch for Deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
spec:
  template:
    spec:
      containers:
        - name: api-server
          envFrom:
            - configMapRef:
                name: api-server-config
            - secretRef:
                name: api-server-secrets
                optional: false
```

---

## 9. Kustomize Best Practices

### 9.1 Strategic Merge vs JSON Patch

- **Prefer strategic merge patches** for ConfigMap, Deployment, Service. Merge semantics are intuitive (maps merge, lists replace by name).
- **Use JSON patch** only when strategic merge cannot express the change (e.g. add array element at index, rename key).

### 9.2 Name Suffix/Prefix

- **nameSuffix: "-dev"** — Resources become `api-server-dev`, `platform-ui-dev`. Useful if multiple overlays deploy to same cluster.
- **namePrefix: "staging-"** — Resources become `staging-api-server`. Alternative to suffix.
- **Default:** No prefix/suffix when each environment has its own cluster/namespace.

### 9.3 Common Labels

Every overlay adds:

```yaml
commonLabels:
  doki.io/environment: dev   # or staging, prod
  doki.io/managed-by: argocd
```

Enables:
- `kubectl get all -l doki.io/environment=prod`
- Prometheus metrics filtering
- Network policy targeting

### 9.4 Image Transformer

Use `images` in kustomization to override registry/tag without patching Deployment:

```yaml
images:
  - name: api-server
    newName: harbor.example.com/doki-stack/api-server
    newTag: v1.2.3
```

Base Deployment uses `image: api-server:main`; Kustomize replaces with `newName:newTag`.

### 9.5 Reusable Components (Optional)

For shared patches across overlays:

```
components/
├── low-resources/
│   └── kustomization.yaml
├── high-availability/
│   └── kustomization.yaml
overlays/
├── dev/
│   └── kustomization.yaml  # references components/low-resources
└── prod/
    └── kustomization.yaml  # references components/high-availability
```

### 9.6 Validation

```bash
kustomize build overlays/dev | kubectl apply --dry-run=client -f -
kustomize build overlays/staging | kubectl apply --dry-run=client -f -
kustomize build overlays/prod | kubectl apply --dry-run=client -f -
```

---

## 10. Implementation Order

| Step | Phase | Action |
|------|-------|--------|
| 1 | Phase 0 | Create `overlays/dev/` directory structure |
| 2 | Phase 0 | Add dev `kustomization.yaml` and patches (replicas, resources, env-config) |
| 3 | Phase 0 | Configure Argo CD Application for dev overlay |
| 4 | Phase 0 | Validate dev overlay: `kustomize build overlays/dev` succeeds |
| 5 | Phase 0 | Deploy dev; run `./health-check.sh`; verify all services healthy |
| 6 | Phase 3 | Create `overlays/staging/` with staging patches |
| 7 | Phase 3 | Add staging ExternalSecret overrides (Vault paths) |
| 8 | Phase 3 | Configure Argo CD Application for staging |
| 9 | Phase 3 | Validate and deploy staging |
| 10 | Phase 4 | Create `overlays/prod/` with prod patches |
| 11 | Phase 4 | Add prod HPA, affinity, network policies |
| 12 | Phase 4 | Add prod ExternalSecret overrides |
| 13 | Phase 4 | Configure Argo CD Application for prod |
| 14 | Phase 4 | Validate and deploy prod |

### 10.1 Phase Dependencies

```
Phase 0 (Infra) → overlays/dev
Phase 3 (EE + Hardening) → overlays/staging
Phase 4 (Scale) → overlays/prod
```

Dev overlay is required for Phase 1–2 (MCP + agents). Staging and prod overlays are created when those phases begin.

---

## Appendix: Argo CD Application Example

For multi-namespace overlays (doki-platform, doki-mcp, doki-agents), use one Application per namespace or an ApplicationSet:

```yaml
# argocd/applications/doki-platform-dev.yaml
# One of several apps for dev overlay (one per namespace)
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: doki-platform-dev
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/org/infrastructure.git
    path: overlays/dev
    targetRevision: main
  destination:
    server: https://kubernetes.default.svc
    namespace: doki-platform
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

**Note:** When the overlay produces resources for multiple namespaces (doki-platform, doki-mcp, doki-agents), Argo CD applies each resource to its declared `metadata.namespace`. The `destination.namespace` is the fallback for resources without an explicit namespace. A single Application can sync the full overlay; ensure base manifests set the correct namespace per service.
