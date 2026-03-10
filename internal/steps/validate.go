package steps

import (
	"fmt"
	"strings"
	"time"

	"github.com/doki-stack/infrastructure/internal/runner"
)

func RunValidationTests() []CheckResult {
	var results []CheckResult
	results = append(results, validateDNS()...)
	results = append(results, validateCrossNamespace())
	results = append(results, validateMinIO())
	results = append(results, validateRabbitMQ())
	results = append(results, validateQdrant())
	results = append(results, validateDragonfly())
	results = append(results, validateVault())
	return results
}

var dnsServices = []string{
	"postgres-postgresql.doki-data.svc.cluster.local",
	"minio.doki-data.svc.cluster.local",
	"rabbitmq.doki-data.svc.cluster.local",
	"qdrant.doki-data.svc.cluster.local",
	"dragonfly.doki-data.svc.cluster.local",
	"vault.doki-data.svc.cluster.local",
}

func validateDNS() []CheckResult {
	var results []CheckResult
	for _, svc := range dnsServices {
		res := runner.Run("kubectl", "run", podName("dns"),
			"--rm", "-i", "--restart=Never",
			"--image=busybox:1.36", "-n", "doki-mcp",
			"--", "nslookup", svc,
		)
		name := "DNS: " + svc
		if res.Success() && strings.Contains(res.Stdout, "Address") {
			results = append(results, CheckResult{Name: name, Status: "pass"})
		} else {
			results = append(results, CheckResult{Name: name, Status: "fail"})
		}
	}
	return results
}

func validateCrossNamespace() CheckResult {
	res := runner.Run("kubectl", "run", podName("xns"),
		"--rm", "-i", "--restart=Never",
		"--image=curlimages/curl:latest", "-n", "doki-mcp",
		"--", "curl", "-sf",
		"http://qdrant.doki-data.svc.cluster.local:6333/collections",
		"-o", "/dev/null",
	)
	if res.Success() {
		return CheckResult{Name: "Cross-namespace: doki-mcp -> qdrant.doki-data", Status: "pass"}
	}
	return CheckResult{Name: "Cross-namespace: doki-mcp -> qdrant.doki-data", Status: "fail"}
}

func validateMinIO() CheckResult {
	testObj := fmt.Sprintf("test/validate-%d.txt", randomSuffix())
	cmd := fmt.Sprintf(`
mc alias set local http://minio.doki-data.svc.cluster.local:9000 minioadmin minioadmin && \
mc mb local/scanner-artifacts 2>/dev/null || true && \
echo 'test' | mc pipe local/scanner-artifacts/%s && \
mc cat local/scanner-artifacts/%s | grep -q test && \
mc rm local/scanner-artifacts/%s
`, testObj, testObj, testObj)

	res := runner.Run("kubectl", "run", podName("minio"),
		"--rm", "-i", "--restart=Never",
		"--image=minio/mc:latest", "-n", "doki-data",
		"--", "sh", "-c", cmd,
	)
	if res.Success() {
		return CheckResult{Name: "MinIO create/read/delete", Status: "pass"}
	}
	return CheckResult{Name: "MinIO create/read/delete", Status: "fail"}
}

func validateRabbitMQ() CheckResult {
	queue := fmt.Sprintf("test-validate-%d", randomSuffix())
	rmqURL := "http://rabbitmq.doki-data.svc.cluster.local:15672"
	auth := "doki:changeme-in-vault"
	cmd := fmt.Sprintf(`
curl -sf -u '%s' -X PUT -H 'Content-Type: application/json' -d '{}' '%s/api/queues/%%2F/%s' && \
curl -sf -u '%s' -X POST -H 'Content-Type: application/json' \
  -d '{"properties":{},"routing_key":"%s","payload":"validate-test","payload_encoding":"string"}' \
  '%s/api/exchanges/%%2F/amq.default/publish' && \
curl -sf -u '%s' -X POST -H 'Content-Type: application/json' \
  -d '{"count":1,"ackmode":"ack_requeue_false"}' \
  '%s/api/queues/%%2F/%s/get' | grep -q 'validate-test' && \
curl -sf -u '%s' -X DELETE '%s/api/queues/%%2F/%s'
`, auth, rmqURL, queue,
		auth, queue, rmqURL,
		auth, rmqURL, queue,
		auth, rmqURL, queue,
	)

	res := runner.Run("kubectl", "run", podName("rmq"),
		"--rm", "-i", "--restart=Never",
		"--image=curlimages/curl:latest", "-n", "doki-data",
		"--", "sh", "-c", cmd,
	)
	if res.Success() {
		return CheckResult{Name: "RabbitMQ publish/consume", Status: "pass"}
	}
	return CheckResult{Name: "RabbitMQ publish/consume", Status: "fail"}
}

func validateQdrant() CheckResult {
	res := runner.Run("kubectl", "run", podName("qdr"),
		"--rm", "-i", "--restart=Never",
		"--image=curlimages/curl:latest", "-n", "doki-data",
		"--", "curl", "-sf",
		"http://qdrant.doki-data.svc.cluster.local:6333/collections",
	)
	if res.Success() && strings.Contains(res.Stdout, "collections") {
		return CheckResult{Name: "Qdrant collections API", Status: "pass"}
	}
	return CheckResult{Name: "Qdrant collections API", Status: "fail"}
}

func validateDragonfly() CheckResult {
	key := fmt.Sprintf("validate:test:%d", randomSuffix())
	cmd := fmt.Sprintf(`
redis-cli -h dragonfly.doki-data.svc.cluster.local -p 6379 SET %s ok && \
redis-cli -h dragonfly.doki-data.svc.cluster.local -p 6379 GET %s | grep -q ok && \
redis-cli -h dragonfly.doki-data.svc.cluster.local -p 6379 DEL %s
`, key, key, key)

	res := runner.Run("kubectl", "run", podName("dfv"),
		"--rm", "-i", "--restart=Never",
		"--image=redis:7-alpine", "-n", "doki-data",
		"--", "sh", "-c", cmd,
	)
	if res.Success() {
		return CheckResult{Name: "Dragonfly SET/GET/DEL", Status: "pass"}
	}
	return CheckResult{Name: "Dragonfly SET/GET/DEL", Status: "fail"}
}

func validateVault() CheckResult {
	res := runner.Run("kubectl", "run", podName("vlt"),
		"--rm", "-i", "--restart=Never",
		"--image=curlimages/curl:latest", "-n", "doki-data",
		"--", "curl", "-sf",
		"-H", "X-Vault-Token: root",
		"http://vault.doki-data.svc.cluster.local:8200/v1/sys/health",
		"-o", "/dev/null",
	)
	if res.Success() {
		return CheckResult{Name: "Vault health", Status: "pass"}
	}
	return CheckResult{Name: "Vault health", Status: "fail"}
}

func randomSuffix() int64 {
	return time.Now().UnixNano() % 100000
}
