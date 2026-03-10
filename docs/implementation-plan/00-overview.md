# Infrastructure Implementation Plan — Overview

## 1. Purpose

The `infrastructure/` repository is the **GitOps source of truth** for the Doki Stack platform. Argo CD watches this repository and reconciles cluster state to match the desired manifests defined here. All deployments flow from this single source: developers push changes to this repo, Argo CD detects drift, and syncs the cluster accordingly.

This repository uses:
- **Kustomize** for application manifests (base definitions and environment-specific overlays)
- **Helm** for third-party charts (PostgreSQL, MinIO, RabbitMQ, Qdrant, Dragonfly, Vault, Prometheus, Grafana, Loki, Tempo)

---

## 2. Repository Structure

```
infrastructure/
├── base/                    # Base Kustomize manifests per service
│   ├── platform-ui/         # Next.js frontend
│   ├── api-server/          # Go API server
│   ├── mcp-scanner/         # Rust scanner MCP
│   ├── mcp-execution/       # Rust execution MCP
│   ├── mcp-policy/          # Go policy MCP
│   ├── agent-orchestrator/  # Python LangGraph orchestrator
│   ├── agent-automation/    # Python automation agent
│   ├── agent-review/        # Python review agent
│   └── ee/                  # Enterprise Edition services
│       ├── mcp-memory/
│       ├── mcp-registry/
│       ├── ee-license-server/
│       ├── agent-discovery/
│       ├── agent-rollback/
│       ├── ee-multi-tenancy/
│       ├── ee-notifications/
│       ├── ee-compliance/
│       ├── ee-governance/
│       └── ee-dashboards/
├── overlays/                # Environment-specific overrides
│   ├── dev/
│   ├── staging/
│   └── prod/
├── helm-values/             # Values for third-party Helm charts
│   ├── postgresql.yaml
│   ├── minio.yaml
│   ├── rabbitmq.yaml
│   ├── qdrant.yaml
│   ├── dragonfly.yaml
│   ├── vault.yaml
│   ├── prometheus.yaml
│   ├── grafana.yaml
│   ├── loki.yaml
│   ├── tempo.yaml
│   └── kong.yaml
├── argocd/                  # Argo CD Application CRs
├── kong/                    # Kong routes and plugin configs
├── policies/                # Kyverno, Cilium, Falco rules
├── scripts/                 # Setup, health check, utility scripts
├── cluster/                 # Cluster-level configs (kind, namespaces)
│   ├── kind-config.yaml
│   └── namespaces.yaml
├── tests/                   # Integration and validation tests
└── docs/                    # Design and implementation plan
```

---

## 3. Namespace Layout

| Namespace   | Contents                                                                 | Phase |
|-------------|---------------------------------------------------------------------------|-------|
| doki-system | Kong, Argo CD, ESO, cert-manager                                          | 0–1   |
| doki-data   | PostgreSQL, MinIO, Qdrant, Dragonfly, RabbitMQ, Vault                      | 0     |
| doki-mcp    | mcp-scanner, mcp-execution, mcp-policy, mcp-memory (EE), mcp-registry (EE) | 1, 3–4 |
| doki-agents | agent-orchestrator, agent-automation, agent-review, agent-discovery (EE), agent-rollback (EE) | 2–3 |
| doki-platform | api-server, platform-ui                                                 | 1     |
| doki-ee     | ee-license-server, ee-multi-tenancy, ee-notifications, ee-compliance, ee-governance, ee-dashboards | 3–4 |
| monitoring  | Prometheus, Grafana, Loki, Tempo                                          | 0     |
| ai          | Ollama Service/Endpoints                                                  | 0     |

---

## 4. Phase Mapping

| Phase | Scope |
|-------|-------|
| **Phase 0** | `cluster/`, `helm-values/`, `scripts/`, `policies/` (basic), `argocd/` (bootstrap) |
| **Phase 1** | `base/` (CE MCP + platform services), `kong/`, `overlays/dev/` |
| **Phase 2** | `base/` (agents), `overlays/` updates |
| **Phase 3** | `base/ee/` (Phase 3 EE services), `policies/` (hardened) |
| **Phase 4** | `base/ee/` (Phase 4 EE services), `overlays/staging/`, `overlays/prod/` |

---

## 5. Tooling Decisions

| Tool     | Purpose                                      | ADR  |
|----------|----------------------------------------------|------|
| Kustomize | Application manifests (base + overlays)     | —    |
| Helm     | Third-party charts only                      | —    |
| Argo CD  | GitOps sync                                  | —    |
| kind     | All environments (ADR-001 context)           | —    |
| Cilium   | CNI + network policies                       | —    |
| Traefik  | Ingress controller                           | —    |
| Kong     | API gateway                                  | —    |

---

## 6. Document Index

| #   | Document              | Description |
|-----|------------------------|-------------|
| 00  | Overview               | Master overview, purpose, structure, phases, principles |
| 01  | Phase 0 — Bootstrap    | Cluster setup, helm-values, scripts, basic policies, Argo CD bootstrap |
| 02  | Phase 1 — Base Services| CE MCP and platform base manifests, Kong, dev overlay |
| 03  | Phase 2 — Agents       | Agent base manifests, overlay updates |
| 04  | Observability          | Prometheus, Grafana, Loki, Tempo, alerting, structured logging |
| 05  | Phase 3 — EE Services  | Phase 3 EE base manifests, hardened policies |
| 06  | CI/CD and GitOps       | Harbor, Argo CD, GitHub Actions, GitOps workflow |
| 07  | Argo CD Setup          | Application-of-apps, sync waves, project structure (see also 06) |
| 08  | Kong Configuration     | Routes, plugins, upstream definitions |
| 09  | Policies               | Kyverno, Cilium, Falco rules and constraints |
| 10  | Cluster Config         | kind config, namespaces, bootstrap manifests |
| 11  | Helm Values            | Per-chart values, environment overrides |
| 12  | Testing                | Integration tests, validation, health checks |

---

## 7. Key Principles

- **org_id everywhere** — Every table, API call, log entry, cache key, bucket path, and Qdrant query is scoped by `org_id`. No cross-tenant data access.

- **HITL mandatory** — All infrastructure changes require explicit human approval. No auto-approve for low risk.

- **Fail closed** — If Policy MCP, Qdrant, or embeddings are unavailable, the system blocks. Never proceed without policy context.

- **No Bitnami for RabbitMQ** — Bitnami images are paywalled. Use official Docker images for RabbitMQ.

- **Secrets in Vault** — Never in env vars, config files, LLM prompts, or logs. Use Vault transit encryption and regex redaction.

- **Operational requirements** — All services must have health checks, resource limits, and pod disruption budgets.
