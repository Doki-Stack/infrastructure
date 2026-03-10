# infrastructure

GitOps source of truth for the Doki Stack platform. Contains Kubernetes manifests, Helm values, Kustomize overlays, Argo CD applications, and network policies for all services (CE and EE).

## Purpose

This repository is the single source of truth for all deployments. Argo CD watches this repo and automatically syncs changes to the Kubernetes cluster. Every service, whether CE or EE, has its manifests defined here.

## Technology Stack

| Component | Technology |
|-----------|-----------|
| Orchestration | Kubernetes (kind) |
| Networking | Cilium CNI, Traefik Ingress |
| GitOps | Argo CD |
| Templating | Kustomize, Helm (third-party charts) |
| API Gateway | Kong |
| Security | Kyverno policies, Cilium network policies |
| Certificates | cert-manager (Let's Encrypt) |
| Secrets | HashiCorp Vault + External Secrets Operator |
| Observability | Prometheus, Grafana, Loki, Tempo |

## Structure

```
infrastructure/
├── base/                    # Base Kustomize manifests per service
├── overlays/                # Environment overlays (dev, staging, prod)
├── helm-values/             # Values files for third-party Helm charts
├── argocd/                  # Argo CD Application manifests
├── kong/                    # Kong routes and plugin configs
├── policies/                # Kyverno, Cilium, Falco rules
├── scripts/                 # Setup and health check scripts
├── tests/                   # Integration and E2E tests
└── README.md
```

## Implementation Phase

**Phase 0** (Weeks 1-6) — Foundation. Continuously updated as new services are added.

## License

Apache License 2.0 — see [LICENSE](LICENSE)
