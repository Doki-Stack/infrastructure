#!/bin/bash
set -e

if command -v docker &>/dev/null; then
  HOST_IP=$(docker network inspect kind -f '{{range .IPAM.Config}}{{.Gateway}}{{end}}' 2>/dev/null || true)
fi
if [ -z "$HOST_IP" ]; then
  HOST_IP=$(ip route | grep default | awk '{print $3}' | head -1)
fi
if [ -z "$HOST_IP" ]; then
  echo "Could not detect host IP"
  exit 1
fi

kubectl patch endpoints ollama -n ai --type=merge -p "{\"subsets\":[{\"addresses\":[{\"ip\":\"$HOST_IP\"}],\"ports\":[{\"port\":11434,\"protocol\":\"TCP\"}]}]}"

kubectl run curl-ollama-verify --rm -i --restart=Never --image=curlimages/curl -n ai -- curl -sf http://ollama.ai.svc.cluster.local:11434/api/tags
