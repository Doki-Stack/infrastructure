# Implementation Plan — Cluster and Networking

This document covers the Kubernetes cluster setup and networking layer for the Doki Stack platform. It defines the kind cluster configuration, namespaces, CNI (Cilium), ingress (Traefik), certificate management (cert-manager), DNS, and network topology.

---

## 1. Kind Cluster Configuration

### 1.1 kind-config.yaml

The development cluster uses a single control-plane node with extra port mappings for external access to services. Store this file at `cluster/kind-config.yaml`.

```yaml
# cluster/kind-config.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4

# Disable default CNI so Cilium can be installed (nodes stay NotReady until Cilium is up)
networking:
  disableDefaultCNI: true

nodes:
  - role: control-plane
    # Extra port mappings for NodePort services
    extraPortMappings:
      - containerPort: 30080
        hostPort: 30080
        protocol: TCP
        # HTTP (Traefik)
      - containerPort: 30443
        hostPort: 30443
        protocol: TCP
        # HTTPS (Traefik)
      - containerPort: 30300
        hostPort: 30300
        protocol: TCP
        # Grafana
      - containerPort: 30672
        hostPort: 30672
        protocol: TCP
        # RabbitMQ AMQP
      - containerPort: 31672
        hostPort: 31672
        protocol: TCP
        # RabbitMQ Management UI
      - containerPort: 30901
        hostPort: 30901
        protocol: TCP
        # MinIO Console
    # Mount host Docker socket for Ollama (model access, container runtime)
    extraMounts:
      - hostPath: /var/run/docker.sock
        containerPath: /var/run/docker.sock
    # Labels for node selection (Traefik, Ollama)
    kubeadmConfigPatches:
      - |
        kind: InitConfiguration
        nodeRegistration:
          kubeletExtraArgs:
            node-labels: "ingress-ready=true,doki.io/ollama-ready=true"
```

**Note:** The `ingress-ready=true` label is used by Traefik's node selector to schedule on the control-plane. The `doki.io/ollama-ready=true` label can be used by the Ollama deployment to schedule where the Docker socket is available.

### 1.2 Cluster Creation Command

```bash
kind create cluster --name doki-stack --config cluster/kind-config.yaml
```

### 1.3 Resource Requirements

| Scenario | RAM | Disk | Notes |
|----------|-----|------|-------|
| **Minimum** | 8 GB | 20 GB | Core services only; no Ollama |
| **Recommended** | 12 GB | 30 GB | With Prometheus, Grafana, Loki, Tempo |
| **With Ollama 14B** | +12 GB | +10 GB | Add 12 GB RAM for Qwen 2.5 14B model |

Ensure Docker has sufficient resources allocated (Docker Desktop → Settings → Resources, or `docker info` for limits).

---

## 2. Namespace Creation

### 2.1 namespaces.yaml

Create all namespaces at `cluster/namespaces.yaml`:

```yaml
# cluster/namespaces.yaml
---
apiVersion: v1
kind: Namespace
metadata:
  name: doki-system
  labels:
    app.kubernetes.io/part-of: doki-stack
    doki.io/environment: dev
  annotations:
    doki.io/purpose: "Kong, Argo CD, ESO, cert-manager — platform control plane"
---
apiVersion: v1
kind: Namespace
metadata:
  name: doki-data
  labels:
    app.kubernetes.io/part-of: doki-stack
    doki.io/environment: dev
  annotations:
    doki.io/purpose: "PostgreSQL, MinIO, Qdrant, Dragonfly, RabbitMQ, Vault — data layer"
---
apiVersion: v1
kind: Namespace
metadata:
  name: doki-mcp
  labels:
    app.kubernetes.io/part-of: doki-stack
    doki.io/environment: dev
  annotations:
    doki.io/purpose: "MCP servers — scanner, execution, policy, memory (EE), registry (EE)"
---
apiVersion: v1
kind: Namespace
metadata:
  name: doki-agents
  labels:
    app.kubernetes.io/part-of: doki-stack
    doki.io/environment: dev
  annotations:
    doki.io/purpose: "LangGraph agents — orchestrator, automation, review, discovery (EE), rollback (EE)"
---
apiVersion: v1
kind: Namespace
metadata:
  name: doki-platform
  labels:
    app.kubernetes.io/part-of: doki-stack
    doki.io/environment: dev
  annotations:
    doki.io/purpose: "API server, platform UI — user-facing services"
---
apiVersion: v1
kind: Namespace
metadata:
  name: doki-ee
  labels:
    app.kubernetes.io/part-of: doki-stack
    doki.io/environment: dev
  annotations:
    doki.io/purpose: "Enterprise Edition — license server, multi-tenancy, notifications, compliance, governance, dashboards"
---
apiVersion: v1
kind: Namespace
metadata:
  name: monitoring
  labels:
    app.kubernetes.io/part-of: doki-stack
    doki.io/environment: dev
  annotations:
    doki.io/purpose: "Prometheus, Grafana, Loki, Tempo — observability stack"
---
apiVersion: v1
kind: Namespace
metadata:
  name: ai
  labels:
    app.kubernetes.io/part-of: doki-stack
    doki.io/environment: dev
  annotations:
    doki.io/purpose: "Ollama — LLM inference endpoints"
```

### 2.2 Apply Namespaces

```bash
kubectl apply -f cluster/namespaces.yaml
```

---

## 3. Cilium CNI

Cilium provides the CNI, eBPF-based networking, and replaces kube-proxy. Hubble enables network flow visibility.

### 3.1 Installation

Add the Helm repository and install:

```bash
helm repo add cilium https://helm.cilium.io/
helm repo update

helm install cilium cilium/cilium \
  --version 1.16.0 \
  --namespace kube-system \
  --set kubeProxyReplacement=true \
  --set hubble.enabled=true \
  --set hubble.relay.enabled=true \
  --set hubble.ui.enabled=true \
  --set hubble.relay.service.type=ClusterIP \
  --set hubble.ui.service.type=ClusterIP
```

**Version:** Use `1.16.0` or later stable. Check [Cilium releases](https://github.com/cilium/cilium/releases) for the latest.

**Note:** Cilium replaces kube-proxy. The kind config uses `disableDefaultCNI: true`, so nodes remain `NotReady` until Cilium is installed. Install Cilium immediately after cluster creation.

### 3.2 Hubble UI Access

Hubble UI is available via port-forward during development:

```bash
kubectl port-forward -n kube-system svc/hubble-ui 12000:80
```

Access at `http://localhost:12000` for network flow visualization.

### 3.3 Default Deny NetworkPolicy

Apply a default-deny policy per namespace so that only explicitly allowed traffic is permitted. Store at `policies/cilium/default-deny-namespaces.yaml`:

```yaml
# policies/cilium/default-deny-namespaces.yaml
# Default deny ingress — apply to each namespace
# Use kustomize or a script to generate per-namespace
---
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: default-deny-ingress
  namespace: doki-system
spec:
  endpointSelector: {}
  ingress: []
---
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: default-deny-ingress
  namespace: doki-data
spec:
  endpointSelector: {}
  ingress: []
---
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: default-deny-ingress
  namespace: doki-mcp
spec:
  endpointSelector: {}
  ingress: []
---
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: default-deny-ingress
  namespace: doki-agents
spec:
  endpointSelector: {}
  ingress: []
---
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: default-deny-ingress
  namespace: doki-platform
spec:
  endpointSelector: {}
  ingress: []
---
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: default-deny-ingress
  namespace: doki-ee
spec:
  endpointSelector: {}
  ingress: []
---
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: default-deny-ingress
  namespace: monitoring
spec:
  endpointSelector: {}
  ingress: []
---
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: default-deny-ingress
  namespace: ai
spec:
  endpointSelector: {}
  ingress: []
```

**Note:** After applying default-deny, you must add explicit `CiliumNetworkPolicy` or `CiliumClusterwideNetworkPolicy` rules to allow required traffic (e.g., Traefik → Kong, Kong → services, Prometheus scraping). Those allow rules are defined in the Policies document (08).

---

## 4. Traefik Ingress Controller

Traefik serves as the ingress controller, terminating TLS and routing to Kong (API gateway).

### 4.1 Installation

```bash
helm repo add traefik https://traefik.github.io/charts
helm repo update

helm install traefik traefik/traefik \
  --namespace doki-system \
  --create-namespace \
  --set service.type=NodePort \
  --set ports.web.nodePort=30080 \
  --set ports.websecure.nodePort=30443 \
  --set deployment.kind=Deployment \
  --set providers.kubernetesIngress=true \
  --set providers.kubernetesCRD=true \
  --set ingressClass.enabled=true \
  --set ingressClass.isDefaultClass=true
```

### 4.2 Helm Values File (Recommended)

For reproducibility, use a values file at `helm-values/traefik.yaml`:

```yaml
# helm-values/traefik.yaml
service:
  type: NodePort

ports:
  web:
    nodePort: 30080
    expose:
      default: true
  websecure:
    nodePort: 30443
    expose:
      default: true

deployment:
  kind: Deployment

providers:
  kubernetesIngress: true
  kubernetesCRD: true

ingressClass:
  enabled: true
  isDefaultClass: true

# Node selector for kind (control-plane has ingress-ready label)
nodeSelector:
  ingress-ready: "true"

tolerations:
  - key: node-role.kubernetes.io/control-plane
    operator: Exists
    effect: NoSchedule
```

Install with:

```bash
helm install traefik traefik/traefik \
  --namespace doki-system \
  -f helm-values/traefik.yaml
```

### 4.3 IngressRoute CRDs

Traefik supports IngressRoute CRDs for advanced routing. Example for Kong:

```yaml
# kong/ingressroute.yaml (example)
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: kong-http
  namespace: doki-system
spec:
  entryPoints:
    - web
  routes:
    - match: Host(`api.doki.local`)
      kind: Rule
      services:
        - name: kong-proxy
          port: 8000
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: kong-https
  namespace: doki-system
spec:
  entryPoints:
    - websecure
  routes:
    - match: Host(`api.doki.local`)
      kind: Rule
      services:
        - name: kong-proxy
          port: 8443
```

### 4.4 Middleware

Traefik Middleware for rate limiting, headers, and compression:

```yaml
# kong/middleware.yaml (example)
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: rate-limit
  namespace: doki-system
spec:
  rateLimit:
    average: 100
    burst: 50
---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: secure-headers
  namespace: doki-system
spec:
  headers:
    frameDeny: true
    sslRedirect: true
    stsIncludeSubdomains: true
    stsPreload: true
---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: compress
  namespace: doki-system
spec:
  compress: {}
```

### 4.5 TLS Configuration

TLS is configured via IngressRoute with cert-manager-issued certificates. See Section 5 for ClusterIssuer setup.

---

## 5. cert-manager

cert-manager automates certificate issuance and renewal.

### 5.1 Installation

```bash
helm repo add jetstack https://charts.jetstack.io
helm repo update

helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --set installCRDs=true
```

### 5.2 ClusterIssuer Examples

**Self-signed (development):**

```yaml
# cluster/issuers/selfsigned-issuer.yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: selfsigned-issuer
spec:
  selfSigned: {}
```

**Let's Encrypt Staging (testing, higher rate limits):**

```yaml
# cluster/issuers/letsencrypt-staging.yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-staging
spec:
  acme:
    server: https://acme-staging-v02.api.letsencrypt.org/directory
    email: admin@doki-stack.local  # Replace with real email
    privateKeySecretRef:
      name: letsencrypt-staging-account-key
    solvers:
      - http01:
          ingress:
            class: traefik
```

**Let's Encrypt Production:**

```yaml
# cluster/issuers/letsencrypt-prod.yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@doki-stack.local  # Replace with real email
    privateKeySecretRef:
      name: letsencrypt-prod-account-key
    solvers:
      - http01:
          ingress:
            class: traefik
```

**Usage in Certificate resource:**

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: api-tls
  namespace: doki-system
spec:
  secretName: api-tls-secret
  issuerRef:
    name: selfsigned-issuer      # Use letsencrypt-prod in production
    kind: ClusterIssuer
  dnsNames:
    - api.doki.local
```

---

## 6. DNS and Service Discovery

### 6.1 CoreDNS

kind ships with CoreDNS. No additional configuration is required for cluster-internal DNS.

### 6.2 Service Naming Convention

Services are resolvable as:

```
{service}.{namespace}.svc.cluster.local
```

### 6.3 Key Internal DNS Entries (Data Services)

| Service | DNS Name | Port |
|---------|----------|------|
| PostgreSQL | `postgresql.doki-data.svc.cluster.local` | 5432 |
| MinIO API | `minio.doki-data.svc.cluster.local` | 9000 |
| MinIO Console | `minio-console.doki-data.svc.cluster.local` | 9001 |
| Qdrant | `qdrant.doki-data.svc.cluster.local` | 6333 |
| Dragonfly | `dragonfly.doki-data.svc.cluster.local` | 6379 |
| RabbitMQ AMQP | `rabbitmq.doki-data.svc.cluster.local` | 5672 |
| RabbitMQ Management | `rabbitmq.doki-data.svc.cluster.local` | 15672 |
| Vault | `vault.doki-data.svc.cluster.local` | 8200 |

Applications should use these FQDNs for service-to-service communication.

---

## 7. Port Mapping Summary

| Host Port | Container Port | Purpose |
|-----------|----------------|---------|
| 30080 | 30080 | HTTP (Traefik) |
| 30443 | 30443 | HTTPS (Traefik) |
| 30300 | 30300 | Grafana |
| 30672 | 30672 | RabbitMQ AMQP |
| 31672 | 31672 | RabbitMQ Management UI |
| 30901 | 30901 | MinIO Console |

**Note:** PostgreSQL, Qdrant, Dragonfly, MinIO API, and Vault have no external port mappings. They are only accessible from within the cluster (ClusterIP). Use `kubectl port-forward` for temporary local access during development.

---

## 8. Network Topology

### 8.1 Flow Diagram

```
                    ┌─────────────────────────────────────────────────────────┐
                    │                     External (Host)                      │
                    └─────────────────────────────────────────────────────────┘
                                              │
                    ┌─────────────────────────▼───────────────────────────────┐
                    │  kind NodePort (30080 HTTP, 30443 HTTPS)                 │
                    └─────────────────────────────────────────────────────────┘
                                              │
                    ┌─────────────────────────▼───────────────────────────────┐
                    │  Traefik (doki-system)                                  │
                    │  - TLS termination                                       │
                    │  - IngressRoute / Ingress                                │
                    └─────────────────────────────────────────────────────────┘
                                              │
                    ┌─────────────────────────▼───────────────────────────────┐
                    │  Kong (doki-system)                                      │
                    │  - API gateway, rate limiting, auth                       │
                    └─────────────────────────────────────────────────────────┘
                                              │
        ┌─────────────────────────────────────┼─────────────────────────────────────┐
        │                                     │                                     │
        ▼                                     ▼                                     ▼
┌───────────────┐                   ┌───────────────┐                   ┌───────────────┐
│ doki-platform │                   │   doki-mcp    │                   │ doki-agents   │
│ api-server    │                   │ mcp-scanner   │                   │ orchestrator  │
│ platform-ui   │                   │ mcp-execution │                   │ automation    │
└───────────────┘                   │ mcp-policy    │                   │ review        │
        │                            └───────────────┘                   └───────────────┘
        │                                     │                                     │
        └─────────────────────────────────────┼─────────────────────────────────────┘
                                              │
                    ┌─────────────────────────▼───────────────────────────────┐
                    │  doki-data (ClusterIP only)                               │
                    │  PostgreSQL, MinIO, Qdrant, Dragonfly, RabbitMQ, Vault    │
                    └─────────────────────────────────────────────────────────┘
```

### 8.2 Internal Service-to-Service Communication

- All internal traffic uses **ClusterIP** services.
- MCP servers, agents, and platform services communicate via `{service}.{namespace}.svc.cluster.local`.
- Data services (PostgreSQL, MinIO, Qdrant, Dragonfly, RabbitMQ, Vault) are **not** exposed to the host except via `kubectl port-forward` or dedicated management UIs (RabbitMQ Management, MinIO Console) with explicit NodePort mappings.

### 8.3 Security Boundaries

- **Default deny:** CiliumNetworkPolicy enforces default-deny ingress per namespace.
- **Explicit allow:** Only Traefik → Kong, Kong → services, Prometheus → scraping targets, and other necessary flows are explicitly allowed via Cilium policies.
- **org_id scoping:** Application-level tenant isolation; data services do not enforce org_id at the network layer — that is handled by the application and Policy MCP.

---

## 9. Implementation Order

Execute in this order to avoid dependency issues:

| Step | Action | Command / Notes |
|------|--------|-----------------|
| 1 | Create kind cluster | `kind create cluster --name doki-stack --config cluster/kind-config.yaml` |
| 2 | Install Cilium | `helm install cilium ...` (see Section 3.1) |
| 3 | Wait for Cilium nodes to be ready | `kubectl -n kube-system get pods -l k8s-app=cilium` |
| 4 | Create namespaces | `kubectl apply -f cluster/namespaces.yaml` |
| 5 | Install cert-manager | `helm install cert-manager ...` (see Section 5.1) |
| 6 | Apply ClusterIssuers | `kubectl apply -f cluster/issuers/` |
| 7 | Install Traefik | `helm install traefik ...` (see Section 4.1) |
| 8 | Apply default deny network policies | `kubectl apply -f policies/cilium/default-deny-namespaces.yaml` |

**Note:** After step 8, you must add explicit allow policies before deploying applications, or services will not receive traffic. The Policies document (08) defines the allow rules.

---

## References

- [kind Configuration](https://kind.sigs.k8s.io/docs/user/configuration/)
- [Cilium Helm Install](https://docs.cilium.io/en/stable/installation/k8s-install-helm/)
- [Traefik Helm Chart](https://github.com/traefik/traefik-helm-chart)
- [cert-manager ACME](https://cert-manager.io/docs/configuration/acme/)
- [00-overview.md](./00-overview.md) — Repository structure, phases, principles
