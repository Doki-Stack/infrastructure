#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info() { echo -e "${GREEN}[INFO]${NC} $1"; }
err()  { echo -e "${RED}[ERROR]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }

OLLAMA_MODE="${OLLAMA_MODE:-local}"
OLLAMA_MODELS="${OLLAMA_MODELS:-qwen2.5-coder:14b nomic-embed-text}"

usage() {
  cat <<EOF
Usage: $0 [--mode local|external] [--models "model1 model2"]

Options:
  --mode       local (default) — run Ollama as a pod inside the cluster
               external        — point to Ollama running on the host machine
  --models     Space-separated list of models to pull (default: qwen2.5-coder:14b nomic-embed-text)

Environment variables:
  OLLAMA_MODE    Same as --mode
  OLLAMA_MODELS  Same as --models
EOF
  exit 0
}

while [[ $# -gt 0 ]]; do
  case $1 in
    --mode)     OLLAMA_MODE="$2"; shift 2 ;;
    --models)   OLLAMA_MODELS="$2"; shift 2 ;;
    --help|-h)  usage ;;
    *)          err "Unknown option: $1"; usage ;;
  esac
done

if [[ "$OLLAMA_MODE" != "local" && "$OLLAMA_MODE" != "external" ]]; then
  err "Invalid mode: $OLLAMA_MODE (must be 'local' or 'external')"
  exit 1
fi

setup_local() {
  info "Deploying Ollama inside the cluster (local mode)..."

  # Point kustomization to local resources
  cat > "$ROOT/base/ollama/kustomization.yaml" <<'KUSTOM'
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ./local
KUSTOM

  kubectl apply -k "$ROOT/base/ollama"

  info "Waiting for Ollama pod to be ready..."
  kubectl wait --for=condition=ready pod -l app=ollama -n doki-ai --timeout=300s

  info "Pulling models..."
  for model in $OLLAMA_MODELS; do
    info "  Pulling $model..."
    kubectl exec -n doki-ai ollama-0 -- ollama pull "$model"
  done

  info "Ollama is running locally inside the cluster"
}

setup_external() {
  info "Configuring Ollama endpoint to host machine (external mode)..."

  # Point kustomization to external resources
  cat > "$ROOT/base/ollama/kustomization.yaml" <<'KUSTOM'
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ./external
KUSTOM

  # Detect host IP
  HOST_IP=""
  if command -v docker &>/dev/null; then
    HOST_IP=$(docker network inspect kind -f '{{range .IPAM.Config}}{{.Gateway}}{{end}}' 2>/dev/null || true)
  fi
  if [ -z "$HOST_IP" ]; then
    HOST_IP=$(ip route | grep default | awk '{print $3}' | head -1)
  fi
  if [ -z "$HOST_IP" ]; then
    err "Could not detect host IP"
    exit 1
  fi

  info "Detected host IP: $HOST_IP"

  kubectl apply -k "$ROOT/base/ollama"

  kubectl patch endpoints ollama -n doki-ai --type=merge \
    -p "{\"subsets\":[{\"addresses\":[{\"ip\":\"$HOST_IP\"}],\"ports\":[{\"port\":11434,\"protocol\":\"TCP\"}]}]}"

  info "Verifying connectivity to host Ollama..."
  if kubectl run curl-ollama-verify --rm -i --restart=Never \
    --image=curlimages/curl -n doki-ai -- \
    curl -sf http://ollama.doki-ai.svc.cluster.local:11434/api/tags 2>/dev/null; then
    info "Ollama endpoint configured and reachable"
  else
    warn "Ollama endpoint configured but host Ollama may not be running"
    warn "Ensure Ollama is running on the host with OLLAMA_HOST=0.0.0.0:11434"
  fi
}

info "Ollama setup mode: $OLLAMA_MODE"

case "$OLLAMA_MODE" in
  local)    setup_local ;;
  external) setup_external ;;
esac
