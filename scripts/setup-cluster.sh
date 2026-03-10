#!/bin/bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

info() { echo -e "${GREEN}[INFO]${NC} $1"; }
err() { echo -e "${RED}[ERROR]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }

create_kind_cluster() {
  info "Creating kind cluster..."
  if kind get clusters 2>/dev/null | grep -q doki-stack; then
    warn "Cluster doki-stack already exists"
    return 0
  fi
  kind create cluster --name doki-stack --config "$ROOT/cluster/kind-config.yaml"
  info "Kind cluster created"
}

install_cilium() {
  info "Installing Cilium..."
  helm repo add cilium https://helm.cilium.io 2>/dev/null || true
  helm repo update cilium
  helm upgrade --install cilium cilium/cilium --namespace kube-system --wait
  info "Cilium installed"
}

wait_cilium_ready() {
  info "Waiting for Cilium to be ready..."
  kubectl wait --for=condition=Ready pods -l k8s-app=cilium -n kube-system --timeout=300s
  kubectl wait --for=condition=Ready nodes --all --timeout=300s
  info "Cilium ready"
}

apply_namespaces() {
  info "Applying namespaces..."
  kubectl apply -f "$ROOT/cluster/namespaces.yaml"
  info "Namespaces applied"
}

install_data_services() {
  info "Installing data services..."
  helm repo add bitnami https://charts.bitnami.com/bitnami 2>/dev/null || true
  helm repo add qdrant https://qdrant.github.io/qdrant-helm 2>/dev/null || true
  helm repo update

  info "Installing PostgreSQL..."
  helm upgrade --install postgres bitnami/postgresql -n doki-data -f "$ROOT/base/postgresql/values.yaml" --version 15 --wait

  info "Installing MinIO..."
  helm upgrade --install minio bitnami/minio -n doki-data -f "$ROOT/base/minio/values.yaml" --version 14 --wait

  info "Installing Qdrant..."
  helm upgrade --install qdrant qdrant/qdrant -n doki-data -f "$ROOT/base/qdrant/values.yaml" --version 1.16 --wait

  info "Installing Dragonfly..."
  helm upgrade --install dragonfly oci://ghcr.io/dragonflydb/dragonfly/helm -n doki-data -f "$ROOT/base/dragonfly/values.yaml" --version 1.29.0 --wait

  info "Installing RabbitMQ..."
  kubectl apply -k "$ROOT/base/rabbitmq"
  kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=rabbitmq -n doki-data --timeout=300s

  info "Data services installed"
}

install_vault() {
  info "Installing Vault (dev mode)..."
  helm repo add hashicorp https://helm.releases.hashicorp.com 2>/dev/null || true
  helm repo update hashicorp
  helm upgrade --install vault hashicorp/vault -n doki-data -f "$ROOT/helm-values/vault.yaml" --wait
  info "Vault installed"
}

install_monitoring() {
  info "Installing monitoring stack..."
  helm repo add prometheus-community https://prometheus-community.github.io/helm-charts 2>/dev/null || true
  helm repo add grafana https://grafana.github.io/helm-charts 2>/dev/null || true
  helm repo update

  info "Installing kube-prometheus-stack..."
  helm upgrade --install monitoring prometheus-community/kube-prometheus-stack -n doki-monitoring -f "$ROOT/helm-values/prometheus.yaml" --create-namespace --wait

  info "Installing Loki..."
  helm upgrade --install loki grafana/loki -n doki-monitoring -f "$ROOT/helm-values/loki.yaml" --wait

  info "Installing Tempo..."
  helm upgrade --install tempo grafana/tempo -n doki-monitoring -f "$ROOT/helm-values/tempo.yaml" --wait

  info "Monitoring stack installed"
}

setup_ollama() {
  info "Setting up Ollama endpoint..."
  kubectl apply -k "$ROOT/base/ollama"
  "$ROOT/scripts/setup-ollama-endpoint.sh" || warn "Ollama setup may require host Ollama running"
  info "Ollama endpoint configured"
}

run_health_check() {
  info "Running health check..."
  "$ROOT/scripts/health-check.sh" || true
}

main() {
  info "Starting Doki Stack cluster setup..."
  create_kind_cluster
  install_cilium
  wait_cilium_ready
  apply_namespaces
  install_data_services
  install_vault
  install_monitoring
  setup_ollama
  run_health_check
  info "Cluster setup complete"
}

main "$@"
