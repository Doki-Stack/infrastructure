#!/bin/bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'
FAILED=0

pass() { echo -e "${GREEN}PASS${NC} $1"; }
fail() { echo -e "${RED}FAIL${NC} $1"; ((FAILED++)) || true; }

echo "=== Infrastructure Validation ==="

echo "Test 1: DNS resolution"
SERVICES="postgres-postgresql.doki-data.svc.cluster.local minio.doki-data.svc.cluster.local rabbitmq.doki-data.svc.cluster.local qdrant.doki-data.svc.cluster.local dragonfly.doki-data.svc.cluster.local vault.doki-data.svc.cluster.local"
for svc in $SERVICES; do
  if kubectl run dns-test-$$ --rm -i --restart=Never --image=busybox:1.36 -n doki-mcp -- nslookup "$svc" 2>/dev/null | grep -q "Address"; then
    pass "  $svc"
  else
    fail "  $svc"
  fi
done

echo "Test 2: Cross-namespace connectivity"
if kubectl run net-test-$$ --rm -i --restart=Never --image=curlimages/curl:latest -n doki-mcp -- sh -c "curl -sf http://qdrant.doki-data.svc.cluster.local:6333/collections -o /dev/null" 2>/dev/null; then
  pass "  doki-mcp -> qdrant.doki-data"
else
  fail "  doki-mcp -> qdrant.doki-data"
fi

echo "Test 3: MinIO bucket access"
BUCKET="${MINIO_TEST_BUCKET:-scanner-artifacts}"
TEST_OBJ="test/validate-$(date +%s).txt"
if kubectl run minio-test-$$ --rm -i --restart=Never --image=minio/mc:latest -n doki-data -- sh -c "
  mc alias set local http://minio.doki-data.svc.cluster.local:9000 \${MINIO_ROOT_USER:-minioadmin} \${MINIO_ROOT_PASSWORD:-minioadmin} && \
  mc mb local/${BUCKET} 2>/dev/null || true && \
  echo 'test' | mc pipe local/${BUCKET}/${TEST_OBJ} && \
  mc cat local/${BUCKET}/${TEST_OBJ} | grep -q test && \
  mc rm local/${BUCKET}/${TEST_OBJ}
" 2>/dev/null; then
  pass "  MinIO create/read/delete"
else
  fail "  MinIO create/read/delete"
fi

echo "Test 4: RabbitMQ publish/consume"
RABBITMQ_URL="http://rabbitmq.doki-data.svc.cluster.local:15672"
RABBITMQ_AUTH="${RABBITMQ_USER:-doki}:${RABBITMQ_PASS:-CHANGE_ME_RABBITMQ_PASS}"
QUEUE="test-validate-$(date +%s)"
if kubectl run rabbitmq-test-$$ --rm -i --restart=Never --image=curlimages/curl:latest -n doki-data -- sh -c "
  curl -sf -u '${RABBITMQ_AUTH}' -X PUT -H 'Content-Type: application/json' -d '{}' '${RABBITMQ_URL}/api/queues/%2F/${QUEUE}' && \
  curl -sf -u '${RABBITMQ_AUTH}' -X POST -H 'Content-Type: application/json' -d '{\"properties\":{},\"routing_key\":\"${QUEUE}\",\"payload\":\"validate-test\",\"payload_encoding\":\"string\"}' '${RABBITMQ_URL}/api/exchanges/%2F/amq.default/publish' && \
  curl -sf -u '${RABBITMQ_AUTH}' -X POST -H 'Content-Type: application/json' -d '{\"count\":1,\"ackmode\":\"ack_requeue_false\"}' '${RABBITMQ_URL}/api/queues/%2F/${QUEUE}/get' | grep -q 'validate-test' && \
  curl -sf -u '${RABBITMQ_AUTH}' -X DELETE '${RABBITMQ_URL}/api/queues/%2F/${QUEUE}'
" 2>/dev/null; then
  pass "  RabbitMQ publish/consume"
else
  fail "  RabbitMQ publish/consume"
fi

echo "Test 5: Qdrant collection check"
if kubectl run qdrant-test-$$ --rm -i --restart=Never --image=curlimages/curl:latest -n doki-data -- sh -c 'curl -sf http://qdrant.doki-data.svc.cluster.local:6333/collections | grep -q collections' 2>/dev/null; then
  pass "  Qdrant collections API"
else
  fail "  Qdrant collections API"
fi

echo "Test 6: Dragonfly SET/GET/DEL"
KEY="validate:test:$(date +%s)"
if kubectl run dragonfly-test-$$ --rm -i --restart=Never --image=redis:7-alpine -n doki-data -- sh -c "
  redis-cli -h dragonfly.doki-data.svc.cluster.local -p 6379 SET ${KEY} ok && \
  redis-cli -h dragonfly.doki-data.svc.cluster.local -p 6379 GET ${KEY} | grep -q ok && \
  redis-cli -h dragonfly.doki-data.svc.cluster.local -p 6379 DEL ${KEY}
" 2>/dev/null; then
  pass "  Dragonfly SET/GET/DEL"
else
  fail "  Dragonfly SET/GET/DEL"
fi

echo "Test 7: Vault read test"
if kubectl run vault-test-$$ --rm -i --restart=Never --image=curlimages/curl:latest -n doki-data -- curl -sf -H "X-Vault-Token: ${VAULT_TOKEN:-root}" "http://vault.doki-data.svc.cluster.local:8200/v1/sys/health" -o /dev/null 2>/dev/null; then
  pass "  Vault read"
else
  fail "  Vault read"
fi

echo "=== Summary ==="
if [[ $FAILED -eq 0 ]]; then
  echo -e "${GREEN}All validation tests passed.${NC}"
  exit 0
else
  echo -e "${RED}$FAILED test(s) failed.${NC}"
  exit 1
fi
