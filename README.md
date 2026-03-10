# Doki Stack Infrastructure

GitOps source of truth for the Doki Stack platform. Kubernetes manifests, Helm values, Kustomize overlays, Argo CD applications, and network policies for all services (CE and EE).

## Purpose

This repository is the single source of truth for all deployments. Argo CD watches this repo and syncs changes to the cluster. Every service, CE or EE, has its manifests defined here.

## Directory Structure

```
infrastructure/
├── base/                    # Base Kustomize manifests per service
│   ├── platform-ui/          # Next.js frontend
│   ├── api-server/           # Go API server
│   ├── mcp-scanner/          # MCP scanner
│   ├── mcp-execution/        # MCP execution
│   ├── mcp-policy/           # MCP policy
│   ├── agent-orchestrator/   # LangGraph orchestrator
│   ├── agent-automation/     # Automation agent
│   ├── agent-review/         # Review agent
│   ├── postgresql/           # PostgreSQL Helm chart ref
│   ├── minio/                # MinIO Helm chart ref
│   ├── qdrant/               # Qdrant Helm chart ref
│   ├── dragonfly/            # Dragonfly Helm chart ref
│   ├── rabbitmq/             # RabbitMQ
│   ├── ollama/               # Ollama (local/external modes)
│   ├── vllm/                # vLLM production LLM
│   ├── monitoring/          # Observability
│   └── ee/                   # Enterprise Edition services
├── overlays/                 # Environment overlays (dev, staging, prod)
├── helm-values/              # Values for third-party Helm charts
├── argocd/                   # Argo CD Application manifests
├── kong/                     # Kong routes and plugin configs
├── policies/                 # Kyverno, Cilium, Falco rules
├── cluster/                  # kind config, namespaces
├── cmd/dokictl/              # CLI entry point
├── internal/                 # CLI internal packages
└── docs/                     # Implementation plan
```

## Quick Start

```bash
# Build the CLI
make build

# Interactive setup wizard
./dokictl setup

# Or install everything with defaults
./dokictl setup --all
```

## Namespace Layout

| Namespace     | Contents                                                                 |
|---------------|---------------------------------------------------------------------------|
| doki-system   | Kong, Argo CD, ESO, cert-manager                                         |
| doki-data     | PostgreSQL, MinIO, Qdrant, Dragonfly, RabbitMQ, Vault                    |
| doki-mcp      | mcp-scanner, mcp-execution, mcp-policy, mcp-memory (EE), mcp-registry (EE) |
| doki-agents   | agent-orchestrator, agent-automation, agent-review, agent-discovery (EE), agent-rollback (EE) |
| doki-platform | api-server, platform-ui                                                 |
| doki-ee       | ee-license-server, ee-multi-tenancy, ee-notifications, ee-compliance, ee-governance, ee-dashboards |
| doki-monitoring | Prometheus, Grafana, Loki, Tempo                                      |
| doki-ai       | Ollama, vLLM                                                             |

## Helm Charts

| Chart       | Repo                                      | Version |
|-------------|-------------------------------------------|---------|
| PostgreSQL  | bitnami/postgresql                        | 15      |
| MinIO       | bitnami/minio                             | 14      |
| Qdrant      | qdrant/qdrant                             | 1.16    |
| Dragonfly   | oci://ghcr.io/dragonflydb/dragonfly/helm  | 1.29.0  |
| Kong        | kong/kong                                 | —       |
| Prometheus  | prometheus-community/kube-prometheus-stack | —       |
| Loki        | grafana/loki                              | —       |
| Tempo       | grafana/tempo                             | —       |
| Argo CD     | argo/argo-cd                              | —       |
| Harbor      | harbor/harbor                             | —       |

## Service Inventory

**CE (Community Edition):** platform-ui, api-server, mcp-scanner, mcp-execution, mcp-policy, agent-orchestrator, agent-automation, agent-review

**EE (Enterprise Edition):** mcp-memory, mcp-registry, ee-license-server, agent-discovery, agent-rollback, ee-multi-tenancy, ee-notifications, ee-compliance, ee-governance, ee-dashboards

## CLI Commands

```bash
dokictl setup              # Interactive setup wizard
dokictl setup --all        # Non-interactive, install everything
dokictl teardown           # Delete the kind cluster (with confirmation)
dokictl teardown --force   # Delete without confirmation
dokictl health             # Health check all services
dokictl test smoke         # Smoke tests (deployments, services, PVCs, routes)
dokictl test validate      # Validation tests (DNS, connectivity, data service CRUD)
```

Pre-commit: `pre-commit install && pre-commit run --all-files`

CI: GitHub Actions validate-infra workflow on PR to main modifying `base/`, `overlays/`, `helm-values/`, `policies/`, `kong/`, `argocd/`.

## Implementation Plan

Docs in `docs/implementation-plan/`:

- [00-overview](docs/implementation-plan/00-overview.md)
- [01-cluster-and-networking](docs/implementation-plan/01-cluster-and-networking.md)
- [02-data-services](docs/implementation-plan/02-data-services.md)
- [03-security](docs/implementation-plan/03-security.md)
- [04-observability](docs/implementation-plan/04-observability.md)
- [05-llm-infrastructure](docs/implementation-plan/05-llm-infrastructure.md)
- [06-cicd-and-gitops](docs/implementation-plan/06-cicd-and-gitops.md)
- [07-application-deployments](docs/implementation-plan/07-application-deployments.md)
- [08-kong-api-gateway](docs/implementation-plan/08-kong-api-gateway.md)
- [09-disaster-recovery](docs/implementation-plan/09-disaster-recovery.md)
- [10-environment-overlays](docs/implementation-plan/10-environment-overlays.md)
- [11-testing-and-validation](docs/implementation-plan/11-testing-and-validation.md)

## License

Apache License 2.0 — see [LICENSE](LICENSE)
