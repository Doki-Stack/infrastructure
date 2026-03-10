# CI/CD and GitOps — Implementation Plan

This document covers the CI/CD pipelines and GitOps deployment for the Doki Stack platform. It defines Harbor container registry configuration, Argo CD setup, GitHub Actions workflows, branch strategy, and implementation order.

**References:**
- `00-overview.md` — Repository structure, namespace layout, GitOps principles
- `01-cluster-and-networking.md` — Kind cluster, port mappings, DNS
- `02-data-services.md` — Data layer services (deployed in Wave 1)
- `03-security.md` — Kyverno image policy (Harbor-only images)
- `internal-docs/project-plan/08-cicd.md` — Pipeline flow, Helm chart index

---

## 1. Harbor Container Registry

### 1.1 Overview

| Property | Value |
|----------|-------|
| Helm chart | `harbor/harbor` |
| Helm repo | `https://helm.goharbor.io` |
| Namespace | `doki-system` |
| Access (dev) | `harbor.doki.local` (via `/etc/hosts` or CoreDNS) |
| Access (prod) | Ingress `harbor.doki.local` |

### 1.2 Helm Installation

```bash
helm repo add harbor https://helm.goharbor.io
helm repo update
helm install harbor harbor/harbor -n doki-system -f helm-values/harbor.yaml
```

### 1.3 Helm Values — Dev

File: `helm-values/harbor.yaml`

```yaml
# Harbor — dev configuration (NodePort)
# Helm: helm install harbor harbor/harbor -n doki-system -f helm-values/harbor.yaml
# Prod overlay: expose.type=ingress, persistence.size=100Gi, trivy.blockOnCritical=true

expose:
  type: nodePort
  tls:
    enabled: false  # Dev: HTTP only; prod overlay enables TLS
  nodePort:
    name: http
    ports:
      http: 30002   # Add to kind-config extraPortMappings if needed

externalURL: http://harbor.doki.local:30002

# Persistence
persistence:
  enabled: true
  persistentVolume:
    size: 20Gi
  imageChartStorage:
    type: filesystem
    filesystem:
      rootdirectory: /storage

# Trivy vulnerability scanning
trivy:
  enabled: true
  storageClass: ""
  persistence:
    size: 5Gi
  # Block push if critical CVE found (prod overlay)
  blockOnCritical: false  # Dev: warn only; prod: true

# Resource limits
resources:
  requests:
    memory: 512Mi
    cpu: 250m
  limits:
    memory: 1Gi
    cpu: "1000m"

# Disable notary, chartmuseum for minimal footprint (enable if needed)
notary:
  enabled: false
chartmuseum:
  enabled: false

# Core components
core:
  replicas: 1
  resources:
    requests:
      memory: 256Mi
      cpu: 100m
    limits:
      memory: 512Mi
      cpu: 500m

jobservice:
  replicas: 1
  resources:
    requests:
      memory: 256Mi
      cpu: 100m
    limits:
      memory: 512Mi
      cpu: 500m

registry:
  replicas: 1
  resources:
    requests:
      memory: 256Mi
      cpu: 100m
    limits:
      memory: 512Mi
      cpu: 500m
```

### 1.4 Prod Overlay

Create `helm-values/overlays/prod/harbor.yaml` for production:

```yaml
# Prod overlay — apply via Kustomize or separate values merge
expose:
  type: ingress
  tls:
    enabled: true
  ingress:
    hosts:
      core: harbor.doki.local
    className: traefik

trivy:
  blockOnCritical: true  # Block push on critical CVE

persistence:
  persistentVolume:
    size: 100Gi
```

### 1.5 Harbor Projects

| Project | Purpose | Image Prefix |
|---------|---------|--------------|
| `doki-ce` | Community Edition images | `harbor.doki.local/doki-ce/{service}:{tag}` |
| `doki-ee` | Enterprise Edition images | `harbor.doki.local/doki-ee/{service}:{tag}` |

Create projects via Harbor UI or API after initial install. CI uses robot accounts with push/pull to these projects.

### 1.6 Image Naming Convention

```
harbor.doki.local/doki-ce/{service}:{git-sha}
harbor.doki.local/doki-ce/{service}:{semver}   # release tags
harbor.doki.local/doki-ee/{service}:{git-sha}
```

Examples:
- `harbor.doki.local/doki-ce/mcp-scanner:abc1234`
- `harbor.doki.local/doki-ce/platform-ui:v1.2.3`

### 1.7 Retention Policy

| Environment | Retention | Implementation |
|-------------|-----------|----------------|
| **Dev** | Last 10 tags per repo | Harbor tag retention rule |
| **Prod** | Last 50 tags per repo | Harbor tag retention rule |

Configure via Harbor UI: Project → Tag Retention → Add Rule → "keep the last 10" / "keep the last 50".

### 1.8 Vulnerability Scanning

- **Trivy** integrated in Harbor; scans on push.
- **Dev:** `trivy.blockOnCritical: false` — log warning, allow push.
- **Prod:** `trivy.blockOnCritical: true` — block push if critical CVE found.
- CI also runs Trivy before push (fail fast).

### 1.9 Kind Port Mapping (Optional)

If using NodePort for Harbor in kind, add to `cluster/kind-config.yaml`:

```yaml
extraPortMappings:
  # ... existing ports ...
  - containerPort: 30002
    hostPort: 30002
    protocol: TCP
    # Harbor HTTP
```

### 1.10 DNS — Dev

Add to `/etc/hosts` (or CoreDNS configmap):

```
127.0.0.1 harbor.doki.local
```

---

## 2. Argo CD

### 2.1 Overview

| Property | Value |
|----------|-------|
| Helm chart | `argo/argo-cd` |
| Helm repo | `https://argoproj.github.io/argo-helm` |
| Namespace | `doki-system` (or `argocd` — chart creates it) |
| UI (dev) | NodePort 30080 (shared with Traefik) or dedicated 30444 |

### 2.2 Helm Installation

```bash
helm repo add argo https://argoproj.github.io/argo-helm
helm repo update
helm install argocd argo/argo-cd -n doki-system -f helm-values/argocd.yaml
```

### 2.3 Helm Values — Dev

File: `helm-values/argocd.yaml`

```yaml
# Argo CD — dev configuration
# Helm: helm install argocd argo/argo-cd -n doki-system -f helm-values/argocd.yaml

# Server exposure
server:
  service:
    type: NodePort
    nodePortHttp: 30444
    nodePortHttps: 30445
  replicas: 1
  resources:
    requests:
      memory: 256Mi
      cpu: 100m
    limits:
      memory: 512Mi
      cpu: 500m
  extraArgs:
    - --insecure  # Dev: disable TLS for UI

# Repo server
repoServer:
  replicas: 1
  resources:
    requests:
      memory: 256Mi
      cpu: 100m
    limits:
      memory: 512Mi
      cpu: 500m

# Application controller
controller:
  replicas: 1
  resources:
    requests:
      memory: 256Mi
      cpu: 100m
    limits:
      memory: 512Mi
      cpu: 500m

# Config
configs:
  repositories: |
    - url: https://github.com/doki-stack/infrastructure
      name: infrastructure
      type: git
  params:
    application.namespaces: "doki-data,doki-mcp,doki-agents,doki-platform,doki-ee,doki-system,monitoring,ai"
  cm:
    # Application resource customizations for CRDs
    application.resourceCustomizations: |
      networking.istio.io/v1beta1/VirtualService:
        health.lua: |
          hs = {}
          hs.status = "Healthy"
          hs.message = ""
          return hs
      argoproj.io/Application:
        health.lua: |
          hs = {}
          if obj.status ~= nil and obj.status.health ~= nil then
            hs.status = obj.status.health.status
            hs.message = obj.status.health.message or ""
          else
            hs.status = "Progressing"
            hs.message = "Waiting for health"
          end
          return hs
```

### 2.4 Application CR Template

File: `argocd/applications/_template.yaml`

```yaml
# Template for Argo CD Application CRs
# Copy and customize for each service. Replace {{SERVICE}}, {{PATH}}, {{NAMESPACE}}, {{WAVE}}
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: {{SERVICE}}
  namespace: argocd
  labels:
    app.kubernetes.io/part-of: doki-stack
  annotations:
    argocd.argoproj.io/sync-wave: "{{WAVE}}"
spec:
  project: default
  source:
    repoURL: https://github.com/doki-stack/infrastructure
    targetRevision: main
    path: {{PATH}}
    directory:
      recurse: false
  destination:
    server: https://kubernetes.default.svc
    namespace: {{NAMESPACE}}
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
      - PruneLast=true
  revisionHistoryLimit: 10
```

### 2.5 Example Application CRs

#### Data Service — PostgreSQL

File: `argocd/applications/postgresql.yaml`

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: postgresql
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "1"
spec:
  project: default
  source:
    repoURL: https://github.com/doki-stack/infrastructure
    targetRevision: main
    path: helm-values
    helm:
      valueFiles:
        - postgresql.yaml
      releaseName: postgres
  destination:
    server: https://kubernetes.default.svc
    namespace: doki-data
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

#### MCP Server — mcp-scanner

File: `argocd/applications/mcp-scanner.yaml`

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: mcp-scanner
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "3"
spec:
  project: default
  source:
    repoURL: https://github.com/doki-stack/infrastructure
    targetRevision: main
    path: overlays/dev
    directory:
      include: "mcp-scanner*"
    kustomize:
      namePrefix: ""
  destination:
    server: https://kubernetes.default.svc
    namespace: doki-mcp
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
  revisionHistoryLimit: 10
```

**Note:** Adjust `path` and `directory` to match actual Kustomize layout. If using `base/mcp-scanner` + `overlays/dev`, set `path: overlays/dev` and ensure kustomization includes mcp-scanner.

#### Platform — api-server

File: `argocd/applications/api-server.yaml`

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: api-server
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "5"
spec:
  project: default
  source:
    repoURL: https://github.com/doki-stack/infrastructure
    targetRevision: main
    path: base/api-server
    directory:
      recurse: false
  destination:
    server: https://kubernetes.default.svc
    namespace: doki-platform
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
  revisionHistoryLimit: 10
```

### 2.6 Sync Waves

| Wave | Scope | Services |
|------|-------|----------|
| **0** | Namespaces, RBAC | `cluster/namespaces.yaml`, cluster-scoped RBAC |
| **1** | Data services | PostgreSQL, MinIO, Qdrant, Dragonfly, RabbitMQ |
| **2** | Security | Vault, ESO (External Secrets Operator) |
| **3** | MCP servers | mcp-scanner, mcp-execution, mcp-policy, mcp-memory (EE), mcp-registry (EE) |
| **4** | Agents | agent-orchestrator, agent-automation, agent-review, agent-discovery (EE), agent-rollback (EE) |
| **5** | Platform | api-server, platform-ui |
| **6** | Kong | Kong gateway (depends on backend services for routes) |

### 2.7 Sync Policy by Environment

| Environment | SyncPolicy | Notes |
|-------------|------------|-------|
| **Dev** | `automated: { prune: true, selfHeal: true }` | Auto-sync on git change |
| **Staging** | `automated: false` | Manual sync or Argo CD UI |
| **Prod** | `automated: false` | Manual sync; HITL mandatory |

Override via overlay: `argocd/applications/overlays/prod/*.yaml` patches `syncPolicy.automated` to empty.

### 2.8 ApplicationSet for Bulk Management

File: `argocd/applicationset-services.yaml`

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: doki-services
  namespace: argocd
spec:
  generators:
    - list:
        elements:
          - name: postgresql
            path: helm-values
            namespace: doki-data
            wave: "1"
            helm: "postgresql.yaml"
          - name: minio
            path: helm-values
            namespace: doki-data
            wave: "1"
            helm: "minio.yaml"
          - name: mcp-scanner
            path: base/mcp-scanner
            namespace: doki-mcp
            wave: "3"
          - name: mcp-execution
            path: base/mcp-execution
            namespace: doki-mcp
            wave: "3"
          - name: mcp-policy
            path: base/mcp-policy
            namespace: doki-mcp
            wave: "3"
          - name: agent-orchestrator
            path: base/agent-orchestrator
            namespace: doki-agents
            wave: "4"
          - name: api-server
            path: base/api-server
            namespace: doki-platform
            wave: "5"
          - name: platform-ui
            path: base/platform-ui
            namespace: doki-platform
            wave: "5"
  template:
    metadata:
      name: '{{name}}'
      namespace: argocd
      annotations:
        argocd.argoproj.io/sync-wave: '{{wave}}'
    spec:
      project: default
      source:
        repoURL: https://github.com/doki-stack/infrastructure
        targetRevision: main
        path: '{{path}}'
        {{#if helm}}
        helm:
          valueFiles:
            - '{{helm}}'
        {{/if}}
      destination:
        server: https://kubernetes.default.svc
        namespace: '{{namespace}}'
      syncPolicy:
        automated:
          prune: true
          selfHeal: true
```

**Note:** ApplicationSet list generator does not support conditional `helm` in the template natively. For Helm-based apps, use separate Application CRs or a matrix generator. Simplified version without Helm:

```yaml
# Simplified ApplicationSet — use for Kustomize-based apps only
# Helm apps (postgresql, minio, etc.) remain as individual Application CRs
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: doki-kustomize-services
  namespace: argocd
spec:
  generators:
    - list:
        elements:
          - name: mcp-scanner
            path: overlays/dev
            namespace: doki-mcp
            wave: "3"
          - name: mcp-execution
            path: overlays/dev
            namespace: doki-mcp
            wave: "3"
          - name: api-server
            path: overlays/dev
            namespace: doki-platform
            wave: "5"
  template:
    metadata:
      name: '{{name}}'
      namespace: argocd
      annotations:
        argocd.argoproj.io/sync-wave: '{{wave}}'
    spec:
      project: default
      source:
        repoURL: https://github.com/doki-stack/infrastructure
        targetRevision: main
        path: '{{path}}'
        directory:
          include: '{{name}}*'
      destination:
        server: https://kubernetes.default.svc
        namespace: '{{namespace}}'
      syncPolicy:
        automated:
          prune: true
          selfHeal: true
```

### 2.9 App of Apps

File: `argocd/app-of-apps.yaml`

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: doki-app-of-apps
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: default
  source:
    repoURL: https://github.com/doki-stack/infrastructure
    targetRevision: main
    path: argocd/applications
    directory:
      recurse: true
      exclude: '_template.yaml'
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

### 2.10 Custom Health Checks

For CRDs (e.g., Kong Ingress, Kyverno Policy), add health checks in `configs.cm.application.resourceCustomizations`:

```yaml
configs:
  cm:
    application.resourceCustomizations: |
      # Kong Ingress
      configuration.konghq.com/v1/KongIngress:
        health.lua: |
          hs = {}
          hs.status = "Healthy"
          hs.message = ""
          return hs
      # Kyverno Policy
      kyverno.io/v1/ClusterPolicy:
        health.lua: |
          hs = {}
          if obj.status ~= nil and obj.status.ready ~= nil then
            if obj.status.ready == true then
              hs.status = "Healthy"
            else
              hs.status = "Progressing"
            end
          else
            hs.status = "Progressing"
          end
          hs.message = ""
          return hs
```

---

## 3. GitHub Actions CI Pipeline

### 3.1 Monorepo Structure

| Language | Tooling | Package Manager |
|----------|---------|-----------------|
| TypeScript | Turborepo | pnpm |
| Python | uv | uv |
| Rust | cargo | Cargo |
| Go | go modules | go mod |

### 3.2 Pipeline Stages

1. **Detect changed packages** — Turborepo `--filter`, path filters
2. **Lint** — eslint, ruff, clippy, golangci-lint
3. **Type check** — tsc, mypy, cargo check
4. **Test** — vitest, pytest, cargo test, go test
5. **Build** — Turborepo builds, Docker multi-stage
6. **Push to Harbor** — Docker push + Trivy scan
7. **Update image tags** — Commit to infrastructure repo (trigger Argo CD sync)

### 3.3 Workflow — CI (on PR)

File: `.github/workflows/ci.yml`

```yaml
name: CI

on:
  pull_request:
    branches: [main]
  push:
    branches: [main]

env:
  pnpm-store-dir: .pnpm-store

jobs:
  detect-changes:
    runs-on: ubuntu-latest
    outputs:
      ts: ${{ steps.filter.outputs.changes-ts }}
      py: ${{ steps.filter.outputs.changes-py }}
      rust: ${{ steps.filter.outputs.changes-rust }}
      go: ${{ steps.filter.outputs.changes-go }}
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 2

      - uses: dorny/paths-filter@v3
        id: filter
        with:
          filters: |
            changes-ts:
              - 'packages/**'
              - 'apps/**'
              - 'mcp-*/**'
              - '*.json'
              - 'pnpm-lock.yaml'
            changes-py:
              - 'agents/**'
              - '**/pyproject.toml'
              - '**/uv.lock'
            changes-rust:
              - 'mcp-scanner/**'
              - 'mcp-execution/**'
              - 'shared-rust/**'
              - '**/Cargo.toml'
            changes-go:
              - 'api-server/**'
              - 'mcp-policy/**'
              - '**/go.mod'

  lint-typecheck-test-ts:
    needs: detect-changes
    if: needs.detect-changes.outputs.changes-ts == 'true'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: pnpm/action-setup@v4
        with:
          version: 9

      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'pnpm'

      - name: Install dependencies
        run: pnpm install --frozen-lockfile

      - name: Lint
        run: pnpm run lint

      - name: Type check
        run: pnpm run typecheck

      - name: Test
        run: pnpm run test

  lint-typecheck-test-py:
    needs: detect-changes
    if: needs.detect-changes.outputs.changes-py == 'true'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: astral-sh/setup-uv@v4
        with:
          enable-cache: true
          cache-dependency-glob: "**/uv.lock"

      - name: Install dependencies
        run: uv sync

      - name: Lint
        run: uv run ruff check .

      - name: Type check
        run: uv run mypy .

      - name: Test
        run: uv run pytest

  lint-typecheck-test-rust:
    needs: detect-changes
    if: needs.detect-changes.outputs.changes-rust == 'true'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: dtolnay/rust-toolchain@stable
        with:
          components: clippy

      - uses: Swatinem/rust-cache@v2

      - name: Lint
        run: cargo clippy --all-targets -- -D warnings

      - name: Type check
        run: cargo check

      - name: Test
        run: cargo test

  lint-typecheck-test-go:
    needs: detect-changes
    if: needs.detect-changes.outputs.changes-go == 'true'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache-dependency-path: '**/go.sum'

      - name: golangci-lint
        uses: golangci/golangci-lint-action@v4
        with:
          version: latest

      - name: Test
        run: go test ./...
```

### 3.4 Workflow — Build and Push (on merge to main)

File: `.github/workflows/build-push.yml`

```yaml
name: Build and Push

on:
  push:
    branches: [main]

env:
  REGISTRY: harbor.doki.local
  CE_PROJECT: doki-ce

jobs:
  build-push:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        include:
          - service: mcp-scanner
            context: ./mcp-scanner
            dockerfile: ./mcp-scanner/Dockerfile
          - service: mcp-execution
            context: ./mcp-execution
            dockerfile: ./mcp-execution/Dockerfile
          - service: mcp-policy
            context: ./mcp-policy
            dockerfile: ./mcp-policy/Dockerfile
          - service: agent-orchestrator
            context: ./agents/orchestrator
            dockerfile: ./agents/orchestrator/Dockerfile
          - service: api-server
            context: ./api-server
            dockerfile: ./api-server/Dockerfile
          - service: platform-ui
            context: ./apps/platform-ui
            dockerfile: ./apps/platform-ui/Dockerfile
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to Harbor
        uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ secrets.HARBOR_USERNAME }}
          password: ${{ secrets.HARBOR_PASSWORD }}

      - name: Extract metadata
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ${{ env.REGISTRY }}/${{ env.CE_PROJECT }}/${{ matrix.service }}
          tags: |
            type=sha,prefix=
            type=raw,value=latest,enable=${{ github.ref == 'refs/heads/main' }}

      - name: Build and push
        uses: docker/build-push-action@v6
        with:
          context: ${{ matrix.context }}
          file: ${{ matrix.dockerfile }}
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max

      - name: Run Trivy scan
        uses: aquasecurity/trivy-action@master
        with:
          image-ref: ${{ env.REGISTRY }}/${{ env.CE_PROJECT }}/${{ matrix.service }}:${{ github.sha }}
          format: 'sarif'
          output: 'trivy-results.sarif'
          severity: 'CRITICAL,HIGH'
        continue-on-error: true  # Dev: don't fail; prod: fail on CRITICAL

  update-infra-repo:
    needs: build-push
    runs-on: ubuntu-latest
    if: github.ref == 'refs/heads/main'
    steps:
      - uses: actions/checkout@v4
        with:
          repository: doki-stack/infrastructure
          token: ${{ secrets.INFRA_REPO_TOKEN }}
          path: infra

      - name: Update image tags
        run: |
          cd infra
          # Option A: kustomize edit set image — run from overlay dir
          # Assumes overlays/dev/kustomization.yaml has images for each service
          cd overlays/dev
          kustomize edit set image \
            harbor.doki.local/doki-ce/mcp-scanner=harbor.doki.local/doki-ce/mcp-scanner:${{ github.sha }} \
            harbor.doki.local/doki-ce/mcp-execution=harbor.doki.local/doki-ce/mcp-execution:${{ github.sha }} \
            harbor.doki.local/doki-ce/mcp-policy=harbor.doki.local/doki-ce/mcp-policy:${{ github.sha }} \
            harbor.doki.local/doki-ce/agent-orchestrator=harbor.doki.local/doki-ce/agent-orchestrator:${{ github.sha }} \
            harbor.doki.local/doki-ce/api-server=harbor.doki.local/doki-ce/api-server:${{ github.sha }} \
            harbor.doki.local/doki-ce/platform-ui=harbor.doki.local/doki-ce/platform-ui:${{ github.sha }}
          cd ../..
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git add -A
          git diff --staged --quiet || git commit -m "chore: update image tags to ${{ github.sha }}"
          git push
```

### 3.5 Docker Multi-Stage Build Pattern

Example for Rust (mcp-scanner):

```dockerfile
# mcp-scanner/Dockerfile
FROM rust:1.75-bookworm AS chef
RUN cargo install cargo-chef
WORKDIR /app

FROM chef AS planner
COPY . .
RUN cargo chef prepare --recipe-path recipe.json

FROM chef AS builder
COPY --from=planner /app/recipe.json recipe.json
RUN cargo chef cook --release --recipe-path recipe.json
COPY . .
RUN cargo build --release

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/target/release/mcp-scanner /usr/local/bin/
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/mcp-scanner"]
```

Example for TypeScript (platform-ui):

```dockerfile
# apps/platform-ui/Dockerfile
FROM node:20-alpine AS deps
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN corepack enable pnpm && pnpm install --frozen-lockfile

FROM node:20-alpine AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN corepack enable pnpm && pnpm build

FROM node:20-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
COPY --from=builder /app/public ./public
EXPOSE 3000
CMD ["node", "server.js"]
```

Example for Python (agent-orchestrator):

```dockerfile
# agents/orchestrator/Dockerfile
FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim AS builder
WORKDIR /app
COPY pyproject.toml uv.lock ./
RUN uv sync --no-dev --no-install-project

FROM python:3.12-slim
WORKDIR /app
COPY --from=builder /app/.venv /app/.venv
COPY . .
ENV PATH="/app/.venv/bin:$PATH"
EXPOSE 8000
CMD ["python", "-m", "agent_orchestrator"]
```

### 3.6 Caching Strategy

| Stack | Cache Key | Action |
|-------|-----------|--------|
| pnpm | `pnpm-store` | `cache: 'pnpm'` in setup-node |
| uv | `**/uv.lock` | `enable-cache: true` in setup-uv |
| Cargo | `target/` | `Swatinem/rust-cache` |
| Go | `**/go.sum` | `cache-dependency-path` in setup-go |
| Docker | GHA cache | `cache-from: type=gha`, `cache-to: type=gha,mode=max` |

---

## 4. GitOps Workflow

### 4.1 Developer Flow — Application Code

```
Code change → PR → CI (lint, typecheck, test) → Merge to main
  → Build & Push workflow → Docker build → Push to Harbor
  → Trivy scan → Update image tag in infrastructure repo
  → Argo CD detects change → Sync → Deployed
```

### 4.2 Infrastructure Change Flow

```
Manifest change (base/, overlays/, helm-values/) → PR → Review
  → Merge to main → Argo CD auto-sync (dev) or manual sync (staging/prod)
  → Cluster reconciled
```

### 4.3 Image Tag Update Strategy

| Option | Mechanism | Pros | Cons |
|--------|-----------|------|------|
| **A** | `kustomize edit set image` in CI | Single source of truth in Git | Requires CI to push to infra repo |
| **B** | Argo CD Image Updater | Watches Harbor for new tags | Extra component; may drift from Git |

**Recommended:** Option A — CI updates infrastructure repo with new image tags. Git remains source of truth; rollback via git revert.

### 4.4 Rollback

1. **Git revert** — Revert the commit that updated the image tag.
2. **Argo CD sync** — Argo CD syncs to previous revision.
3. **Argo CD UI** — One-click rollback to previous Application revision (if not using auto-sync).

```bash
# Manual rollback via Argo CD CLI
argocd app rollback mcp-scanner <revision>
```

---

## 5. Branch Strategy

### 5.1 Application Repo (Monorepo)

| Branch | Purpose | Deploys To |
|--------|---------|------------|
| `main` | Always deployable | Dev (auto) |
| `release/*` | Staging/prod cuts | Staging, Prod (manual) |
| `feature/*` | Development | — (no deploy) |

### 5.2 Infrastructure Repo

| Branch | Purpose |
|--------|---------|
| `main` | Single branch; all environments via Kustomize overlays |

Overlays: `overlays/dev`, `overlays/staging`, `overlays/prod` select environment. Argo CD Applications point to `path: overlays/dev` (or staging/prod) per cluster.

---

## 6. Secrets in CI

### 6.1 GitHub Actions Secrets

| Secret | Purpose |
|--------|---------|
| `HARBOR_USERNAME` | Harbor robot account or CI user |
| `HARBOR_PASSWORD` | Harbor robot account token |
| `INFRA_REPO_TOKEN` | PAT or GitHub App token for pushing to infrastructure repo |
| `KUBECONFIG` | (Optional) For direct Argo CD sync; prefer Git-based flow |

### 6.2 Harbor Access

- **CI uses robot account** — Create in Harbor: Project → Robot Accounts → Add Robot.
- **Permissions:** Push and pull to `doki-ce` (and `doki-ee` if building EE).
- **Never use admin** — Principle of least privilege.

### 6.3 Never Commit

- Passwords, tokens, API keys
- `kubeconfig` files
- `.env` with secrets

Use GitHub Secrets, Vault, or External Secrets Operator for runtime secrets in cluster.

---

## 7. Implementation Order

| Step | Task | Owner | Depends On |
|------|------|-------|------------|
| 1 | Install Harbor in `doki-system` | Platform | kind cluster, helm |
| 2 | Configure Harbor projects (`doki-ce`, `doki-ee`), retention, Trivy | Platform | Step 1 |
| 3 | Create Harbor robot account for CI | Platform | Step 2 |
| 4 | Install Argo CD in `doki-system` | Platform | kind cluster |
| 5 | Create Application CRs for data services (Wave 1) | DevOps | Step 4, helm-values |
| 6 | Set up GitHub Actions `ci.yml` | DevOps | — |
| 7 | Set up GitHub Actions `build-push.yml` | DevOps | Step 3 |
| 8 | Test full pipeline: commit → build → push → deploy | DevOps | Steps 5–7 |
| 9 | Add sync wave ordering to all Applications | DevOps | Step 8 |
| 10 | Configure ApplicationSet for bulk management | DevOps | Step 9 |

---

## Appendix A: Kind Config with Harbor Port

Add to `cluster/kind-config.yaml` if using Harbor NodePort:

```yaml
extraPortMappings:
  # ... existing ...
  - containerPort: 30002
    hostPort: 30002
    protocol: TCP
    # Harbor HTTP
```

---

## Appendix B: Argo CD CLI Access

```bash
# Get initial admin password
kubectl -n doki-system get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d

# Login (dev, insecure)
argocd login localhost:30444 --insecure --username admin --password <password>
```

---

## Appendix C: Turborepo Remote Caching (Optional)

For faster CI, enable Turborepo remote cache (Vercel or self-hosted):

```yaml
# In ci.yml, add:
env:
  TURBO_TOKEN: ${{ secrets.TURBO_TOKEN }}
  TURBO_TEAM: ${{ vars.TURBO_TEAM }}
```

---

*Document version: 1.0. Last updated: 2025-03.*
