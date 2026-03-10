# LLM Infrastructure — Development and Production

This document covers LLM infrastructure for both development (Ollama) and production (vLLM), including embedding services, model management, resource requirements, network flow, and implementation order.

**References:**
- `02-data-services.md` — Dragonfly key patterns, MinIO prompts bucket
- `03-security.md` — Cilium policies (doki-mcp → ai, doki-agents → ai)
- `01-cluster-and-networking.md` — kind config, ai namespace

---

## 1. Ollama (Development)

Ollama runs on the **host machine** (not inside Kubernetes). Pods in the cluster reach it via a Service backed by manual Endpoints pointing to the host.

### 1.1 Installation

```bash
curl -fsSL https://ollama.com/install.sh | sh
```

### 1.2 Configuration

| Variable | Value | Purpose |
|----------|-------|---------|
| `OLLAMA_HOST` | `0.0.0.0:11434` | Bind to all interfaces so K8s can reach host |

Set before starting Ollama (e.g. in `~/.bashrc` or systemd override):

```bash
export OLLAMA_HOST=0.0.0.0:11434
```

For systemd:

```bash
sudo mkdir -p /etc/systemd/system/ollama.service.d
echo -e '[Service]\nEnvironment="OLLAMA_HOST=0.0.0.0:11434"' | sudo tee /etc/systemd/system/ollama.service.d/override.conf
sudo systemctl daemon-reload && sudo systemctl restart ollama
```

### 1.3 Models to Pull

| Model | Size | Purpose | RAM |
|-------|------|---------|-----|
| `qwen2.5-coder:14b` | ~8.7GB | Primary dev model | ~12GB |
| `qwen2.5-coder:7b` | ~4.4GB | Fallback for low-memory | ~6GB |
| `nomic-embed-text` | ~274MB | Embeddings (Policy MCP, Memory MCP) | ~300MB |

```bash
ollama pull qwen2.5-coder:14b
ollama pull qwen2.5-coder:7b
ollama pull nomic-embed-text
```

**Note:** Pin versions explicitly (e.g. `qwen2.5-coder:14b`), never use `:latest`.

### 1.4 Memory Requirements

| Model | RAM |
|-------|-----|
| 14B | ~12GB |
| 7B | ~6GB |
| nomic-embed-text | ~300MB |

Ensure the host has sufficient RAM. For dev with 14B + embeddings: **≥16GB** recommended.

### 1.5 Kubernetes Access — Service + Endpoints

Ollama runs on the host. Create a Service in the `ai` namespace with manual Endpoints pointing to the host.

**Host resolution:**
- **Docker Desktop (Mac/Windows):** `host.docker.internal` — works by default.
- **Linux (kind):** Add `extraHosts` to kind config so nodes resolve `host.docker.internal`:

```yaml
# Add to cluster/kind-config.yaml under nodes[0]
extraHosts:
  - host.docker.internal:host-gateway
```

Requires kind 0.11+ and Docker 20.10+ with `host-gateway` support. Alternatively, use the host's IP (e.g. from `ip route | grep default | awk '{print $3}'`) and substitute in the Endpoints below.

#### Service + Endpoints YAML

File: `base/ollama/ollama-service.yaml`

```yaml
# base/ollama/ollama-service.yaml
# Ollama runs on host; Service + Endpoints expose it to cluster
---
apiVersion: v1
kind: Service
metadata:
  name: ollama
  namespace: ai
  labels:
    app: ollama
    app.kubernetes.io/part-of: doki-stack
spec:
  type: ClusterIP
  ports:
    - name: http
      port: 11434
      targetPort: 11434
      protocol: TCP
  # No selector — we use manual Endpoints
---
apiVersion: v1
kind: Endpoints
metadata:
  name: ollama
  namespace: ai
  labels:
    app: ollama
    app.kubernetes.io/part-of: doki-stack
subsets:
  - addresses:
      - ip: 192.168.0.1  # REPLACE: host.docker.internal or host gateway IP
    ports:
      - name: http
        port: 11434
        protocol: TCP
```

**Important:** Replace `192.168.0.1` with the actual host address:
- **Docker Desktop:** Use `host.docker.internal`. Kubernetes cannot resolve this directly; use the resolved IP. Option: run a small init script or use a fixed host IP.
- **Linux kind:** If `extraHosts: host.docker.internal:host-gateway` is set, the kind node resolves it. For Endpoints, you need an IP. Get it with:
  ```bash
  # From host: gateway IP of the kind network
  docker network inspect kind -f '{{range .IPAM.Config}}{{.Gateway}}{{end}}'
  ```
  Or use the host's primary IP. Set this in a Kustomize overlay or ConfigMap-driven script.

**Alternative: ExternalName Service** (DNS-only, no Endpoints)

ExternalName does not work for `host.docker.internal` because it's a CNAME and the cluster DNS may not resolve it. Prefer manual Endpoints with the resolved IP.

### 1.6 Internal DNS

| Service | DNS | Port |
|---------|-----|------|
| Ollama | `ollama.ai.svc.cluster.local` | 11434 |

### 1.7 Health Check

```bash
curl -s http://ollama.ai.svc.cluster.local:11434/api/tags
```

Returns JSON with model list. Non-empty response indicates Ollama is healthy.

---

## 2. vLLM (Production)

vLLM provides high-throughput, OpenAI-compatible inference for production.

### 2.1 Overview

| Property | Value |
|----------|-------|
| Image | `vllm/vllm-openai` |
| Model | `Qwen/Qwen2.5-Coder-32B-Instruct` |
| API | OpenAI-compatible `/v1/chat/completions` |
| Namespace | `ai` |

### 2.2 Hardware Requirements

| Configuration | GPU | Notes |
|---------------|-----|-------|
| **Preferred** | 1× A100 80GB | Single node, no tensor parallelism |
| **Alternative** | 2× A10 24GB | Tensor parallelism (`--tensor-parallel-size 2`) |

### 2.3 Deployment YAML

File: `base/vllm/deployment.yaml`

```yaml
# base/vllm/deployment.yaml
# Production LLM inference — requires GPU node
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vllm
  namespace: ai
  labels:
    app: vllm
    app.kubernetes.io/part-of: doki-stack
spec:
  replicas: 1
  selector:
    matchLabels:
      app: vllm
  template:
    metadata:
      labels:
        app: vllm
        app.kubernetes.io/part-of: doki-stack
    spec:
      # Schedule on GPU nodes only
      nodeSelector:
        nvidia.com/gpu: "true"
      containers:
        - name: vllm
          image: vllm/vllm-openai:latest
          args:
            - --model
            - Qwen/Qwen2.5-Coder-32B-Instruct
            - --tensor-parallel-size
            - "1"
            - --max-model-len
            - "8192"
            - --gpu-memory-utilization
            - "0.9"
          env:
            - name: HUGGING_FACE_HUB_TOKEN
              valueFrom:
                secretKeyRef:
                  name: vllm-hf-token
                  key: token
                  optional: false
          resources:
            requests:
              memory: "16Gi"
              cpu: "4"
              nvidia.com/gpu: "1"
            limits:
              memory: "24Gi"
              cpu: "8"
              nvidia.com/gpu: "1"
          volumeMounts:
            - name: model-cache
              mountPath: /root/.cache/huggingface
          ports:
            - containerPort: 8000
              name: http
          livenessProbe:
            httpGet:
              path: /health
              port: 8000
            initialDelaySeconds: 120
            periodSeconds: 30
            timeoutSeconds: 10
          readinessProbe:
            httpGet:
              path: /health
              port: 8000
            initialDelaySeconds: 60
            periodSeconds: 10
            timeoutSeconds: 5
      volumes:
        - name: model-cache
          persistentVolumeClaim:
            claimName: vllm-model-cache
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: vllm-model-cache
  namespace: ai
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 50Gi
  storageClassName: ""  # Use cluster default
---
apiVersion: v1
kind: Service
metadata:
  name: vllm
  namespace: ai
  labels:
    app: vllm
    app.kubernetes.io/part-of: doki-stack
spec:
  type: ClusterIP
  ports:
    - name: http
      port: 8000
      targetPort: 8000
      protocol: TCP
  selector:
    app: vllm
```

**Tensor parallelism (2× A10):** Set `--tensor-parallel-size "2"` and `nvidia.com/gpu: "2"` in resources.

**Secrets:** Create `vllm-hf-token` from Vault via External Secrets Operator. Path: `secret/data/orgs/doki-stack/vllm` with key `token`.

### 2.4 Internal DNS

| Service | DNS | Port |
|---------|-----|------|
| vLLM | `vllm.ai.svc.cluster.local` | 8000 |

### 2.5 Health Check

```bash
curl -s http://vllm.ai.svc.cluster.local:8000/health
```

### 2.6 Grafana Dashboard Metrics

Create a Grafana dashboard for vLLM observability. vLLM exposes Prometheus metrics; scrape from the vLLM pod or Service.

| Panel | Metric / Description |
|-------|----------------------|
| **Token throughput** | `vllm:num_output_tokens_total` (rate) — tokens/sec |
| **Queue depth** | `vllm:num_requests_waiting` — requests waiting in queue |
| **Batch size** | `vllm:num_requests_running` — concurrent requests |
| **Latency p50** | Histogram `vllm:request_latency_seconds` — 0.5 quantile |
| **Latency p95** | Histogram `vllm:request_latency_seconds` — 0.95 quantile |
| **Latency p99** | Histogram `vllm:request_latency_seconds` — 0.99 quantile |
| **GPU utilization** | `DCGM_FI_DEV_GPU_UTIL` (if DCGM exporter used) or node GPU metrics |

**Note:** Exact metric names may vary by vLLM version. Consult [vLLM metrics documentation](https://docs.vllm.ai/en/latest/) for the current schema.

Example Prometheus scrape config (add to `helm-values/prometheus.yaml` or ServiceMonitor):

```yaml
# ServiceMonitor for vLLM (if vLLM exposes /metrics)
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: vllm
  namespace: ai
spec:
  selector:
    matchLabels:
      app: vllm
  endpoints:
    - port: http
      path: /metrics
      interval: 15s
```

---

## 3. Embedding Service

Embeddings use Ollama's `nomic-embed-text` model. In production, this can remain on Ollama (co-located or separate) or be migrated to a dedicated embedding service.

### 3.1 API

| Endpoint | Method | Body | Response |
|----------|--------|------|----------|
| `/api/embeddings` | POST | `{"model": "nomic-embed-text", "input": "text" \| ["text1", "text2"]}` | `{"data": [{"embedding": [float, ...]}]}` |

### 3.2 Output Dimensions

| Model | Dimensions |
|-------|------------|
| nomic-embed-text | 768 |

### 3.3 Consumers

| Service | Use Case |
|---------|----------|
| mcp-policy | Policy document embedding and similarity search (Qdrant) |
| mcp-memory (EE) | Agent memory embedding |

### 3.4 Batch Size Recommendations

- **Recommended:** 32 texts per batch
- **Maximum:** 64 (avoid OOM; tune based on text length)

### 3.5 Caching

Cache embeddings in Dragonfly to avoid redundant API calls:

| Key Pattern | TTL | Purpose |
|-------------|-----|---------|
| `embed:{hash(text)}` | 24h | Embedding cache |

Hash: SHA-256 of normalized text (e.g. trim, lowercase). Key example: `embed:a1b2c3d4e5f6...`.

Add to `02-data-services.md` Dragonfly key patterns if not already present.

---

## 4. Model Management

### 4.1 Model Versioning Strategy

- **Pin model versions** in deployment configs: `qwen2.5-coder:14b`, not `:latest`.
- **Document model changes** in `CHANGELOG.md` (or `docs/CHANGELOG-models.md`).
- **vLLM:** Pin image tag and model name. Example: `vllm/vllm-openai:v0.4.0` with `Qwen/Qwen2.5-Coder-32B-Instruct`.

### 4.2 Prompt Versioning

- **Storage:** MinIO bucket `prompts`, path `prompts/{service}/v{version}/system.md`.
- **Example:** `prompts/automation/v1/system.md`, `prompts/review/v2/system.md`.
- **Version bumps:** Create new object; never edit in place. Migration scripts update service config to new version.

### 4.3 Fallback Strategy

| Priority | Model | When |
|----------|-------|------|
| Primary | qwen2.5-coder:14b (dev) / vLLM 32B (prod) | Default |
| Fallback | qwen2.5-coder:7b | Primary unavailable or low memory |
| Circuit breaker | — | 3 consecutive failures → switch to fallback; re-check primary every 60s |

Implementation: Application-level (agent-orchestrator, agent-automation, agent-review). Track failure count; on 3 failures, use fallback URL/model; every 60s attempt primary again and reset on success.

---

## 5. Resource Summary

| Component | Environment | RAM | GPU | Disk |
|-----------|-------------|-----|-----|------|
| Ollama (14B) | Dev | 12GB | — | 10GB |
| Ollama (7B) | Dev fallback | 6GB | — | 5GB |
| nomic-embed-text | All | 300MB | — | 300MB |
| vLLM (32B) | Prod | 16GB | 1× A100 80GB | 50GB |

**Total dev (14B + embeddings):** ~12.5GB RAM, ~10.5GB disk.

---

## 6. Network Flow

### 6.1 Service Dependencies

| Consumer | Target | Port | Purpose |
|----------|--------|------|---------|
| mcp-policy | ollama.ai.svc.cluster.local | 11434 | Embeddings (nomic-embed-text) |
| mcp-memory (EE) | ollama.ai.svc.cluster.local | 11434 | Embeddings (nomic-embed-text) |
| agent-automation | ollama.ai.svc.cluster.local | 11434 | Chat (qwen2.5-coder) |
| agent-review | ollama.ai.svc.cluster.local | 11434 | Chat (qwen2.5-coder) |
| agent-orchestrator | ollama.ai.svc.cluster.local | 11434 | Chat (qwen2.5-coder) |

In production, chat consumers use `vllm.ai.svc.cluster.local:8000`; embeddings may stay on Ollama or move to a dedicated embedding service.

### 6.2 Cilium Policies

Allow egress from `doki-mcp` and `doki-agents` to the `ai` namespace:

| Policy | From | To | Port |
|--------|------|-----|------|
| allow-mcp-to-ai | doki-mcp | ai (ollama) | 11434 |
| allow-agents-to-ai | doki-agents | ai (ollama, vllm) | 11434, 8000 |

See `03-security.md` Section 5.3.4 for `doki-agents → ai`. Add `doki-mcp → ai` if not present:

```yaml
# policies/cilium/allow-doki-mcp-to-ai.yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-mcp-to-ai
  namespace: doki-mcp
spec:
  endpointSelector: {}
  egress:
    - toFQDNs:
        - matchName: "*.ai.svc.cluster.local"
      toPorts:
        - ports:
            - port: "11434"
              protocol: TCP
    - toEndpoints:
        - {}
      toPorts:
        - ports:
            - port: "53"
              protocol: UDP
        - ports:
            - port: "53"
              protocol: TCP
```

---

## 7. Implementation Order

| Step | Action | Command / Notes |
|------|--------|-----------------|
| 1 | Install Ollama on host | `curl -fsSL https://ollama.com/install.sh \| sh` |
| 2 | Configure OLLAMA_HOST | `export OLLAMA_HOST=0.0.0.0:11434` (or systemd override) |
| 3 | Pull models | `ollama pull qwen2.5-coder:14b`; `ollama pull nomic-embed-text`; optionally `ollama pull qwen2.5-coder:7b` |
| 4 | Create ai namespace | `kubectl apply -f cluster/namespaces.yaml` (includes ai) |
| 5 | Resolve host IP for Endpoints | `docker network inspect kind -f '{{range .IPAM.Config}}{{.Gateway}}{{end}}'` (Linux) or use host IP |
| 6 | Deploy Service + Endpoints for Ollama | `kubectl apply -f base/ollama/ollama-service.yaml` (update Endpoints IP) |
| 7 | Apply Cilium allow policies | `kubectl apply -f policies/cilium/allow-doki-mcp-to-ai.yaml`; `kubectl apply -f policies/cilium/allow-doki-agents-to-ai.yaml` |
| 8 | Verify connectivity from doki-mcp | `kubectl run -it --rm curl --image=curlimages/curl -n doki-mcp -- curl -s http://ollama.ai.svc.cluster.local:11434/api/tags` |
| 9 | Verify connectivity from doki-agents | `kubectl run -it --rm curl --image=curlimages/curl -n doki-agents -- curl -s http://ollama.ai.svc.cluster.local:11434/api/tags` |
| 10 | (Prod) Deploy vLLM | Create GPU node pool; apply `base/vllm/`; create `vllm-hf-token` Secret from Vault |

---

## Appendix A: Ollama Endpoints IP Resolution Script

For automation, use a script to populate the Ollama Endpoints IP:

```bash
#!/bin/bash
# scripts/resolve-ollama-host.sh
# Outputs the host IP for Ollama Endpoints (kind cluster)
if command -v docker &>/dev/null; then
  # Try kind network gateway (Linux)
  GATEWAY=$(docker network inspect kind -f '{{range .IPAM.Config}}{{.Gateway}}{{end}}' 2>/dev/null)
  if [ -n "$GATEWAY" ]; then
    echo "$GATEWAY"
    exit 0
  fi
fi
# Fallback: host default gateway
ip route | grep default | awk '{print $3}' | head -1
```

Use in CI or Makefile to patch the Endpoints resource before apply.

---

## Appendix B: vLLM Tensor Parallelism (2× A10)

For 2× A10 24GB, use this Deployment overlay:

```yaml
# overlays/prod/vllm-tensor-parallel.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vllm
  namespace: ai
spec:
  template:
    spec:
      containers:
        - name: vllm
          args:
            - --model
            - Qwen/Qwen2.5-Coder-32B-Instruct
            - --tensor-parallel-size
            - "2"
            - --max-model-len
            - "8192"
            - --gpu-memory-utilization
            - "0.9"
          resources:
            requests:
              nvidia.com/gpu: "2"
            limits:
              nvidia.com/gpu: "2"
```

Ensure both GPUs are on the same node (use a node pool with 2 GPUs per node).
