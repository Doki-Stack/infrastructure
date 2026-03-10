#!/bin/bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

FAILED=0
CONTEXT="${1:-}"

pass() { echo -e "${GREEN}PASS${NC} $1"; }
fail() { echo -e "${RED}FAIL${NC} $1"; ((FAILED++)) || true; }
skip() { echo -e "${YELLOW}SKIP${NC} $1"; }

if [[ -n "$CONTEXT" ]]; then
  export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
  kubectl config use-context "$CONTEXT" 2>/dev/null || true
fi

echo "=== Doki Stack Health Check ==="

if ! kubectl cluster-info &>/dev/null; then
  fail "Kubernetes cluster accessible"
  exit 1
fi
pass "Kubernetes cluster accessible"

REQUIRED_NS="doki-system doki-data doki-mcp doki-agents doki-platform doki-ee monitoring ai"
MISSING_NS=""
for ns in $REQUIRED_NS; do
  if ! kubectl get namespace "$ns" &>/dev/null; then
    MISSING_NS="$MISSING_NS $ns"
  fi
done
if [[ -n "$MISSING_NS" ]]; then
  fail "Namespaces missing:$MISSING_NS"
else
  pass "All namespaces exist"
fi

BAD_PODS=""
for ns in $REQUIRED_NS; do
  if kubectl get namespace "$ns" &>/dev/null; then
    BAD_PODS="$BAD_PODS $(kubectl get pods -n "$ns" --no-headers 2>/dev/null | grep -v -E 'Running|Completed|Succeeded' | awk -v ns="$ns" '{print ns"/"$1}' || true)"
  fi
done
BAD_PODS=$(echo "$BAD_PODS" | tr -s ' ' | xargs)
if [[ -n "$BAD_PODS" ]]; then
  fail "Pods not ready: $BAD_PODS"
else
  pass "All pods Running/Completed"
fi

PG_HOST="${PG_HOST:-postgres-postgresql.doki-data.svc.cluster.local}"
PG_PORT="${PG_PORT:-5432}"
if kubectl run "pg-check-$$" --rm --restart=Never --image=postgres:16-alpine -n doki-data -- pg_isready -h "$PG_HOST" -p "$PG_PORT" -U postgres 2>/dev/null; then
  pass "PostgreSQL connectivity"
else
  if command -v pg_isready &>/dev/null && [[ -n "${PG_NODEPORT:-}" ]]; then
    if pg_isready -h "${KUBE_HOST:-localhost}" -p "$PG_NODEPORT" -U postgres 2>/dev/null; then
      pass "PostgreSQL connectivity (host)"
    else
      fail "PostgreSQL connectivity"
    fi
  else
    fail "PostgreSQL connectivity"
  fi
fi

MINIO_URL="${MINIO_URL:-http://minio.doki-data.svc.cluster.local:9000}"
if kubectl run "minio-check-$$" --rm --restart=Never --image=curlimages/curl:latest -n doki-data -- curl -sf "${MINIO_URL}/minio/health/live" -o /dev/null 2>/dev/null; then
  pass "MinIO connectivity"
else
  fail "MinIO connectivity"
fi

RABBITMQ_URL="${RABBITMQ_URL:-http://rabbitmq.doki-data.svc.cluster.local:15672}"
RABBITMQ_USER="${RABBITMQ_USER:-doki}"
RABBITMQ_PASS="${RABBITMQ_PASS:-changeme-in-vault}"
if kubectl run "rabbitmq-check-$$" --rm --restart=Never --image=curlimages/curl:latest -n doki-data -- curl -sf -u "${RABBITMQ_USER}:${RABBITMQ_PASS}" "${RABBITMQ_URL}/api/overview" -o /dev/null 2>/dev/null; then
  pass "RabbitMQ management API"
else
  fail "RabbitMQ management API"
fi

QDRANT_URL="${QDRANT_URL:-http://qdrant.doki-data.svc.cluster.local:6333}"
if kubectl run "qdrant-check-$$" --rm --restart=Never --image=curlimages/curl:latest -n doki-data -- curl -sf "${QDRANT_URL}/collections" -o /dev/null 2>/dev/null; then
  pass "Qdrant REST API"
else
  fail "Qdrant REST API"
fi

DRAGONFLY_HOST="${DRAGONFLY_HOST:-dragonfly.doki-data.svc.cluster.local}"
DRAGONFLY_PORT="${DRAGONFLY_PORT:-6379}"
if kubectl run "dragonfly-check-$$" --rm --restart=Never --image=redis:7-alpine -n doki-data -- redis-cli -h "$DRAGONFLY_HOST" -p "$DRAGONFLY_PORT" PING 2>/dev/null | grep -q PONG; then
  pass "Dragonfly PING"
else
  fail "Dragonfly PING"
fi

OLLAMA_URL="${OLLAMA_URL:-http://ollama.ai.svc.cluster.local:11434}"
if kubectl run "ollama-check-$$" --rm --restart=Never --image=curlimages/curl:latest -n ai -- curl -sf "${OLLAMA_URL}/api/tags" -o /dev/null 2>/dev/null; then
  pass "Ollama"
else
  skip "Ollama (optional for dev)"
fi

GRAFANA_URL="${GRAFANA_URL:-http://monitoring-grafana.monitoring.svc.cluster.local:80}"
if kubectl run "grafana-check-$$" --rm --restart=Never --image=curlimages/curl:latest -n monitoring -- curl -sf -o /dev/null -w '%{http_code}' "${GRAFANA_URL}/api/health" 2>/dev/null | grep -q 200; then
  pass "Grafana readiness"
else
  fail "Grafana readiness"
fi

VAULT_ADDR="${VAULT_ADDR:-http://vault.doki-data.svc.cluster.local:8200}"
VAULT_STATUS=$(kubectl run "vault-check-$$" --rm --restart=Never --image=curlimages/curl:latest -n doki-data -- curl -sf "${VAULT_ADDR}/v1/sys/seal-status" 2>/dev/null | grep -o '"sealed":[^,]*' || echo "error")
if echo "$VAULT_STATUS" | grep -q '"sealed":false'; then
  pass "Vault unsealed"
elif echo "$VAULT_STATUS" | grep -q '"sealed":true'; then
  fail "Vault sealed"
else
  fail "Vault status"
fi

KONG_ADMIN="${KONG_ADMIN:-http://kong-kong-admin.doki-system.svc.cluster.local:8001}"
if kubectl run "kong-check-$$" --rm --restart=Never --image=curlimages/curl:latest -n doki-system -- curl -sf "${KONG_ADMIN}/status" -o /dev/null 2>/dev/null; then
  pass "Kong admin API"
else
  fail "Kong admin API"
fi

echo "=== Summary ==="
if [[ $FAILED -eq 0 ]]; then
  echo -e "${GREEN}All checks passed.${NC}"
  exit 0
else
  echo -e "${RED}$FAILED check(s) failed.${NC}"
  exit 1
fi
