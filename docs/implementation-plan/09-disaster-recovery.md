# Disaster Recovery — Backup, Restore, and DR Procedures

This document covers backup, restore, and disaster recovery procedures for the Doki Stack platform. All procedures are designed to meet the defined recovery objectives and support both development and production environments.

**References:**
- [02-data-services.md](./02-data-services.md) — Data service configurations, bucket names, connection strings
- [db-schemas/docs/implementation-plan/02-rls-and-multi-tenancy.md](../../../db-schemas/docs/implementation-plan/02-rls-and-multi-tenancy.md) — PostgreSQL RLS validation
- [db-schemas/docs/implementation-plan/04-migration-strategy.md](../../../db-schemas/docs/implementation-plan/04-migration-strategy.md) — Migration state and schema versioning

---

## 1. Recovery Objectives

### 1.1 Global Objectives

| Metric | Target | Scope |
|--------|--------|-------|
| **RPO** (Recovery Point Objective) | ≤ 1 hour | Maximum acceptable data loss |
| **RTO** (Recovery Time Objective) | ≤ 4 hours | Maximum acceptable downtime |

### 1.2 Per-Component Objectives

| Component | RPO | RTO | Criticality | Notes |
|-----------|-----|-----|--------------|-------|
| **PostgreSQL** | 1h (dev) / 15m (prod) | 2h | Critical | Source of truth for orgs, users, tasks, LangGraph checkpoints |
| **MinIO** | 1h | 2h | Critical | terraform-states, scanner-artifacts; versioning mitigates overwrites |
| **Qdrant** | 6h | 4h | High | Rebuildable from PostgreSQL + policy docs; policy MCP cache |
| **RabbitMQ** | N/A | 1h | Medium | Stateless; topology + DLQ recovery |
| **Dragonfly** | N/A | 15m | Low | Cache only; warm-up after restart |
| **Vault** | 1h (prod) | 1h | Critical | Transit keys, secrets; Raft snapshots |
| **Kubernetes (Velero)** | 24h (dev) / 6h (prod) | 4h | Critical | Full cluster/namespace restore |

---

## 2. Velero (Kubernetes Backup)

### 2.1 Overview

| Property | Value |
|----------|-------|
| Helm chart | `vmware-tanzu/velero` |
| Namespace | `doki-system` |
| Backend | MinIO (S3-compatible) |
| Bucket | `velero-backups` |

### 2.2 Helm Values

File: `helm-values/velero.yaml`

```yaml
# Velero — Kubernetes backup to MinIO
# Helm: helm repo add vmware-tanzu https://vmware-tanzu.github.io/helm-charts
#       helm install velero vmware-tanzu/velero -n doki-system -f helm-values/velero.yaml
#
# Prerequisites:
# 1. Create MinIO bucket: velero-backups (mc mb minio/velero-backups)
# 2. Create velero-credentials secret with cloud credentials (see below)

initContainers:
  - name: velero-plugin-for-aws
    image: velero/velero-plugin-for-aws:v1.11.0
    imagePullPolicy: IfNotPresent
    volumeMounts:
      - mountPath: /target
        name: plugins

resources:
  requests:
    memory: 256Mi
    cpu: 250m
  limits:
    memory: 512Mi
    cpu: 500m

configuration:
  backupStorageLocation:
    - name: default
      provider: aws
      bucket: velero-backups
      default: true
      config:
        region: minio
        s3Url: http://minio.doki-data.svc.cluster.local:9000
        s3ForcePathStyle: "true"
        insecureSkipTLSVerify: "true"
      credential:
        name: velero-credentials
        key: cloud

  # kind does not support CSI snapshots; use filesystem backup via node-agent
  volumeSnapshotLocation: []
  snapshotsEnabled: false

  # Use filesystem backup for PVCs (restic/kopia)
  defaultVolumesToFsBackup: true

  # Server settings
  defaultBackupTTL: 168h  # 7 days (override per schedule for prod)

credentials:
  useSecret: true
  existingSecret: velero-credentials
  secretContents: {}

# Enable node-agent for filesystem backup (required when snapshots disabled)
deployNodeAgent: true

nodeAgent:
  resources:
    requests:
      memory: 128Mi
      cpu: 100m
    limits:
      memory: 256Mi
      cpu: 250m
```

### 2.3 Velero Credentials Secret

Create the `velero-credentials` secret before installing Velero. For MinIO (S3-compatible), use AWS credential format:

```bash
# Create credentials file for MinIO
cat <<EOF > /tmp/velero-credentials
[default]
aws_access_key_id=MINIO_ACCESS_KEY
aws_secret_access_key=MINIO_SECRET_KEY
EOF

# Create secret in doki-system
kubectl create secret generic velero-credentials \
  --namespace doki-system \
  --from-file=cloud=/tmp/velero-credentials

# Clean up
rm /tmp/velero-credentials
```

**Production:** Use Vault/ESO to inject `velero-credentials`; never commit real keys.

### 2.4 MinIO Bucket Setup

```bash
# Port-forward MinIO if needed
kubectl port-forward -n doki-data svc/minio 9000:9000 &

# Create bucket (using mc alias)
mc alias set doki http://localhost:9000 $MINIO_ROOT_USER $MINIO_ROOT_PASSWORD
mc mb doki/velero-backups --ignore-existing
mc anonymous set download doki/velero-backups  # Velero needs read/write
```

### 2.5 Backup Schedules

#### Full Cluster Schedule (Dev: 7 days retention)

```yaml
# base/velero/schedule-full-cluster-dev.yaml
apiVersion: velero.io/v1
kind: Schedule
metadata:
  name: daily-full-backup
  namespace: doki-system
spec:
  schedule: "0 2 * * *"  # 02:00 UTC daily
  template:
    includedNamespaces:
      - "*"
    excludedNamespaces:
      - monitoring
      - ai
      - kube-system
      - kube-public
      - kube-node-lease
    storageLocation: default
    ttl: 168h  # 7 days
    defaultVolumesToFsBackup: true
    hooks: {}
```

#### Full Cluster Schedule (Prod: 30 days retention)

```yaml
# overlays/prod/velero/schedule-full-cluster-prod.yaml
apiVersion: velero.io/v1
kind: Schedule
metadata:
  name: daily-full-backup
  namespace: doki-system
spec:
  schedule: "0 2 * * *"
  template:
    includedNamespaces:
      - "*"
    excludedNamespaces:
      - monitoring
      - ai
      - kube-system
      - kube-public
      - kube-node-lease
    storageLocation: default
    ttl: 720h  # 30 days
    defaultVolumesToFsBackup: true
```

#### Namespaced Schedule (doki-data every 6h)

```yaml
# base/velero/schedule-doki-data.yaml
apiVersion: velero.io/v1
kind: Schedule
metadata:
  name: doki-data-6h
  namespace: doki-system
spec:
  schedule: "0 */6 * * *"  # Every 6 hours
  template:
    includedNamespaces:
      - doki-data
    storageLocation: default
    ttl: 168h  # 7 days (dev); override to 720h in prod overlay
    defaultVolumesToFsBackup: true
```

### 2.6 Excluded Namespaces

| Namespace | Reason |
|-----------|--------|
| `monitoring` | Recreatable; Prometheus/Grafana/Loki/Tempo can be redeployed |
| `ai` | Stateless; Ollama endpoints redeploy from manifests |
| `kube-system` | Cluster-managed; Cilium, CoreDNS |
| `kube-public` | Cluster-managed |
| `kube-node-lease` | Ephemeral node leases |

### 2.7 On-Demand Backup (Before Destructive Operations)

```bash
# Full cluster backup
velero backup create manual-pre-destructive-$(date +%Y%m%d-%H%M%S) \
  --namespace doki-system \
  --exclude-namespaces monitoring,ai,kube-system,kube-public,kube-node-lease \
  --default-volumes-to-fs-backup

# Namespace-only backup (e.g., before doki-data changes)
velero backup create manual-doki-data-$(date +%Y%m%d-%H%M%S) \
  --namespace doki-system \
  --include-namespaces doki-data \
  --default-volumes-to-fs-backup
```

### 2.8 Restore Procedure

#### Step 1: List available backups

```bash
velero backup get --namespace doki-system
```

#### Step 2: Create restore from backup

```bash
# Replace BACKUP_NAME with actual backup name from step 1
velero restore create --from-backup BACKUP_NAME \
  --namespace doki-system \
  --wait
```

#### Step 3: Monitor restore progress

```bash
velero restore describe RESTORE_NAME --namespace doki-system
velero restore logs RESTORE_NAME --namespace doki-system
```

#### Step 4: Verify restored resources

```bash
kubectl get pods -A | grep -v kube-system
kubectl get pvc -A
```

#### Step 5: Restart any stuck pods

```bash
# If pods are in CrashLoopBackOff, check logs and restart
kubectl rollout restart deployment -n doki-platform
kubectl rollout restart deployment -n doki-mcp
kubectl rollout restart statefulset -n doki-data
```

#### Step 6: Run RLS validation (PostgreSQL)

```bash
# From db-schemas repo
export DATABASE_URL="postgres://app_admin:PASSWORD@postgres-postgresql.doki-data.svc.cluster.local:5432/ai_automation?sslmode=disable"
./scripts/validate-rls.sh
```

---

## 3. PostgreSQL Backup & Recovery

### 3.1 Dev: pg_dump to MinIO

#### CronJob Manifest

```yaml
# base/postgresql/backup-cronjob.yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: postgres-backup
  namespace: doki-data
  labels:
    app: postgres-backup
spec:
  schedule: "0 3 * * *"  # 03:00 UTC daily (after Velero)
  concurrencyPolicy: Forbid
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 3
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          initContainers:
            - name: pg-dump
              image: postgres:16-alpine
              command:
                - /bin/sh
                - -c
                - |
                  set -e
                  export PGPASSWORD="${POSTGRES_PASSWORD}"
                  TIMESTAMP=$(date +%Y%m%d-%H%M%S)
                  pg_dump -h postgres-postgresql.doki-data.svc.cluster.local -U postgres -d ai_automation -Fc | gzip > /backup/ai_automation-${TIMESTAMP}.sql.gz
                  pg_dump -h postgres-postgresql.doki-data.svc.cluster.local -U postgres -d terraform_states -Fc | gzip > /backup/terraform_states-${TIMESTAMP}.sql.gz
                  echo "$TIMESTAMP" > /backup/latest.txt
              env:
                - name: POSTGRES_PASSWORD
                  valueFrom:
                    secretKeyRef:
                      name: postgres-credentials
                      key: postgres-password
              volumeMounts:
                - name: backup
                  mountPath: /backup
          containers:
            - name: upload-to-minio
              image: minio/mc:latest
              command:
                - /bin/sh
                - -c
                - |
                  mc alias set minio http://minio.doki-data.svc.cluster.local:9000 $MINIO_ROOT_USER $MINIO_ROOT_PASSWORD
                  mc mb minio/scanner-artifacts --ignore-existing
                  mc cp /backup/ai_automation-*.sql.gz minio/scanner-artifacts/org_id=system/backups/postgres/ || true
                  mc cp /backup/terraform_states-*.sql.gz minio/scanner-artifacts/org_id=system/backups/postgres/ || true
              env:
                - name: MINIO_ROOT_USER
                  valueFrom:
                    secretKeyRef:
                      name: minio-credentials
                      key: rootUser
                - name: MINIO_ROOT_PASSWORD
                  valueFrom:
                    secretKeyRef:
                      name: minio-credentials
                      key: rootPassword
              volumeMounts:
                - name: backup
                  mountPath: /backup
          volumes:
            - name: backup
              emptyDir: {}
```

### 3.2 Prod: WAL Archival and PITR

#### archive_command Configuration

Add to PostgreSQL `postgresql.conf` (via ConfigMap or Helm values):

```conf
wal_level = replica
archive_mode = on
archive_command = 'wal-g wal-push %p'
```

#### wal-g Configuration

Create `~/.wal-g.json` or use environment variables:

```json
{
  "WALG_S3_PREFIX": "s3://velero-backups/pg-wal/",
  "AWS_ACCESS_KEY_ID": "<minio-access-key>",
  "AWS_SECRET_ACCESS_KEY": "<minio-secret-key>",
  "AWS_ENDPOINT": "http://minio.doki-data.svc.cluster.local:9000",
  "AWS_S3_FORCE_PATH_STYLE": "true"
}
```

#### pgBackRest Alternative

```ini
# /etc/pgbackrest/pgbackrest.conf
[main]
pg1-path=/var/lib/postgresql/data
repo1-type=s3
repo1-s3-endpoint=minio.doki-data.svc.cluster.local
repo1-s3-port=9000
repo1-s3-bucket=velero-backups
repo1-path=/pgbackrest
repo1-s3-uri-style=path
```

### 3.3 PostgreSQL Restore Procedure

#### Step 1: Stop services that use PostgreSQL

```bash
kubectl scale deployment --all -n doki-platform --replicas=0
kubectl scale deployment --all -n doki-mcp --replicas=0
kubectl scale deployment --all -n doki-agents --replicas=0
```

#### Step 2: Restore base backup

**Dev (pg_restore from pg_dump):**

```bash
# Download latest backup from MinIO
mc cp minio/scanner-artifacts/org_id=system/backups/postgres/ai_automation-LATEST.sql.gz /tmp/
gunzip -c /tmp/ai_automation-LATEST.sql.gz | pg_restore -h postgres-postgresql.doki-data.svc.cluster.local -U postgres -d ai_automation --clean --if-exists
```

**Prod (wal-g):**

```bash
wal-g backup-fetch LATEST
```

#### Step 3: Replay WAL to target timestamp (PITR)

**Prod only:** Create `recovery.signal` and configure `restore_command`:

```conf
restore_command = 'wal-g wal-fetch %f %p'
recovery_target_time = '2025-03-10 14:30:00 UTC'
```

#### Step 4: Verify data integrity

```bash
# Run migrations to ensure schema is current (db-schemas)
# Verify RLS
export DATABASE_URL="postgres://app_admin:PASSWORD@postgres-postgresql.doki-data.svc.cluster.local:5432/ai_automation?sslmode=disable"
cd db-schemas && ./scripts/validate-rls.sh
```

#### Step 5: Restart services

```bash
kubectl scale deployment --all -n doki-platform --replicas=1
kubectl scale deployment --all -n doki-mcp --replicas=1
kubectl scale deployment --all -n doki-agents --replicas=1
```

### 3.4 Cross-Reference: db-schemas Migration State

Before restore, ensure the target schema version matches the backup. See `db-schemas/docs/implementation-plan/04-migration-strategy.md`:

- Run `db-schemas` migrations after restore if backup is from an older schema version
- The `schema_migrations` table tracks applied migrations
- RLS policies are part of migrations; `validate-rls.sh` confirms tenant isolation

---

## 4. MinIO Backup Strategy

### 4.1 Versioning

Enable versioning on all buckets (already in `helm-values/minio.yaml`):

```yaml
buckets:
  - name: scanner-artifacts
    versioning: true
  - name: terraform-states
    versioning: true
  - name: execution-plans
    versioning: true
  - name: velero-backups
    versioning: true
```

### 4.2 Replication (Prod)

```bash
mc alias set secondary https://minio-secondary.example.com $SECONDARY_ACCESS_KEY $SECONDARY_SECRET_KEY
mc replicate add doki/terraform-states --remote-bucket secondary/terraform-states
mc replicate add doki/scanner-artifacts --remote-bucket secondary/scanner-artifacts
```

### 4.3 Critical vs Non-Critical Buckets

| Bucket | Criticality | Recovery |
|--------|-------------|----------|
| `terraform-states` | Critical | Replicate; restore from secondary |
| `scanner-artifacts` | Critical | Replicate; versioning protects overwrites |
| `velero-backups` | Critical | Replicate to secondary cluster |
| `execution-plans` | Non-critical | Regeneratable by agents |
| `prompts` | Medium | Platform-level; can be restored from Git |

### 4.4 Lifecycle Policy

Retain current + 3 previous versions; expire non-current after 90 days:

```json
{
  "Rules": [
    {
      "ID": "retain-versions",
      "Status": "Enabled",
      "NoncurrentVersionExpiration": {
        "NoncurrentDays": 90
      },
      "RuleFilter": {}
    }
  ]
}
```

Apply via MinIO Console or `mc ilm add`.

---

## 5. RabbitMQ Recovery

### 5.1 Stateless Design

From application perspective, RabbitMQ is stateless—messages are transient. On recovery, topology (exchanges, queues, bindings) must be recreated.

### 5.2 Definitions Export

```bash
kubectl exec -n doki-data rabbitmq-0 -- rabbitmqctl export_definitions /tmp/definitions.json
kubectl cp doki-data/rabbitmq-0:/tmp/definitions.json ./rabbitmq-definitions.json
```

Store `rabbitmq-definitions.json` in MinIO or Git (e.g. `scanner-artifacts/org_id=system/backups/rabbitmq/`).

### 5.3 Recovery Procedure

1. **Redeploy StatefulSet:** `kubectl apply -f base/rabbitmq/`
2. **Wait for pod ready:** `kubectl wait --for=condition=Ready pod/rabbitmq-0 -n doki-data --timeout=120s`
3. **Import definitions:** `kubectl exec -n doki-data rabbitmq-0 -- rabbitmqctl import_definitions /path/to/definitions.json`
4. **DLQ messages:** Preserved in PVC; after recovery, DLQ queues contain messages that failed before the incident. Replay manually or via script if needed.

---

## 6. Qdrant Recovery

### 6.1 Snapshot API

```bash
curl -X POST "http://qdrant.doki-data.svc.cluster.local:6333/collections/policies/snapshots"
curl "http://qdrant.doki-data.svc.cluster.local:6333/collections/policies/snapshots"
```

### 6.2 CronJob for Periodic Snapshots

```yaml
# base/qdrant/snapshot-cronjob.yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: qdrant-snapshot
  namespace: doki-data
spec:
  schedule: "0 */6 * * *"  # Every 6 hours
  concurrencyPolicy: Forbid
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          containers:
            - name: snapshot
              image: curlimages/curl:latest
              command:
                - /bin/sh
                - -c
                - |
                  for col in policies agent_memories; do
                    SNAPSHOT=$(curl -s -X POST "http://qdrant.doki-data.svc.cluster.local:6333/collections/$col/snapshots" | jq -r '.result.name')
                    if [ -n "$SNAPSHOT" ]; then
                      curl -s "http://qdrant.doki-data.svc.cluster.local:6333/collections/$col/snapshots/$SNAPSHOT" -o /tmp/${col}-${SNAPSHOT}
                      mc alias set minio http://minio.doki-data.svc.cluster.local:9000 $MINIO_ROOT_USER $MINIO_ROOT_PASSWORD
                      mc cp /tmp/${col}-${SNAPSHOT} minio/scanner-artifacts/org_id=system/backups/qdrant/
                    fi
                  done
              env:
                - name: MINIO_ROOT_USER
                  valueFrom:
                    secretKeyRef:
                      name: minio-credentials
                      key: rootUser
                - name: MINIO_ROOT_PASSWORD
                  valueFrom:
                    secretKeyRef:
                      name: minio-credentials
                      key: rootPassword
```

**Note:** Use an image with `jq` and `mc` (e.g. custom image or `alpine` with apk add). The `curlimages/curl` image lacks `jq`; consider `alpine/curl` with `jq` or a custom Dockerfile.

### 6.3 Recovery Procedure

1. **Create collection** (if dropped): Use Qdrant API or let Policy MCP recreate on startup
2. **Restore from snapshot:** `POST /collections/{name}/snapshots/upload` or recover from MinIO
3. **If lost:** Rebuild from PostgreSQL (policy documents, agent memories) and re-embed via Policy MCP / Memory MCP

---

## 7. Dragonfly Recovery

### 7.1 Cache-Only Design

Dragonfly is a cache. No backup required.

### 7.2 Recovery

1. **Restart pod:** `kubectl rollout restart statefulset dragonfly -n doki-data`
2. **Caches rebuild on demand** as Policy MCP, Scanner MCP, and API server make requests

### 7.3 Warm-Up Strategy (Optional)

```bash
curl -X POST http://kong-proxy.doki-system.svc.cluster.local:8000/mcp/policy/query \
  -H "Content-Type: application/json" \
  -d '{"query": "common policies", "org_id": "..."}'
```

---

## 8. Vault Recovery

### 8.1 Dev Mode

Dev mode has no persistence. Acceptable for local development. Restart clears all data.

### 8.2 Prod: Raft Snapshots

```bash
kubectl exec -n doki-data vault-0 -- vault operator raft snapshot save /tmp/vault.snap
kubectl cp doki-data/vault-0:/tmp/vault.snap ./vault-$(date +%Y%m%d).snap
mc cp ./vault-$(date +%Y%m%d).snap minio/scanner-artifacts/org_id=system/backups/vault/
```

### 8.3 Restore Procedure

1. **Stop Vault:** Scale StatefulSet to 0
2. **Clear PVC data** (or use fresh PVC)
3. **Copy snapshot to pod:** `kubectl cp ./vault.snap doki-data/vault-0:/tmp/vault.snap`
4. **Restore:** `kubectl exec -n doki-data vault-0 -- vault operator raft snapshot restore /tmp/vault.snap`
5. **Unseal:** Provide unseal keys (from secure storage)
6. **Scale up:** `kubectl scale statefulset vault -n doki-data --replicas=1`

### 8.4 Unseal Procedure

```bash
kubectl exec -n doki-data vault-0 -- vault operator unseal $UNSEAL_KEY_1
kubectl exec -n doki-data vault-0 -- vault operator unseal $UNSEAL_KEY_2
kubectl exec -n doki-data vault-0 -- vault operator unseal $UNSEAL_KEY_3
```

---

## 9. DR Drill Procedure

**Frequency:** Monthly

### Checklist

| Step | Action | Command / Verification |
|------|--------|------------------------|
| 1 | Trigger Velero backup | `velero backup create dr-drill-$(date +%Y%m%d) --wait` |
| 2 | Delete non-production namespace | `kubectl delete namespace doki-mcp` (or use a test namespace) |
| 3 | Restore from Velero | `velero restore create --from-backup dr-drill-YYYYMMDD --wait` |
| 4 | Verify pods healthy | `kubectl get pods -A \| grep -v Completed` |
| 5 | Run RLS validation | `cd db-schemas && ./scripts/validate-rls.sh` |
| 6 | Verify MinIO buckets | `mc ls minio/scanner-artifacts` |
| 7 | Verify RabbitMQ topology | `kubectl exec -n doki-data rabbitmq-0 -- rabbitmqctl list_queues` |
| 8 | Record results | Document time-to-recovery, issues, improvements |

### Drill Report Template

```markdown
## DR Drill — YYYY-MM-DD

- **Start:** HH:MM UTC
- **End:** HH:MM UTC
- **Time to recovery:** Xh Ym
- **Issues:** (list any)
- **Actions:** (follow-up items)
```

---

## 10. Multi-Site DR (Phase 4 / Prod)

### 10.1 Architecture

- **Primary:** Hetzner primary cluster
- **Standby:** Hetzner secondary cluster (warm standby)
- **PostgreSQL:** WAL shipping to standby
- **MinIO:** Bucket replication to secondary
- **DNS:** Manual failover for MVP; automated later

### 10.2 PostgreSQL Standby

- Streaming replication: `primary_conninfo` points to primary
- WAL archive shared via MinIO
- Promote standby: `pg_ctl promote` or `recovery_target = 'immediate'`

### 10.3 Argo CD

- Same infrastructure repo, different overlay (`overlays/prod-primary`, `overlays/prod-standby`)
- Standby cluster runs Argo CD in read-only or suspended mode
- On failover: switch overlay, sync, promote PostgreSQL

### 10.4 DNS Failover

- MVP: Manual CNAME/ALIAS update to point to standby cluster ingress
- Future: Automated health-check-based failover (e.g. Route53, Cloudflare)

---

## 11. Implementation Order

| Step | Action | Owner |
|------|--------|-------|
| 1 | Install Velero with MinIO backend | Platform |
| 2 | Configure backup schedules (full + doki-data) | Platform |
| 3 | Create PostgreSQL CronJob (pg_dump) | Database |
| 4 | Test full backup/restore cycle | QA/SRE |
| 5 | Document runbooks (this doc + linked) | Technical Writer |
| 6 | Schedule first DR drill | SRE |

---

## References

- [Velero Documentation](https://velero.io/docs/)
- [PostgreSQL Continuous Archiving](https://www.postgresql.org/docs/current/continuous-archiving.html)
- [wal-g](https://github.com/wal-g/wal-g)
- [pgBackRest](https://pgbackrest.org/)
- [Qdrant Snapshots](https://qdrant.tech/documentation/guides/snapshots/)
- [Vault Raft Storage](https://developer.hashicorp.com/vault/docs/configuration/storage/raft)
