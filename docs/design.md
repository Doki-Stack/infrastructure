# infrastructure — High-Level Design

## Overview

The infrastructure repository is the GitOps source of truth for all Doki Stack deployments. Argo CD watches this repository and reconciles the cluster state to match the desired state defined here.

## Architecture

```
infrastructure/
├── base/                        # Base manifests (Kustomize)
│   ├── mcp-scanner/             # Deployment, Service, ConfigMap
│   ├── mcp-execution/
│   ├── mcp-policy/
│   ├── agent-orchestrator/
│   ├── agent-automation/
│   ├── agent-review/
│   ├── api-server/
│   ├── platform-ui/
│   └── ee/                      # EE service manifests
│       ├── mcp-memory/
│       ├── mcp-registry/
│       ├── ee-license-server/
│       └── ...
├── overlays/
│   ├── dev/                     # Dev-specific patches (replicas, resources)
│   ├── staging/
│   └── prod/
├── helm-values/
│   ├── postgresql.yaml
│   ├── minio.yaml
│   ├── qdrant.yaml
│   ├── dragonfly.yaml
│   ├── rabbitmq.yaml
│   ├── vault.yaml
│   ├── prometheus.yaml
│   ├── grafana.yaml
│   ├── loki.yaml
│   └── tempo.yaml
├── argocd/
│   ├── app-of-apps.yaml         # Root Application
│   └── applications/            # Individual Argo CD Applications
├── kong/
│   ├── routes.yaml
│   └── plugins.yaml
├── policies/
│   ├── kyverno/
│   ├── cilium/
│   └── falco/
├── scripts/
│   ├── health-check.sh
│   └── setup.sh
└── tests/
    ├── integration/
    └── e2e/
```

## Deployment Flow

```
Developer pushes code → CI builds Docker image → CI pushes to Harbor
→ CI updates image tag in this repo → Argo CD detects change → Argo CD syncs to cluster
```

## Namespace Layout

| Namespace | Services |
|-----------|---------|
| `platform` | api-server, platform-ui |
| `mcp` | mcp-scanner, mcp-execution, mcp-policy |
| `agents` | agent-orchestrator, agent-automation, agent-review |
| `data` | PostgreSQL, MinIO, Qdrant, Dragonfly, RabbitMQ |
| `security` | Vault, Kyverno, Falco |
| `monitoring` | Prometheus, Grafana, Loki, Tempo |
| `ai` | Ollama endpoint |
| `ee` | All EE services (when deployed) |
