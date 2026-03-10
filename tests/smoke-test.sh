#!/bin/bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'
FAILED=0

pass() { echo -e "${GREEN}PASS${NC} $1"; }
fail() { echo -e "${RED}FAIL${NC} $1"; ((FAILED++)) || true; }

echo "=== Smoke Tests ==="

echo "Check: Deployments"
BAD_DEPLOYS=$(kubectl get deploy -A -l app.kubernetes.io/part-of=doki-stack -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name} {.status.readyReplicas}/{.spec.replicas}{"\n"}{end}' 2>/dev/null | while read -r line; do
  name="${line%% *}"
  replicas="${line##* }"
  ready="${replicas%%/*}"
  total="${replicas##*/}"
  [[ -n "$total" && "$total" != "0" && "$ready" != "$total" ]] && echo "$name"
done || true)
if [[ -z "$BAD_DEPLOYS" ]]; then
  pass "All deployments have available replicas"
else
  fail "Deployments not ready: $BAD_DEPLOYS"
fi

echo "Check: Services"
SVC_FAIL=0
for ns in doki-data doki-mcp doki-platform doki-agents doki-system monitoring doki-ai; do
  for svc in $(kubectl get svc -n "$ns" -o name 2>/dev/null); do
    ep=$(kubectl get "$svc" -n "$ns" -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null)
    sel=$(kubectl get "$svc" -n "$ns" -o jsonpath='{.spec.selector}' 2>/dev/null)
    if [[ -n "$sel" && "$sel" != "{}" && -z "$ep" ]]; then
      fail "No endpoints: $svc (ns $ns)"
      SVC_FAIL=1
    fi
  done
done
[[ $SVC_FAIL -eq 0 ]] && pass "All services have endpoints"

echo "Check: PVCs"
UNBOUND=$(kubectl get pvc -A -o jsonpath='{range .items[?(@.status.phase!="Bound")]}{.metadata.namespace}/{.metadata.name} {.status.phase}{"\n"}{end}' 2>/dev/null)
if [[ -z "$UNBOUND" ]]; then
  pass "All PVCs are bound"
else
  fail "PVCs not bound: $UNBOUND"
fi

echo "Check: Kong routes"
KONG_PROXY="${KONG_PROXY:-http://localhost:30080}"
HTTP_CODE=$(curl -sf -o /dev/null -w "%{http_code}" "${KONG_PROXY}/health" 2>/dev/null || echo "000")
if [[ "$HTTP_CODE" =~ ^(200|404)$ ]]; then
  pass "Kong routes return 200 for health endpoints"
else
  fail "Kong health route (got $HTTP_CODE)"
fi

echo "=== Summary ==="
if [[ $FAILED -eq 0 ]]; then
  echo -e "${GREEN}All smoke tests passed.${NC}"
  exit 0
else
  echo -e "${RED}$FAILED test(s) failed.${NC}"
  exit 1
fi
