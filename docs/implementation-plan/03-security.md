# Implementation Plan — Security Infrastructure

This document covers all security infrastructure for the Doki Stack platform: HashiCorp Vault, External Secrets Operator, Kyverno admission policies, Falco runtime security, Cilium network policies, RBAC, and Pod Security Standards.

---

## 1. HashiCorp Vault

### 1.1 Overview

Vault is the central secrets management system. All secrets (credentials, API keys, license keys) are stored in Vault—never in environment variables, config files, LLM prompts, or logs. Vault provides transit encryption for sensitive fields (PII, credentials) and integrates with Kubernetes via the auth method.

### 1.2 Helm Chart and Namespace

| Setting | Value |
|---------|-------|
| Helm chart | `hashicorp/vault` |
| Namespace | `doki-data` (co-located with data services) |
| Repo | `helm repo add hashicorp https://helm.releases.hashicorp.com` |

### 1.3 Dev vs Prod Modes

| Mode | Use Case | Configuration |
|------|----------|---------------|
| **Dev** | Local development (kind) | Single unsealed instance, in-memory storage |
| **Prod** | Staging/Production | HA with Raft storage, auto-unseal via KMS |

### 1.4 Helm Values

Store at `helm-values/vault.yaml`:

```yaml
# helm-values/vault.yaml
# ---
# DEV MODE (local development)
# Use: helm install vault hashicorp/vault -f helm-values/vault.yaml --set server.dev.enabled=true
# ---
server:
  dev:
    enabled: true
    devRootToken: "root"  # Only for dev; never use in prod
  # ---
  # PROD MODE (uncomment for staging/prod)
  # ---
  # dev:
  #   enabled: false
  # ha:
  #   enabled: true
  #   raft:
  #     enabled: true
  #     config: |
  #       ui = true
  #       listener "tcp" {
  #         tls_disable = 1
  #         address = "[::]:8200"
  #         cluster_address = "[::]:8201"
  #       }
  #       storage "raft" {
  #         path = "/vault/data"
  #       }
  #       service_registration "kubernetes" {}
  #   replicas: 3
  #   resources:
  #     requests:
  #       memory: "256Mi"
  #       cpu: "250m"
  #     limits:
  #       memory: "512Mi"
  #       cpu: "500m"

# Common settings (both modes)
resources:
  requests:
    memory: "256Mi"
    cpu: "250m"
  limits:
    memory: "512Mi"
    cpu: "500m"

# Injector disabled for dev (optional); enable for prod to auto-inject secrets
injector:
  enabled: false  # Set true when using Vault Agent sidecar injection

# Service type
service:
  type: ClusterIP
```

**Prod overlay** (for `overlays/prod/` or separate values file):

```yaml
# helm-values/vault-prod.yaml (prod overlay)
server:
  dev:
    enabled: false
  ha:
    enabled: true
    raft:
      enabled: true
      config: |
        ui = true
        listener "tcp" {
          tls_disable = 1
          address = "[::]:8200"
          cluster_address = "[::]:8201"
        }
        storage "raft" {
          path = "/vault/data"
        }
        service_registration "kubernetes" {}
    replicas: 3
    resources:
      requests:
        memory: "256Mi"
        cpu: "250m"
      limits:
        memory: "512Mi"
        cpu: "500m"
```

### 1.5 Vault Paths

| Path | Purpose |
|------|---------|
| `secret/data/{dev,staging,prod}/{service}/*` | Service-specific secrets (DB credentials, API keys) |
| `secret/data/platform/license` | Enterprise Edition license key |
| `secret/data/orgs/{org_id}/cloud/{provider}` | Cloud credentials per org (EE multi-tenancy) |
| `transit/` | Encryption-as-a-service keys for PII, credentials |

**Path structure examples:**

```
secret/data/dev/postgresql/admin
secret/data/dev/minio/root
secret/data/dev/rabbitmq/admin
secret/data/dev/mcp-policy/api-key
secret/data/prod/platform/license
secret/data/orgs/org-123/cloud/aws
transit/keys/pii-encryption
transit/keys/credential-encryption
```

### 1.6 Kubernetes Auth Method

Enable Kubernetes auth and map service accounts to Vault policies:

```bash
# After Vault is running and unsealed
vault auth enable kubernetes

vault write auth/kubernetes/config \
  kubernetes_host="https://$KUBERNETES_PORT_443_TCP_ADDR:443" \
  token_reviewer_jwt="$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
  kubernetes_ca_cert=@/var/run/secrets/kubernetes.io/serviceaccount/ca.crt

# Create policy for read-only service access
vault policy write doki-service-read - <<EOF
path "secret/data/{{env}}/{{service}}/*" {
  capabilities = ["read", "list"]
}
path "transit/encrypt/pii-encryption" {
  capabilities = ["create", "update"]
}
path "transit/decrypt/pii-encryption" {
  capabilities = ["create", "update"]
}
EOF

# Create policy for admin/operators
vault policy write doki-admin - <<EOF
path "secret/data/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}
path "secret/metadata/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}
path "transit/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}
path "sys/*" {
  capabilities = ["read", "list"]
}
EOF

# Role for MCP services (example: mcp-policy in doki-mcp namespace)
vault write auth/kubernetes/role/mcp-policy \
  bound_service_account_names=mcp-policy \
  bound_service_account_namespaces=doki-mcp \
  policies=doki-service-read \
  ttl=1h
```

### 1.7 Policy Examples

**Read-only for services** (`doki-service-read`):

```hcl
path "secret/data/{{env}}/{{service}}/*" {
  capabilities = ["read", "list"]
}
path "transit/encrypt/pii-encryption" {
  capabilities = ["create", "update"]
}
path "transit/decrypt/pii-encryption" {
  capabilities = ["create", "update"]
}
```

**Admin for operators** (`doki-admin`):

```hcl
path "secret/data/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}
path "secret/metadata/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}
path "transit/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}
```

### 1.8 Transit Encryption and Log Redaction

- **Transit**: Use `transit/encrypt` and `transit/decrypt` for PII and credentials before storing in DB or logs.
- **Regex redaction**: Configure log redaction to mask secrets. Example patterns:
  - `(?i)(password|secret|token|api[_-]?key)=["']?[^"'\s]+`
  - `Bearer [A-Za-z0-9\-._~+/]+=*`
  - `-----BEGIN.*-----`

---

## 2. External Secrets Operator (ESO)

### 2.1 Overview

ESO syncs secrets from Vault into Kubernetes Secrets. Each namespace that needs secrets has a SecretStore pointing to Vault, and ExternalSecret CRs map Vault paths to K8s Secret keys.

### 2.2 Helm Chart and Namespace

| Setting | Value |
|---------|-------|
| Helm chart | `external-secrets/external-secrets` |
| Namespace | `doki-system` |
| Repo | `helm repo add external-secrets https://charts.external-secrets.io` |

### 2.3 Installation

```bash
helm repo add external-secrets https://charts.external-secrets.io
helm repo update

helm install external-secrets external-secrets/external-secrets \
  --namespace doki-system \
  --create-namespace \
  -f helm-values/external-secrets.yaml
```

### 2.4 Helm Values

```yaml
# helm-values/external-secrets.yaml
installCRDs: true

# Refresh interval: 1h dev, 5m prod (override via overlay)
replicaCount: 1

resources:
  requests:
    memory: "64Mi"
    cpu: "50m"
  limits:
    memory: "128Mi"
    cpu: "100m"
```

### 2.5 SecretStore Example

One SecretStore per namespace that needs Vault secrets. Uses Kubernetes auth (service account token).

```yaml
# policies/external-secrets/secretstore-doki-mcp.yaml
apiVersion: external-secrets.io/v1beta1
kind: SecretStore
metadata:
  name: vault
  namespace: doki-mcp
spec:
  provider:
    vault:
      server: "http://vault.doki-data.svc.cluster.local:8200"
      path: "secret"
      version: "v2"
      auth:
        kubernetes:
          mountPath: "kubernetes"
          role: "eso-doki-mcp"
          serviceAccountRef:
            name: external-secrets
            namespace: doki-system
---
# policies/external-secrets/secretstore-doki-agents.yaml
apiVersion: external-secrets.io/v1beta1
kind: SecretStore
metadata:
  name: vault
  namespace: doki-agents
spec:
  provider:
    vault:
      server: "http://vault.doki-data.svc.cluster.local:8200"
      path: "secret"
      version: "v2"
      auth:
        kubernetes:
          mountPath: "kubernetes"
          role: "eso-doki-agents"
          serviceAccountRef:
            name: external-secrets
            namespace: doki-system
```

**Vault role for ESO** (create in Vault):

```bash
vault write auth/kubernetes/role/eso-doki-mcp \
  bound_service_account_names=external-secrets \
  bound_service_account_namespaces=doki-system \
  policies=eso-read-doki-mcp \
  ttl=1h

vault policy write eso-read-doki-mcp - <<EOF
path "secret/data/dev/doki-mcp/*" {
  capabilities = ["read", "list"]
}
path "secret/data/staging/doki-mcp/*" {
  capabilities = ["read", "list"]
}
path "secret/data/prod/doki-mcp/*" {
  capabilities = ["read", "list"]
}
EOF
```

### 2.6 ExternalSecret Example

```yaml
# policies/external-secrets/externalsecret-mcp-policy.yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: mcp-policy-secrets
  namespace: doki-mcp
spec:
  refreshInterval: 1h  # Use 5m for prod overlay
  secretStoreRef:
    name: vault
    kind: SecretStore
  target:
    name: mcp-policy-secrets
    creationPolicy: Owner
  data:
    - secretKey: api-key
      remoteRef:
        key: dev/mcp-policy
        property: api-key
    - secretKey: database-url
      remoteRef:
        key: dev/postgresql
        property: connection-url
```

**Prod overlay** (`refreshInterval: 5m`):

```yaml
spec:
  refreshInterval: 5m
  data:
    - secretKey: api-key
      remoteRef:
        key: prod/mcp-policy
        property: api-key
```

---

## 3. Kyverno Admission Policies

### 3.1 Overview

Kyverno enforces security and consistency policies at admission time. Start in **audit mode** to validate policies without blocking, then switch to **enforce** after validation.

### 3.2 Helm Chart and Namespace

| Setting | Value |
|---------|-------|
| Helm chart | `kyverno/kyverno` |
| Namespace | `doki-system` |
| Repo | `helm repo add kyverno https://kyverno.github.io/kyverno/` |

### 3.3 Installation

```bash
helm repo add kyverno https://kyverno.github.io/kyverno/
helm repo update

helm install kyverno kyverno/kyverno \
  --namespace doki-system \
  --create-namespace \
  --set replicaCount=1 \
  --set validationFailureAction=Audit  # Start in audit; change to Enforce after validation
```

### 3.4 Policy 1: require-labels

All pods must have `app.kubernetes.io/name` and `app.kubernetes.io/part-of`.

```yaml
# policies/kyverno/require-labels.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-labels
  annotations:
    policies.kyverno.io/title: Require Standard Labels
    policies.kyverno.io/description: All pods must have app.kubernetes.io/name and app.kubernetes.io/part-of
spec:
  validationFailureAction: Audit  # Change to Enforce after validation
  background: true
  rules:
    - name: check-labels
      match:
        any:
          - resources:
              kinds:
                - Pod
              namespaces:
                - doki-data
                - doki-mcp
                - doki-agents
                - doki-platform
                - doki-ee
                - doki-system
                - monitoring
                - ai
      exclude:
        any:
          - resources:
              namespaces:
                - kube-system
                - kube-public
                - kube-node-lease
      validate:
        message: "Pods must have app.kubernetes.io/name and app.kubernetes.io/part-of labels"
        pattern:
          metadata:
            labels:
              app.kubernetes.io/name: "?*"
              app.kubernetes.io/part-of: "?*"
```

### 3.5 Policy 2: require-resource-limits

All containers must have CPU and memory limits.

```yaml
# policies/kyverno/require-resource-limits.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-resource-limits
  annotations:
    policies.kyverno.io/title: Require Resource Limits
    policies.kyverno.io/description: All containers must have CPU and memory limits
spec:
  validationFailureAction: Audit
  background: true
  rules:
    - name: check-resource-limits
      match:
        any:
          - resources:
              kinds:
                - Pod
              namespaces:
                - doki-data
                - doki-mcp
                - doki-agents
                - doki-platform
                - doki-ee
                - doki-system
                - monitoring
                - ai
      exclude:
        any:
          - resources:
              namespaces:
                - kube-system
      validate:
        message: "All containers must have CPU and memory limits"
        pattern:
          spec:
            containers:
              - resources:
                  limits:
                    memory: "?*"
                    cpu: "?*"
```

### 3.6 Policy 3: disallow-privileged

No privileged containers.

```yaml
# policies/kyverno/disallow-privileged.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: disallow-privileged
  annotations:
    policies.kyverno.io/title: Disallow Privileged Containers
    policies.kyverno.io/description: No privileged containers allowed
spec:
  validationFailureAction: Audit
  background: true
  rules:
    - name: check-privileged
      match:
        any:
          - resources:
              kinds:
                - Pod
              namespaces:
                - doki-data
                - doki-mcp
                - doki-agents
                - doki-platform
                - doki-ee
                - doki-system
                - monitoring
                - ai
      exclude:
        any:
          - resources:
              namespaces:
                - kube-system
      validate:
        message: "Privileged containers are not allowed"
        pattern:
          spec:
            containers:
              - securityContext:
                  privileged: false
```

### 3.7 Policy 4: restrict-registries

Only allow images from Harbor registry (`harbor.doki.local/`).

```yaml
# policies/kyverno/restrict-registries.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: restrict-registries
  annotations:
    policies.kyverno.io/title: Restrict Container Registries
    policies.kyverno.io/description: Only allow images from Harbor registry
spec:
  validationFailureAction: Audit
  background: true
  rules:
    - name: check-registry
      match:
        any:
          - resources:
              kinds:
                - Pod
              namespaces:
                - doki-data
                - doki-mcp
                - doki-agents
                - doki-platform
                - doki-ee
                - doki-system
      exclude:
        any:
          - resources:
              namespaces:
                - kube-system
                - monitoring
      validate:
        message: "Only images from harbor.doki.local/ are allowed"
        pattern:
          spec:
            containers:
              - image: "harbor.doki.local/*"
```

**Note:** For dev (kind without Harbor), use a more permissive pattern or disable this policy. Example dev override: allow `docker.io/*`, `ghcr.io/*` in addition to `harbor.doki.local/*`.

### 3.8 Policy 5: require-non-root

All containers must run as non-root.

```yaml
# policies/kyverno/require-non-root.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-non-root
  annotations:
    policies.kyverno.io/title: Require Non-Root
    policies.kyverno.io/description: All containers must run as non-root
spec:
  validationFailureAction: Audit
  background: true
  rules:
    - name: check-non-root
      match:
        any:
          - resources:
              kinds:
                - Pod
              namespaces:
                - doki-data
                - doki-mcp
                - doki-agents
                - doki-platform
                - doki-ee
                - doki-system
                - monitoring
                - ai
      exclude:
        any:
          - resources:
              namespaces:
                - kube-system
          - resources:
              names:
                - vault-*
              namespaces:
                - doki-data
      validate:
        message: "Containers must run as non-root (runAsNonRoot: true)"
        pattern:
          spec:
            securityContext:
              runAsNonRoot: true
            containers:
              - securityContext:
                  allowPrivilegeEscalation: false
```

### 3.9 Policy 6: require-read-only-rootfs

All containers must have readOnlyRootFilesystem.

```yaml
# policies/kyverno/require-read-only-rootfs.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-read-only-rootfs
  annotations:
    policies.kyverno.io/title: Require Read-Only Root Filesystem
    policies.kyverno.io/description: All containers must have readOnlyRootFilesystem
spec:
  validationFailureAction: Audit
  background: true
  rules:
    - name: check-read-only-rootfs
      match:
        any:
          - resources:
              kinds:
                - Pod
              namespaces:
                - doki-data
                - doki-mcp
                - doki-agents
                - doki-platform
                - doki-ee
                - doki-system
                - ai
      exclude:
        any:
          - resources:
              namespaces:
                - kube-system
                - monitoring
          - resources:
              names:
                - vault-*
              namespaces:
                - doki-data
      validate:
        message: "Containers must have readOnlyRootFilesystem: true"
        pattern:
          spec:
            containers:
              - securityContext:
                  readOnlyRootFilesystem: true
```

### 3.10 Policy 7: require-probes

All pods must have readiness and liveness probes.

```yaml
# policies/kyverno/require-probes.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-probes
  annotations:
    policies.kyverno.io/title: Require Health Probes
    policies.kyverno.io/description: All pods must have readiness and liveness probes
spec:
  validationFailureAction: Audit
  background: true
  rules:
    - name: check-probes
      match:
        any:
          - resources:
              kinds:
                - Pod
              namespaces:
                - doki-data
                - doki-mcp
                - doki-agents
                - doki-platform
                - doki-ee
                - doki-system
                - ai
      exclude:
        any:
          - resources:
              namespaces:
                - kube-system
                - monitoring
      validate:
        message: "All containers must have readinessProbe and livenessProbe"
        pattern:
          spec:
            containers:
              - livenessProbe: "?*"
                readinessProbe: "?*"
```

### 3.11 Exceptions Summary

| Policy | Exceptions |
|--------|------------|
| require-labels | kube-system, kube-public, kube-node-lease |
| require-resource-limits | kube-system |
| disallow-privileged | kube-system |
| restrict-registries | kube-system, monitoring |
| require-non-root | kube-system, vault-* in doki-data |
| require-read-only-rootfs | kube-system, monitoring, vault-* in doki-data |
| require-probes | kube-system, monitoring |

---

## 4. Falco Runtime Security

### 4.1 Overview

Falco detects runtime threats: shell access to containers, privilege escalation, sensitive file access, unexpected network connections. Output is sent to Loki for correlation with application logs.

### 4.2 Helm Chart and Namespace

| Setting | Value |
|---------|-------|
| Helm chart | `falcosecurity/falco` |
| Namespace | `doki-system` |
| Repo | `helm repo add falcosecurity https://falcosecurity.github.io/charts` |

### 4.3 Installation

```bash
helm repo add falcosecurity https://falcosecurity.github.io/charts
helm repo update

helm install falco falcosecurity/falco \
  --namespace doki-system \
  --create-namespace \
  -f helm-values/falco.yaml
```

### 4.4 Helm Values and Custom Rules

```yaml
# helm-values/falco.yaml
driver:
  kind: ebpf  # Use eBPF for modern kernels; fallback to module if needed

falco:
  jsonOutput: true
  jsonIncludeOutputProperty: true
  jsonIncludeTagsProperty: true

  # Output to stdout (captured by Loki/Fluent Bit)
  # Or configure fileOutput for Promtail to tail
  fileOutput:
    enabled: true
    keepAlive: false
    filename: /var/log/falco_events.json

  customRules:
    # Detect shell access to containers
    - rule: Terminal shell in container
      desc: A shell was used as the entrypoint/exec point into a container
      condition: >
        spawned_process and container and shell_procs and proc.tty != 0
        and container_entrypoint
      output: "Shell access in container (user=%user.name %container.info shell=%proc.name parent=%proc.pname cmdline=%proc.cmdline)"
      priority: WARNING
      tags: [container, shell, mitre_execution]

    # Detect privilege escalation
    - rule: Privilege escalation
      desc: Detect privilege escalation attempts
      condition: >
        spawned_process and container and proc.name in (sudo, su, doas)
      output: "Privilege escalation attempt (user=%user.name container=%container.info proc=%proc.cmdline)"
      priority: WARNING
      tags: [container, privilege_escalation, mitre_privilege_escalation]

    # Detect sensitive file access (Vault tokens, kubeconfig)
    - rule: Sensitive file access
      desc: Access to Vault tokens, kubeconfig, or other sensitive files
      condition: >
        open_read and container and
        (fd.name startswith /var/run/secrets/kubernetes.io/serviceaccount/token or
         fd.name contains /vault/token or
         fd.name contains kubeconfig or
         fd.name contains .kube/config)
      output: "Sensitive file access (user=%user.name file=%fd.name container=%container.info)"
      priority: CRITICAL
      tags: [container, sensitive_file, mitre_credential_access]

    # Detect unexpected outbound connections
    - rule: Unexpected outbound connection
      desc: Outbound connection to non-standard port
      condition: >
        outbound and container and
        not fd.sport in (80, 443, 5432, 5672, 6379, 6333, 9000, 9001, 8200, 9090, 3100)
      output: "Unexpected outbound connection (user=%user.name dest=%fd.rip port=%fd.sport container=%container.info)"
      priority: WARNING
      tags: [container, network, mitre_command_and_control]

    # Doki Stack: Alert on direct DB access bypassing MCPs
    - rule: Direct database connection from non-data namespace
      desc: Direct PostgreSQL/Redis connection from namespace that should use MCP
      condition: >
        outbound and container and
        (fd.rip in (postgresql.doki-data.svc.cluster.local, dragonfly.doki-data.svc.cluster.local) or
         fd.sport in (5432, 6379)) and
        k8s.ns.name in (doki-platform, doki-mcp) and
        not k8s.ns.name in (doki-data)
      output: "Direct DB access from %k8s.ns.name (expected via MCP) (dest=%fd.rip port=%fd.sport)"
      priority: WARNING
      tags: [doki-stack, policy_violation]
```

---

## 5. Cilium Network Policies

### 5.1 Overview

Default deny ingress/egress per namespace. Explicit allow rules define permitted traffic. Use `CiliumNetworkPolicy` (namespaced) or `CiliumClusterwideNetworkPolicy` (cluster-wide).

### 5.2 Default Deny

Apply default-deny policies per namespace (see `01-cluster-and-networking.md`). Below are the **allow** rules.

### 5.3 Allow Rules

#### 5.3.1 doki-mcp → doki-data

MCP servers need PostgreSQL, MinIO, Qdrant, Dragonfly, RabbitMQ, Vault.

```yaml
# policies/cilium/allow-doki-mcp-to-doki-data.yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-mcp-to-data
  namespace: doki-mcp
spec:
  endpointSelector: {}
  egress:
    - toEndpoints:
        - matchLabels:
            app.kubernetes.io/name: postgresql
      toPorts:
        - ports:
            - port: "5432"
              protocol: TCP
    - toEndpoints:
        - matchLabels:
            app.kubernetes.io/name: minio
      toPorts:
        - ports:
            - port: "9000"
              protocol: TCP
            - port: "9001"
              protocol: TCP
    - toEndpoints:
        - matchLabels:
            app.kubernetes.io/name: qdrant
      toPorts:
        - ports:
            - port: "6333"
              protocol: TCP
    - toEndpoints:
        - matchLabels:
            app.kubernetes.io/name: dragonfly
      toPorts:
        - ports:
            - port: "6379"
              protocol: TCP
    - toEndpoints:
        - matchLabels:
            app.kubernetes.io/name: rabbitmq
      toPorts:
        - ports:
            - port: "5672"
              protocol: TCP
            - port: "15672"
              protocol: TCP
    - toEndpoints:
        - matchLabels:
            app.kubernetes.io/name: vault
      toPorts:
        - ports:
            - port: "8200"
              protocol: TCP
    - toEndpoints:
        - {}
      toPorts:
        - ports:
            - port: "53"
              protocol: UDP
        - ports:
            - port: "53"
              protocol: TCP
```

#### 5.3.2 doki-agents → doki-data

Agents need PostgreSQL, RabbitMQ, MinIO, Vault.

```yaml
# policies/cilium/allow-doki-agents-to-doki-data.yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-agents-to-data
  namespace: doki-agents
spec:
  endpointSelector: {}
  egress:
    - toEndpoints:
        - matchLabels:
            app.kubernetes.io/name: postgresql
      toPorts:
        - ports:
            - port: "5432"
              protocol: TCP
    - toEndpoints:
        - matchLabels:
            app.kubernetes.io/name: rabbitmq
      toPorts:
        - ports:
            - port: "5672"
              protocol: TCP
    - toEndpoints:
        - matchLabels:
            app.kubernetes.io/name: minio
      toPorts:
        - ports:
            - port: "9000"
              protocol: TCP
    - toEndpoints:
        - matchLabels:
            app.kubernetes.io/name: vault
      toPorts:
        - ports:
            - port: "8200"
              protocol: TCP
    - toEndpoints:
        - {}
      toPorts:
        - ports:
            - port: "53"
              protocol: UDP
        - ports:
            - port: "53"
              protocol: TCP
```

#### 5.3.3 doki-agents → doki-mcp

Agents call all MCP servers.

```yaml
# policies/cilium/allow-doki-agents-to-doki-mcp.yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-agents-to-mcp
  namespace: doki-agents
spec:
  endpointSelector: {}
  egress:
    - toFQDNs:
        - matchName: "*.doki-mcp.svc.cluster.local"
      toPorts:
        - ports:
            - port: "8080"
              protocol: TCP
            - port: "3000"
              protocol: TCP
            - port: "50051"
              protocol: TCP
    - toEndpoints:
        - {}
      toPorts:
        - ports:
            - port: "53"
              protocol: UDP
        - ports:
            - port: "53"
              protocol: TCP
```

#### 5.3.4 doki-agents → ai

Agents call Ollama.

```yaml
# policies/cilium/allow-doki-agents-to-ai.yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-agents-to-ai
  namespace: doki-agents
spec:
  endpointSelector: {}
  egress:
    - toFQDNs:
        - matchName: "*.ai.svc.cluster.local"
      toPorts:
        - ports:
            - port: "11434"
              protocol: TCP
    - toEndpoints:
        - {}
      toPorts:
        - ports:
            - port: "53"
              protocol: UDP
        - ports:
            - port: "53"
              protocol: TCP
```

#### 5.3.5 doki-platform → doki-data

Platform (api-server) needs PostgreSQL, Dragonfly, RabbitMQ, Vault.

```yaml
# policies/cilium/allow-doki-platform-to-doki-data.yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-platform-to-data
  namespace: doki-platform
spec:
  endpointSelector: {}
  egress:
    - toEndpoints:
        - matchLabels:
            app.kubernetes.io/name: postgresql
      toPorts:
        - ports:
            - port: "5432"
              protocol: TCP
    - toEndpoints:
        - matchLabels:
            app.kubernetes.io/name: dragonfly
      toPorts:
        - ports:
            - port: "6379"
              protocol: TCP
    - toEndpoints:
        - matchLabels:
            app.kubernetes.io/name: rabbitmq
      toPorts:
        - ports:
            - port: "5672"
              protocol: TCP
    - toEndpoints:
        - matchLabels:
            app.kubernetes.io/name: vault
      toPorts:
        - ports:
            - port: "8200"
              protocol: TCP
    - toEndpoints:
        - {}
      toPorts:
        - ports:
            - port: "53"
              protocol: UDP
        - ports:
            - port: "53"
              protocol: TCP
```

#### 5.3.6 doki-platform → doki-agents

Platform calls agent-orchestrator.

```yaml
# policies/cilium/allow-doki-platform-to-doki-agents.yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-platform-to-agents
  namespace: doki-platform
spec:
  endpointSelector: {}
  egress:
    - toEndpoints:
        - matchLabels:
            app.kubernetes.io/name: agent-orchestrator
      toPorts:
        - ports:
            - port: "8000"
              protocol: TCP
    - toFQDNs:
        - matchName: "*.doki-agents.svc.cluster.local"
    - toEndpoints:
        - {}
      toPorts:
        - ports:
            - port: "53"
              protocol: UDP
        - ports:
            - port: "53"
              protocol: TCP
```

#### 5.3.7 doki-system (Kong) → doki-platform, doki-mcp

Kong routes to platform and MCP services.

```yaml
# policies/cilium/allow-doki-system-kong-egress.yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-kong-egress
  namespace: doki-system
spec:
  endpointSelector:
    matchLabels:
      app.kubernetes.io/name: kong
  egress:
    - toFQDNs:
        - matchName: "*.doki-platform.svc.cluster.local"
        - matchName: "*.doki-mcp.svc.cluster.local"
      toPorts:
        - ports:
            - port: "8080"
              protocol: TCP
            - port: "3000"
              protocol: TCP
            - port: "8000"
              protocol: TCP
    - toEndpoints:
        - {}
      toPorts:
        - ports:
            - port: "53"
              protocol: UDP
        - ports:
            - port: "53"
              protocol: TCP
```

#### 5.3.8 monitoring → all namespaces

Prometheus scrapes metrics from all namespaces.

```yaml
# policies/cilium/allow-monitoring-egress.yaml
apiVersion: cilium.io/v2
kind: CiliumClusterwideNetworkPolicy
metadata:
  name: allow-monitoring-egress
spec:
  endpointSelector:
    matchLabels:
      app.kubernetes.io/name: prometheus
  egress:
    - toEndpoints:
        - {}
      toPorts:
        - ports:
            - port: "9090"
              protocol: TCP
            - port: "8080"
              protocol: TCP
            - port: "3000"
              protocol: TCP
            - port: "9091"
              protocol: TCP
    - toEndpoints:
        - {}
      toPorts:
        - ports:
            - port: "53"
              protocol: UDP
        - ports:
            - port: "53"
              protocol: TCP
```

#### 5.3.9 Ingress: Traefik → Kong

```yaml
# policies/cilium/allow-traefik-to-kong.yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-traefik-to-kong
  namespace: doki-system
spec:
  endpointSelector:
    matchLabels:
      app.kubernetes.io/name: kong
  ingress:
    - fromEndpoints:
        - matchLabels:
            app.kubernetes.io/name: traefik
      toPorts:
        - ports:
            - port: "8000"
              protocol: TCP
            - port: "8443"
              protocol: TCP
```

#### 5.3.10 doki-data: Ingress from allowed namespaces

Data services must allow ingress from doki-mcp, doki-agents, and doki-platform. Use `app.kubernetes.io/part-of: doki-stack` to allow any Doki Stack pod. For stricter isolation, use namespace-specific selectors.

```yaml
# policies/cilium/allow-doki-data-ingress.yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-postgresql-ingress
  namespace: doki-data
spec:
  endpointSelector:
    matchLabels:
      app.kubernetes.io/name: postgresql
  ingress:
    - fromEndpoints:
        - matchLabels:
            app.kubernetes.io/part-of: doki-stack
      toPorts:
        - ports:
            - port: "5432"
              protocol: TCP
---
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-minio-ingress
  namespace: doki-data
spec:
  endpointSelector:
    matchLabels:
      app.kubernetes.io/name: minio
  ingress:
    - fromEndpoints:
        - matchLabels:
            app.kubernetes.io/part-of: doki-stack
      toPorts:
        - ports:
            - port: "9000"
              protocol: TCP
            - port: "9001"
              protocol: TCP
---
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-qdrant-ingress
  namespace: doki-data
spec:
  endpointSelector:
    matchLabels:
      app.kubernetes.io/name: qdrant
  ingress:
    - fromEndpoints:
        - matchLabels:
            app.kubernetes.io/part-of: doki-stack
      toPorts:
        - ports:
            - port: "6333"
              protocol: TCP
---
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-dragonfly-ingress
  namespace: doki-data
spec:
  endpointSelector:
    matchLabels:
      app.kubernetes.io/name: dragonfly
  ingress:
    - fromEndpoints:
        - matchLabels:
            app.kubernetes.io/part-of: doki-stack
      toPorts:
        - ports:
            - port: "6379"
              protocol: TCP
---
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-rabbitmq-ingress
  namespace: doki-data
spec:
  endpointSelector:
    matchLabels:
      app.kubernetes.io/name: rabbitmq
  ingress:
    - fromEndpoints:
        - matchLabels:
            app.kubernetes.io/part-of: doki-stack
      toPorts:
        - ports:
            - port: "5672"
              protocol: TCP
            - port: "15672"
              protocol: TCP
---
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-vault-ingress
  namespace: doki-data
spec:
  endpointSelector:
    matchLabels:
      app.kubernetes.io/name: vault
  ingress:
    - fromEndpoints:
        - matchLabels:
            app.kubernetes.io/part-of: doki-stack
      toPorts:
        - ports:
            - port: "8200"
              protocol: TCP
```

### 5.4 Deny: doki-platform → doki-data (selective)

If policy requires platform to never access PostgreSQL directly (must go through MCP), add an explicit deny rule. Cilium supports deny semantics. Consult [Cilium Network Policy](https://docs.cilium.io/en/stable/network/concepts/policy/deny/) for exact syntax. The allow rules above already restrict traffic; the explicit deny is only needed if you want to block platform → PostgreSQL entirely.

---

## 6. RBAC

### 6.1 ClusterRoles

| ClusterRole | Purpose | Permissions |
|-------------|---------|-------------|
| `doki-admin` | Full platform access | `*` on all resources |
| `doki-operator` | Deploy, restart, scale | Deployments, StatefulSets, Pods (create, update, delete, get, list), ConfigMaps, Secrets (get, list) |
| `doki-viewer` | Read-only | Get, list, watch on most resources; no create/update/delete |

```yaml
# policies/rbac/clusterroles.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: doki-admin
  labels:
    app.kubernetes.io/part-of: doki-stack
rules:
  - apiGroups: ["*"]
    resources: ["*"]
    verbs: ["*"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: doki-operator
  labels:
    app.kubernetes.io/part-of: doki-stack
rules:
  - apiGroups: [""]
    resources: ["pods", "pods/log", "pods/exec", "services", "configmaps", "secrets"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["apps"]
    resources: ["deployments", "statefulsets", "replicasets"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: doki-viewer
  labels:
    app.kubernetes.io/part-of: doki-stack
rules:
  - apiGroups: ["*"]
    resources: ["*"]
    verbs: ["get", "list", "watch"]
```

### 6.2 RoleBindings

Bind ClusterRoles to users/groups per namespace. Example for `doki-platform`:

```yaml
# policies/rbac/rolebinding-doki-platform.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: doki-operator-binding
  namespace: doki-platform
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: doki-operator
subjects:
  - kind: User
    name: platform-operator
    apiGroup: rbac.authorization.k8s.io
  - kind: Group
    name: doki-operators
    apiGroup: rbac.authorization.k8s.io
```

### 6.3 ServiceAccounts

One ServiceAccount per Deployment. Example:

```yaml
# base/platform-ui/serviceaccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: platform-ui
  namespace: doki-platform
  labels:
    app.kubernetes.io/name: platform-ui
    app.kubernetes.io/part-of: doki-stack
```

### 6.4 Vault Integration

Each service's ServiceAccount is mapped to a Vault Kubernetes auth role. The role grants the policy that allows reading that service's secrets. Example:

```bash
vault write auth/kubernetes/role/platform-ui \
  bound_service_account_names=platform-ui \
  bound_service_account_namespaces=doki-platform \
  policies=doki-service-read \
  ttl=1h
```

---

## 7. Pod Security Standards

### 7.1 Overview

Apply Pod Security Admission labels per namespace. Levels: `privileged`, `baseline`, `restricted`.

### 7.2 Namespace Labels

```yaml
# policies/pod-security/namespace-labels.yaml
# Apply to each namespace via overlay or patch
---
apiVersion: v1
kind: Namespace
metadata:
  name: doki-data
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
---
apiVersion: v1
kind: Namespace
metadata:
  name: doki-mcp
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
---
apiVersion: v1
kind: Namespace
metadata:
  name: doki-agents
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
---
apiVersion: v1
kind: Namespace
metadata:
  name: doki-platform
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
---
apiVersion: v1
kind: Namespace
metadata:
  name: doki-system
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
---
apiVersion: v1
kind: Namespace
metadata:
  name: doki-ee
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
---
apiVersion: v1
kind: Namespace
metadata:
  name: monitoring
  labels:
    pod-security.kubernetes.io/enforce: baseline
    pod-security.kubernetes.io/audit: baseline
    pod-security.kubernetes.io/warn: restricted
---
apiVersion: v1
kind: Namespace
metadata:
  name: ai
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
```

### 7.3 Vault Exception

Vault in `doki-data` may need privileges (e.g., for Raft storage, auto-unseal). Use a namespace-level exception or run Vault in a separate namespace with `baseline`. Kyverno policies exclude `vault-*` pods in `doki-data` for require-non-root and require-read-only-rootfs.

---

## 8. Implementation Order

1. **Install Vault (dev mode)** — Install Helm chart with `server.dev.enabled=true`
2. **Configure Vault paths and policies** — Create paths, transit keys, policies, Kubernetes auth roles
3. **Install ESO** — Install External Secrets Operator in `doki-system`
4. **Create SecretStores per namespace** — SecretStore in doki-mcp, doki-agents, doki-platform, etc.
5. **Install Kyverno (audit mode)** — Install with `validationFailureAction=Audit`
6. **Apply Kyverno policies** — Apply all 7 policies in audit mode, validate no unexpected violations
7. **Install Falco** — Install with eBPF driver, configure custom rules
8. **Apply Cilium network policies** — Apply default-deny (if not already), apply allow rules, test connectivity
9. **Switch Kyverno to enforce mode** — After validation, set `validationFailureAction: Enforce` on all policies
10. **Apply RBAC and Pod Security** — Apply ClusterRoles and RoleBindings, apply Pod Security labels to namespaces

---

## 9. File Reference

| File | Purpose |
|------|---------|
| `helm-values/vault.yaml` | Vault Helm values (dev/prod) |
| `helm-values/external-secrets.yaml` | ESO Helm values |
| `helm-values/falco.yaml` | Falco Helm values |
| `policies/external-secrets/secretstore-*.yaml` | SecretStore CRs |
| `policies/external-secrets/externalsecret-*.yaml` | ExternalSecret CRs |
| `policies/kyverno/*.yaml` | Kyverno ClusterPolicies |
| `policies/cilium/allow-*.yaml` | Cilium allow rules |
| `policies/cilium/default-deny-namespaces.yaml` | Default deny (see 01-cluster-and-networking.md) |
| `policies/rbac/clusterroles.yaml` | ClusterRoles |
| `policies/rbac/rolebinding-*.yaml` | RoleBindings |
| `policies/pod-security/namespace-labels.yaml` | Pod Security labels |
