# Kong API Gateway — Implementation Plan

This document covers Kong API Gateway configuration for the Doki Stack platform. Kong provides routing, authentication, rate limiting, and security headers at the edge. It runs in **DB-less mode** with declarative configuration managed by the Kong Ingress Controller (KIC) via Kubernetes CRDs.

**References:**
- `implementation-plan/00-overview.md` — Repository structure, phases, namespace layout
- `implementation-plan/01-cluster-and-networking.md` — Traefik ingress, port mappings (30080/30443)
- `implementation-plan/02-data-services.md` — Dragonfly (Redis-compatible) for rate limiting
- `implementation-plan/03-security.md` — Auth0, Vault, Cilium policies

---

## 1. Kong Installation

### 1.1 Overview

| Property | Value |
|----------|-------|
| Helm chart | `kong/kong` |
| Namespace | `doki-system` |
| Mode | DB-less (declarative config via KIC CRDs) |
| Ingress Controller | Kong Ingress Controller (KIC) |

### 1.2 Helm Repository

```bash
helm repo add kong https://charts.konghq.com
helm repo update
```

### 1.3 Helm Values — `helm-values/kong.yaml`

File: `helm-values/kong.yaml`

```yaml
# Kong API Gateway — dev configuration
# Helm: helm install kong kong/kong -n doki-system -f helm-values/kong.yaml

# -----------------------------------------------------------------------------
# Kong core
# -----------------------------------------------------------------------------
env:
  database: "off"                    # DB-less mode
  router_flavor: "traditional"
  nginx_worker_processes: "2"
  proxy_access_log: /dev/stdout
  proxy_error_log: /dev/stderr
  admin_access_log: /dev/stdout
  admin_error_log: /dev/stderr
  prefix: /kong_prefix/

# -----------------------------------------------------------------------------
# Proxy service — NodePort for dev (30080 HTTP, 30443 HTTPS)
# -----------------------------------------------------------------------------
proxy:
  enabled: true
  type: NodePort
  http:
    enabled: true
    servicePort: 80
    containerPort: 8000
    nodePort: 30080
  tls:
    enabled: true
    servicePort: 443
    containerPort: 8443
    nodePort: 30443
    parameters:
      - http2

# -----------------------------------------------------------------------------
# Admin API — local only, not exposed externally in prod
# -----------------------------------------------------------------------------
admin:
  enabled: true
  type: ClusterIP
  http:
    enabled: true
    servicePort: 8001
    containerPort: 8001
  tls:
    enabled: true
    servicePort: 8444
    containerPort: 8444
    parameters:
      - http2
  ingress:
    enabled: false

# -----------------------------------------------------------------------------
# Kong Ingress Controller (KIC)
# -----------------------------------------------------------------------------
ingressController:
  enabled: true
  ingressClass: kong
  createIngressClass: true
  env:
    kong_admin_tls_skip_verify: true

# -----------------------------------------------------------------------------
# Resources
# -----------------------------------------------------------------------------
resources:
  requests:
    memory: 256Mi
    cpu: 250m
  limits:
    memory: 512Mi
    cpu: 500m

# -----------------------------------------------------------------------------
# DB-less config — leave empty when using KIC (KIC manages config via CRDs)
# -----------------------------------------------------------------------------
dblessConfig:
  configMap: ""
  secret: ""
  config: ""

# -----------------------------------------------------------------------------
# Migrations — disable for DB-less
# -----------------------------------------------------------------------------
migrations:
  preUpgrade: false
  postUpgrade: false

# -----------------------------------------------------------------------------
# PostgreSQL — not used in DB-less mode
# -----------------------------------------------------------------------------
postgresql:
  enabled: false
```

### 1.4 Install Command

```bash
helm install kong kong/kong \
  --namespace doki-system \
  --create-namespace \
  -f helm-values/kong.yaml
```

**Note:** The cluster doc defines Traefik on ports 30080/30443. If both Kong and Traefik are used, either: (a) use Kong as the primary ingress (Traefik disabled or on different ports), or (b) use Kong as ClusterIP and have Traefik forward to Kong. For dev, Kong NodePort 30080/30443 allows direct access to the API gateway.

### 1.5 Production Overlay

For production (`helm-values/overlays/prod/kong.yaml`), override:

- `proxy.type: LoadBalancer` (or keep NodePort if behind external LB)
- `admin.ingress.enabled: false` (admin never exposed)
- `resources`: increase to 512Mi/500m → 1Gi/1000m if needed
- Add Prometheus plugin for metrics

---

## 2. Route Configuration

Routes are defined via standard Kubernetes `Ingress` resources with Kong-specific annotations, or via `KongIngress` CRDs for fine-grained control. The KIC reconciles these into Kong's configuration.

### 2.1 Internal vs External Routes

| Visibility | Routes | Notes |
|------------|--------|-------|
| **External** | `/`, `/api/*`, `/agents/*` | Exposed through Kong; platform-ui, api-server, agent-orchestrator |
| **Internal only** | `/mcp/scanner/*`, `/mcp/execution/*`, `/mcp/policy/*` | Not exposed through Kong in production; cluster-internal only |

In production, MCP servers are called directly by api-server and agents within the cluster. Kong does **not** expose MCP paths externally. For dev/testing, MCP routes may be exposed via Kong.

### 2.2 CE Routes (Phase 1) — `kong/routes/ce-routes.yaml`

```yaml
# kong/routes/ce-routes.yaml
# CE (Community Edition) routes — Phase 1
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: platform-ui
  namespace: doki-platform
  annotations:
    konghq.com/strip-path: "false"
    konghq.com/plugins: correlation-id, platform-ui-rate-limit, request-transformer, response-transformer, cors
spec:
  ingressClassName: kong
  rules:
    - host: app.doki.local
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: platform-ui
                port:
                  number: 3000
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: api-server
  namespace: doki-platform
  annotations:
    konghq.com/strip-path: "false"
    konghq.com/plugins: correlation-id, jwt-auth, api-rate-limit, request-transformer, response-transformer, cors, request-size-limit
spec:
  ingressClassName: kong
  rules:
    - host: api.doki.local
      http:
        paths:
          - path: /api
            pathType: Prefix
            backend:
              service:
                name: api-server
                port:
                  number: 3000
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: mcp-scanner
  namespace: doki-mcp
  annotations:
    konghq.com/strip-path: "false"
    konghq.com/plugins: correlation-id, internal-only, request-transformer
  labels:
    doki.io/route-type: internal
spec:
  ingressClassName: kong
  rules:
    - host: mcp.doki.local
      http:
        paths:
          - path: /mcp/scanner
            pathType: Prefix
            backend:
              service:
                name: mcp-scanner
                port:
                  number: 3000
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: mcp-execution
  namespace: doki-mcp
  annotations:
    konghq.com/strip-path: "false"
    konghq.com/plugins: correlation-id, internal-only, request-transformer
  labels:
    doki.io/route-type: internal
spec:
  ingressClassName: kong
  rules:
    - host: mcp.doki.local
      http:
        paths:
          - path: /mcp/execution
            pathType: Prefix
            backend:
              service:
                name: mcp-execution
                port:
                  number: 3000
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: mcp-policy
  namespace: doki-mcp
  annotations:
    konghq.com/strip-path: "false"
    konghq.com/plugins: correlation-id, internal-only, request-transformer
  labels:
    doki.io/route-type: internal
spec:
  ingressClassName: kong
  rules:
    - host: mcp.doki.local
      http:
        paths:
          - path: /mcp/policy
            pathType: Prefix
            backend:
              service:
                name: mcp-policy
                port:
                  number: 3000
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: agent-orchestrator
  namespace: doki-agents
  annotations:
    konghq.com/strip-path: "false"
    konghq.com/plugins: correlation-id, jwt-auth, agent-rate-limit, request-transformer, response-transformer, cors
spec:
  ingressClassName: kong
  rules:
    - host: api.doki.local
      http:
        paths:
          - path: /agents
            pathType: Prefix
            backend:
              service:
                name: agent-orchestrator
                port:
                  number: 8000
```

### 2.3 CE Route Summary

| Path | Service | Namespace | Port | Auth | Rate Limit |
|------|---------|-----------|------|------|------------|
| `/` | platform-ui | doki-platform | 3000 | Session (cookie) | 200/min |
| `/api/*` | api-server | doki-platform | 3000 | JWT (Auth0) | 100/min |
| `/mcp/scanner/*` | mcp-scanner | doki-mcp | 3000 | Internal only | N/A |
| `/mcp/execution/*` | mcp-execution | doki-mcp | 3000 | Internal only | N/A |
| `/mcp/policy/*` | mcp-policy | doki-mcp | 3000 | Internal only | N/A |
| `/agents/*` | agent-orchestrator | doki-agents | 8000 | JWT (Auth0) | 50/min |

### 2.4 EE Routes (Phase 3–4) — `kong/routes/ee-routes.yaml`

```yaml
# kong/routes/ee-routes.yaml
# EE (Enterprise Edition) routes — Phase 3–4
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ee-license-server
  namespace: doki-ee
  annotations:
    konghq.com/plugins: jwt-auth, api-rate-limit, request-transformer
spec:
  ingressClassName: kong
  rules:
    - host: api.doki.local
      http:
        paths:
          - path: /api/license
            pathType: Exact
            backend:
              service:
                name: ee-license-server
                port:
                  number: 3000
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: mcp-memory
  namespace: doki-mcp
  annotations:
    konghq.com/plugins: jwt-auth, api-rate-limit, request-transformer
spec:
  ingressClassName: kong
  rules:
    - host: api.doki.local
      http:
        paths:
          - path: /api/memory
            pathType: Prefix
            backend:
              service:
                name: mcp-memory
                port:
                  number: 3000
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: mcp-registry
  namespace: doki-mcp
  annotations:
    konghq.com/plugins: jwt-auth, api-rate-limit, request-transformer
spec:
  ingressClassName: kong
  rules:
    - host: api.doki.local
      http:
        paths:
          - path: /api/registry
            pathType: Prefix
            backend:
              service:
                name: mcp-registry
                port:
                  number: 3000
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ee-multi-tenancy
  namespace: doki-ee
  annotations:
    konghq.com/plugins: jwt-auth, api-rate-limit, request-transformer
spec:
  ingressClassName: kong
  rules:
    - host: api.doki.local
      http:
        paths:
          - path: /api/orgs
            pathType: Prefix
            backend:
              service:
                name: ee-multi-tenancy
                port:
                  number: 3000
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ee-notifications
  namespace: doki-ee
  annotations:
    konghq.com/plugins: jwt-auth, api-rate-limit, request-transformer
spec:
  ingressClassName: kong
  rules:
    - host: api.doki.local
      http:
        paths:
          - path: /api/notifications
            pathType: Prefix
            backend:
              service:
                name: ee-notifications
                port:
                  number: 3000
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ee-compliance
  namespace: doki-ee
  annotations:
    konghq.com/plugins: jwt-auth, api-rate-limit, request-transformer
spec:
  ingressClassName: kong
  rules:
    - host: api.doki.local
      http:
        paths:
          - path: /api/compliance
            pathType: Prefix
            backend:
              service:
                name: ee-compliance
                port:
                  number: 3000
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ee-governance
  namespace: doki-ee
  annotations:
    konghq.com/plugins: jwt-auth, api-rate-limit, request-transformer
spec:
  ingressClassName: kong
  rules:
    - host: api.doki.local
      http:
        paths:
          - path: /api/governance
            pathType: Prefix
            backend:
              service:
                name: ee-governance
                port:
                  number: 3000
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ee-dashboards
  namespace: doki-ee
  annotations:
    konghq.com/plugins: jwt-auth, api-rate-limit, request-transformer
spec:
  ingressClassName: kong
  rules:
    - host: api.doki.local
      http:
        paths:
          - path: /api/dashboards
            pathType: Prefix
            backend:
              service:
                name: ee-dashboards
                port:
                  number: 3000
```

### 2.5 EE Route Summary

| Path | Service | Namespace |
|------|---------|-----------|
| `/api/license` | ee-license-server | doki-ee |
| `/api/memory/*` | mcp-memory | doki-mcp |
| `/api/registry/*` | mcp-registry | doki-mcp |
| `/api/orgs/*` | ee-multi-tenancy | doki-ee |
| `/api/notifications/*` | ee-notifications | doki-ee |
| `/api/compliance/*` | ee-compliance | doki-ee |
| `/api/governance/*` | ee-governance | doki-ee |
| `/api/dashboards/*` | ee-dashboards | doki-ee |

---

## 3. JWT Authentication Plugin

Kong validates Auth0-issued JWTs using the **OpenID Connect** plugin (supports JWKS discovery) or the **JWT** plugin with pre-configured keys. For Auth0, the **OpenID Connect** plugin is recommended.

### 3.1 KongPlugin — JWT (Auth0 JWKS) — `kong/kong-plugins/jwt-auth.yaml`

**Option A: OpenID Connect plugin (recommended for Auth0)**

```yaml
# kong/kong-plugins/jwt-auth.yaml
# Auth0 JWT validation via OpenID Connect plugin (JWKS discovery)
apiVersion: configuration.konghq.com/v1
kind: KongPlugin
metadata:
  name: jwt-auth
  namespace: doki-system
  labels:
    app.kubernetes.io/part-of: doki-stack
plugin: openid-connect
config:
  issuer: "https://${AUTH0_TENANT}.auth0.com/"
  # JWKS URI is auto-discovered from issuer/.well-known/openid-configuration
  auth_methods:
    - bearer
  scopes_required: []
  scopes_claim: []
  audience_required: []
  # Claims to extract and inject as headers (claim → header by array position)
  upstream_headers_claims:
    - sub
    - org_id
    - roles
  upstream_headers_names:
    - X-User-Id
    - X-Org-Id
    - X-User-Roles
  # Cache TTL for JWKS
  cache_ttl: 3600
  # Optional: restrict to specific Auth0 audience
  # audience:
  #   - "https://api.doki.local"
```

**Note:** Replace `${AUTH0_TENANT}` with the Auth0 tenant domain (e.g. `doki-stack` for `doki-stack.auth0.com`). In production, use a ConfigMap or External Secrets for the issuer URL.

**Option B: JWT plugin with static JWKS (if OIDC plugin unavailable)**

```yaml
# kong/kong-plugins/jwt-auth.yaml
# Alternative: JWT plugin — requires Kong Enterprise or custom plugin for JWKS
# For Kong OSS, use key-auth or pre-configured JWT credentials per consumer
apiVersion: configuration.konghq.com/v1
kind: KongPlugin
metadata:
  name: jwt-auth
  namespace: doki-system
plugin: jwt
config:
  uri_param_names: []
  cookie_names: []
  claims_to_verify: []
  key_claim_name: kid
  secret_is_base64: false
  # For Kong OSS JWT: keys must be configured per consumer via KongConsumer CRD
  # Auth0 JWKS: use Kong's jwt-key-auth or openid-connect plugin
```

**Recommended:** Use the OpenID Connect plugin. If using Kong OSS without it, configure a custom plugin or use a sidecar/proxy for JWT validation.

### 3.2 Auth0 Custom Claims

Ensure Auth0 rules/actions add `org_id` and `roles` to the token:

```javascript
// Auth0 Action: Add org_id and roles to token
exports.onExecutePostLogin = async (event, api) => {
  const namespace = 'https://api.doki.local';
  api.accessToken.setCustomClaim(`${namespace}/org_id`, event.user.app_metadata?.org_id);
  api.accessToken.setCustomClaim(`${namespace}/roles`, event.user.app_metadata?.roles || []);
};
```

In Kong's `upstream_headers_claims`, use the full namespaced claim name if Auth0 uses a custom namespace (e.g. `https://api.doki.local/org_id`). Kong OIDC supports both short and namespaced claim names.

---

## 4. Rate Limiting Plugin

Rate limiting uses Dragonfly (Redis-compatible) as the backend. The `policy` must be `redis` for DB-less mode (cluster policy is not supported).

### 4.1 KongPlugin — Rate Limiting — `kong/kong-plugins/rate-limiting.yaml`

```yaml
# kong/kong-plugins/rate-limiting.yaml
# Rate limiting with Dragonfly (Redis-compatible) backend
apiVersion: configuration.konghq.com/v1
kind: KongPlugin
metadata:
  name: api-rate-limit
  namespace: doki-system
  labels:
    app.kubernetes.io/part-of: doki-stack
plugin: rate-limiting
config:
  policy: redis
  redis:
    host: dragonfly.doki-data.svc.cluster.local
    port: 6379
    # Optional: password from Vault/ESO in prod
    # password:
    #   valueFrom:
    #     secretKeyRef:
    #       name: dragonfly-credentials
    #       key: password
  minute: 100
  hour: 5000
  fault_tolerant: true
  limit_by: header
  header_name: X-User-Id
  # Fallback to IP if no user header (unauthenticated)
  # Use consumer when JWT is present
---
apiVersion: configuration.konghq.com/v1
kind: KongPlugin
metadata:
  name: platform-ui-rate-limit
  namespace: doki-system
plugin: rate-limiting
config:
  policy: redis
  redis:
    host: dragonfly.doki-data.svc.cluster.local
    port: 6379
  minute: 200
  fault_tolerant: true
  limit_by: ip
---
apiVersion: configuration.konghq.com/v1
kind: KongPlugin
metadata:
  name: agent-rate-limit
  namespace: doki-system
plugin: rate-limiting
config:
  policy: redis
  redis:
    host: dragonfly.doki-data.svc.cluster.local
    port: 6379
  minute: 50
  fault_tolerant: true
  limit_by: header
  header_name: X-User-Id
```

### 4.2 Rate Limit Policies Summary

| Plugin | Limit | Key | Use Case |
|--------|-------|-----|----------|
| api-rate-limit | 100/min | X-User-Id (or IP) | Standard API |
| platform-ui-rate-limit | 200/min | IP | UI (session-based) |
| agent-rate-limit | 50/min | X-User-Id | Agent orchestrator |

### 4.3 Write Operations and SSE (Phase 3+)

For write operations (20 req/min) and SSE (max 10 concurrent per user), use Kong's **response-ratelimiting** or **request-termination** in combination, or Kong Enterprise **Rate Limiting Advanced** plugin. For OSS:

```yaml
# kong/kong-plugins/rate-limiting-write.yaml (Phase 3+)
apiVersion: configuration.konghq.com/v1
kind: KongPlugin
metadata:
  name: write-rate-limit
  namespace: doki-system
plugin: rate-limiting
config:
  policy: redis
  redis:
    host: dragonfly.doki-data.svc.cluster.local
    port: 6379
  minute: 20
  limit_by: header
  header_name: X-User-Id
```

Key pattern for Redis: `rate:{org_id}:{user_id}` — the Kong rate-limiting plugin uses `limit_by` to form the key; for `header_name: X-User-Id`, the key will be based on that header. To include `org_id`, use a custom plugin or Kong Enterprise.

---

## 5. Additional Plugins

### 5.1 Request Transformer — `kong/kong-plugins/request-transformer.yaml`

Use for adding static headers. For `X-Request-Id`, prefer the correlation-id plugin (see §5.2).

```yaml
# kong/kong-plugins/request-transformer.yaml
apiVersion: configuration.konghq.com/v1
kind: KongPlugin
metadata:
  name: request-transformer
  namespace: doki-system
plugin: request-transformer
config:
  add:
    headers: []
  append:
    headers: []
  remove:
    headers: []
```

Apply the `correlation-id` plugin (§5.2) for `X-Request-Id` propagation.

### 5.2 Correlation ID — `kong/kong-plugins/correlation-id.yaml`

```yaml
# kong/kong-plugins/correlation-id.yaml
apiVersion: configuration.konghq.com/v1
kind: KongPlugin
metadata:
  name: correlation-id
  namespace: doki-system
plugin: correlation-id
config:
  header_name: X-Request-Id
  generator: uuid
  echo_downstream: true
```

### 5.3 Response Transformer — `kong/kong-plugins/response-transformer.yaml`

```yaml
# kong/kong-plugins/response-transformer.yaml
apiVersion: configuration.konghq.com/v1
kind: KongPlugin
metadata:
  name: response-transformer
  namespace: doki-system
plugin: response-transformer
config:
  add:
    headers:
      - "Strict-Transport-Security:max-age=31536000; includeSubDomains; preload"
      - "X-Content-Type-Options:nosniff"
      - "X-Frame-Options:DENY"
      - "X-XSS-Protection:1; mode=block"
      - "Content-Security-Policy:default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'"
  remove:
    headers: []
```

### 5.4 CORS — `kong/kong-plugins/cors.yaml`

```yaml
# kong/kong-plugins/cors.yaml
apiVersion: configuration.konghq.com/v1
kind: KongPlugin
metadata:
  name: cors
  namespace: doki-system
plugin: cors
config:
  origins:
    - "https://app.doki.local"
    - "http://localhost:3000"
    - "http://127.0.0.1:3000"
  methods:
    - GET
    - POST
    - PUT
    - PATCH
    - DELETE
    - OPTIONS
  headers:
    - Accept
    - Authorization
    - Content-Type
    - X-Request-Id
    - X-Org-Id
  exposed_headers:
    - X-Request-Id
  credentials: true
  max_age: 3600
```

### 5.5 Request Size Limiting — `kong/kong-plugins/request-size-limit.yaml`

```yaml
# kong/kong-plugins/request-size-limit.yaml
apiVersion: configuration.konghq.com/v1
kind: KongPlugin
metadata:
  name: request-size-limit
  namespace: doki-system
plugin: request-size-limiting
config:
  allowed_payload_size: 10
  size_unit: megabytes
```

### 5.6 IP Restriction (Production) — `kong/kong-plugins/ip-restriction.yaml`

```yaml
# kong/kong-plugins/ip-restriction.yaml
# Apply only in prod overlay
apiVersion: configuration.konghq.com/v1
kind: KongPlugin
metadata:
  name: ip-restriction
  namespace: doki-system
plugin: ip-restriction
config:
  allow:
    - "10.0.0.0/8"
    - "172.16.0.0/12"
    - "192.168.0.0/16"
  # Or use deny for blocklist
  # deny: []
```

### 5.7 Internal-Only (MCP) — `kong/kong-plugins/internal-only.yaml`

For MCP routes, use IP restriction to allow only cluster-internal CIDRs, or a network policy. In dev, this plugin can be a no-op or allow all.

```yaml
# kong/kong-plugins/internal-only.yaml
# Restrict MCP routes to cluster-internal IPs (prod)
apiVersion: configuration.konghq.com/v1
kind: KongPlugin
metadata:
  name: internal-only
  namespace: doki-system
plugin: ip-restriction
config:
  allow:
    - "10.0.0.0/8"
    - "172.16.0.0/12"
    - "192.168.0.0/16"
```

---

## 6. Internal vs External Routes

### 6.1 Summary

| Route Type | Paths | Exposed via Kong | Callers |
|------------|-------|------------------|---------|
| **External** | `/`, `/api/*`, `/agents/*` | Yes | Browser, external clients |
| **Internal** | `/mcp/scanner/*`, `/mcp/execution/*`, `/mcp/policy/*` | No (prod) / Yes (dev) | api-server, agents (in-cluster) |

### 6.2 Architecture

```
                    ┌─────────────────────────────────────────────────────────┐
                    │                     External                            │
                    └─────────────────────────────────────────────────────────┘
                                              │
                                              ▼
                    ┌─────────────────────────────────────────────────────────┐
                    │  Traefik (30080/30443) → Kong (proxy 8000/8443)         │
                    └─────────────────────────────────────────────────────────┘
                                              │
                    ┌─────────────────────────┼─────────────────────────────┐
                    │                         │                             │
                    ▼                         ▼                             ▼
            ┌───────────────┐         ┌───────────────┐             ┌───────────────┐
            │ platform-ui   │         │ api-server    │             │ agent-        │
            │ (/)           │         │ (/api/*)     │             │ orchestrator  │
            │ Session auth  │         │ JWT auth      │             │ (/agents/*)  │
            │ 200/min       │         │ 100/min       │             │ JWT, 50/min  │
            └───────────────┘         └───────┬───────┘             └───────────────┘
                                             │
                                             │ In-cluster (no Kong)
                                             ▼
                    ┌─────────────────────────────────────────────────────────┐
                    │  mcp-scanner, mcp-execution, mcp-policy                  │
                    │  (cluster-internal only in prod)                         │
                    └─────────────────────────────────────────────────────────┘
```

### 6.3 agent-orchestrator SSE Relay

The agent-orchestrator exposes SSE for real-time updates. In the architecture, agent-orchestrator is proxied through api-server (SSE relay). Thus:

- **External path:** `/api/agents/stream` or similar, routed by api-server to agent-orchestrator
- **Direct path:** `/agents/*` via Kong for agent-specific endpoints

Document which path is used: if api-server relays SSE, then `/agents/*` on Kong may only serve non-SSE endpoints. Adjust routes accordingly.

---

## 7. Kong Directory Structure

```
kong/
├── kong-plugins/
│   ├── jwt-auth.yaml
│   ├── rate-limiting.yaml
│   ├── rate-limiting-write.yaml
│   ├── request-transformer.yaml
│   ├── response-transformer.yaml
│   ├── correlation-id.yaml
│   ├── cors.yaml
│   ├── request-size-limit.yaml
│   ├── ip-restriction.yaml
│   └── internal-only.yaml
├── routes/
│   ├── ce-routes.yaml
│   └── ee-routes.yaml
└── consumers/
    └── service-accounts.yaml
```

### 7.1 Service Accounts — `kong/consumers/service-accounts.yaml`

For machine-to-machine auth (e.g. CI, internal services), create KongConsumers with key-auth or JWT credentials:

```yaml
# kong/consumers/service-accounts.yaml
apiVersion: configuration.konghq.com/v1
kind: KongConsumer
metadata:
  name: ci-service
  namespace: doki-system
username: ci-service
---
apiVersion: v1
kind: Secret
metadata:
  name: ci-service-key
  namespace: doki-system
  labels:
    konghq.com/credential: key-auth
stringData:
  key: "CHANGE_ME_GENERATE_SECURE_KEY"
---
# Reference the credential in KongConsumer (Kong 3.x uses KongConsumerGroup or inline)
# For key-auth, create KongConsumer with credential reference
```

---

## 8. Health Check and Monitoring

### 8.1 Kong Status Endpoint

Kong exposes a status endpoint for health checks:

- **Path:** `/status`
- **Port:** 8100 (status listener, internal)
- **Response:** JSON with `database` (or "off" for DB-less), `server` info

The Kong Helm chart configures a readiness probe. For external health checks, use the proxy endpoint (e.g. `GET /` with a known route).

### 8.2 Prometheus Plugin

Enable the Prometheus plugin for Kong metrics:

```yaml
# kong/kong-plugins/prometheus.yaml
apiVersion: configuration.konghq.com/v1
kind: KongPlugin
metadata:
  name: prometheus
  namespace: doki-system
plugin: prometheus
config:
  per_consumer: true
  status_code_metrics: true
  latency_metrics: true
```

Apply globally or to a specific Ingress. Metrics are exposed at `:8100/metrics` (status port) or a dedicated metrics port depending on Kong version.

### 8.3 Grafana Dashboard

Import the official Kong dashboard (ID: 7424) or create a custom dashboard with:

- `kong_http_requests_total` — request rate per route, status code
- `kong_http_request_duration_seconds` — latency percentiles
- `kong_http_status` — error rate per route

### 8.4 Scrape Configuration

Add to `helm-values/prometheus.yaml`:

```yaml
# Add to prometheus scrape config
- job_name: kong
  metrics_path: /metrics
  static_configs:
    - targets:
        - kong-admin.doki-system.svc.cluster.local:8100
  relabel_configs:
    - source_labels: [__address__]
      target_label: __address__
      replacement: kong-admin.doki-system.svc.cluster.local:8100
```

---

## 9. Implementation Order

| Step | Task | Phase |
|------|------|-------|
| 1 | Install Kong (DB-less mode) in doki-system | 1 |
| 2 | Apply KongPlugins: jwt-auth, rate-limiting | 1 |
| 3 | Apply CE routes (ce-routes.yaml) | 1 |
| 4 | Apply additional plugins: CORS, request-transformer, response-transformer, request-size-limit | 1 |
| 5 | Configure Auth0 issuer and test JWT flow end-to-end | 1 |
| 6 | Enable Prometheus plugin and Grafana dashboard | 1 |
| 7 | (Phase 3–4) Apply EE routes (ee-routes.yaml) | 3–4 |
| 8 | (Phase 3–4) Add IP restriction for prod | 4 |

### 9.1 Validation Commands

```bash
# Verify Kong is running
kubectl get pods -n doki-system -l app.kubernetes.io/name=kong

# Check KIC is reconciling
kubectl logs -n doki-system -l app.kubernetes.io/component=app --tail=50

# Test proxy (replace with your node IP/host)
curl -H "Host: api.doki.local" http://localhost:30080/api/health

# Test JWT (with valid Auth0 token)
curl -H "Host: api.doki.local" -H "Authorization: Bearer $TOKEN" http://localhost:30080/api/me
```

---

## 10. References

- [Kong DB-less and Declarative Configuration](https://docs.konghq.com/gateway/latest/production/deployment-topologies/db-less-and-declarative-config)
- [Kong Helm Chart](https://github.com/Kong/charts/tree/main/charts/kong)
- [Kong Ingress Controller](https://docs.konghq.com/kubernetes-ingress-controller/)
- [Kong JWT Plugin](https://docs.konghq.com/hub/kong-inc/jwt/)
- [Kong OpenID Connect Plugin](https://docs.konghq.com/hub/kong-inc/openid-connect/)
- [Kong Rate Limiting Plugin](https://docs.konghq.com/hub/kong-inc/rate-limiting/)
- [Auth0 Kong Integration](https://auth0.com/docs/integrations/integrating-auth0-with-kong)
