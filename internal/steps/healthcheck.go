package steps

import (
	"fmt"
	"strings"
	"time"

	"github.com/doki-stack/infrastructure/internal/runner"
	"github.com/doki-stack/infrastructure/internal/ui"
)

type CheckResult struct {
	Name   string
	Status string // "pass", "fail", "skip"
	Detail string
}

func (c CheckResult) Render() string {
	switch c.Status {
	case "pass":
		return ui.Pass(c.Name)
	case "fail":
		msg := c.Name
		if c.Detail != "" {
			msg += " (" + c.Detail + ")"
		}
		return ui.Fail(msg)
	case "skip":
		return ui.Skip(c.Name)
	default:
		return c.Name
	}
}

func RunAllHealthChecks() []CheckResult {
	return []CheckResult{
		checkClusterAccess(),
		checkNamespaces(),
		checkPodStatus(),
		checkPostgreSQL(),
		checkMinIO(),
		checkRabbitMQ(),
		checkQdrant(),
		checkDragonfly(),
		checkOllama(),
		checkGrafana(),
		checkVault(),
		checkKong(),
	}
}

func podName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()%100000)
}

func checkClusterAccess() CheckResult {
	res := runner.Run("kubectl", "cluster-info")
	if res.Success() {
		return CheckResult{Name: "Kubernetes cluster accessible", Status: "pass"}
	}
	return CheckResult{Name: "Kubernetes cluster accessible", Status: "fail"}
}

var requiredNamespaces = []string{
	"doki-system", "doki-data", "doki-mcp", "doki-agents",
	"doki-platform", "doki-ee", "doki-monitoring", "doki-ai",
}

func checkNamespaces() CheckResult {
	var missing []string
	for _, ns := range requiredNamespaces {
		res := runner.Run("kubectl", "get", "namespace", ns)
		if !res.Success() {
			missing = append(missing, ns)
		}
	}
	if len(missing) > 0 {
		return CheckResult{
			Name: "All namespaces exist", Status: "fail",
			Detail: "missing: " + strings.Join(missing, ", "),
		}
	}
	return CheckResult{Name: "All namespaces exist", Status: "pass"}
}

func checkPodStatus() CheckResult {
	var badPods []string
	for _, ns := range requiredNamespaces {
		res := runner.Run("kubectl", "get", "pods", "-n", ns, "--no-headers")
		if !res.Success() {
			continue
		}
		for _, line := range strings.Split(res.Stdout, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if !strings.Contains(line, "Running") &&
				!strings.Contains(line, "Completed") &&
				!strings.Contains(line, "Succeeded") {
				fields := strings.Fields(line)
				if len(fields) > 0 {
					badPods = append(badPods, ns+"/"+fields[0])
				}
			}
		}
	}
	if len(badPods) > 0 {
		return CheckResult{
			Name: "All pods Running/Completed", Status: "fail",
			Detail: strings.Join(badPods, ", "),
		}
	}
	return CheckResult{Name: "All pods Running/Completed", Status: "pass"}
}

func curlCheck(name, namespace, url string) CheckResult {
	res := runner.Run("kubectl", "run", podName("chk"),
		"--rm", "-i", "--restart=Never",
		"--image=curlimages/curl:latest",
		"-n", namespace,
		"--", "curl", "-sf", url, "-o", "/dev/null",
	)
	if res.Success() {
		return CheckResult{Name: name, Status: "pass"}
	}
	return CheckResult{Name: name, Status: "fail"}
}

func checkPostgreSQL() CheckResult {
	res := runner.Run("kubectl", "run", podName("pg"),
		"--rm", "-i", "--restart=Never",
		"--image=postgres:16-alpine", "-n", "doki-data",
		"--", "pg_isready",
		"-h", "postgres-postgresql.doki-data.svc.cluster.local",
		"-p", "5432", "-U", "postgres",
	)
	if res.Success() {
		return CheckResult{Name: "PostgreSQL connectivity", Status: "pass"}
	}
	return CheckResult{Name: "PostgreSQL connectivity", Status: "fail"}
}

func checkMinIO() CheckResult {
	return curlCheck("MinIO connectivity", "doki-data",
		"http://minio.doki-data.svc.cluster.local:9000/minio/health/live")
}

func checkRabbitMQ() CheckResult {
	return curlCheck("RabbitMQ management API", "doki-data",
		"http://rabbitmq.doki-data.svc.cluster.local:15672/api/overview")
}

func checkQdrant() CheckResult {
	return curlCheck("Qdrant REST API", "doki-data",
		"http://qdrant.doki-data.svc.cluster.local:6333/collections")
}

func checkDragonfly() CheckResult {
	res := runner.Run("kubectl", "run", podName("df"),
		"--rm", "-i", "--restart=Never",
		"--image=redis:7-alpine", "-n", "doki-data",
		"--", "redis-cli",
		"-h", "dragonfly.doki-data.svc.cluster.local",
		"-p", "6379", "PING",
	)
	if res.Success() && strings.Contains(res.Stdout, "PONG") {
		return CheckResult{Name: "Dragonfly PING", Status: "pass"}
	}
	return CheckResult{Name: "Dragonfly PING", Status: "fail"}
}

func checkOllama() CheckResult {
	res := runner.Run("kubectl", "run", podName("ollama"),
		"--rm", "-i", "--restart=Never",
		"--image=curlimages/curl:latest", "-n", "doki-ai",
		"--", "curl", "-sf",
		"http://ollama.doki-ai.svc.cluster.local:11434/api/tags", "-o", "/dev/null",
	)
	if res.Success() {
		return CheckResult{Name: "Ollama", Status: "pass"}
	}
	return CheckResult{Name: "Ollama (optional)", Status: "skip"}
}

func checkGrafana() CheckResult {
	return curlCheck("Grafana readiness", "doki-monitoring",
		"http://monitoring-grafana.doki-monitoring.svc.cluster.local:80/api/health")
}

func checkVault() CheckResult {
	res := runner.Run("kubectl", "run", podName("vault"),
		"--rm", "-i", "--restart=Never",
		"--image=curlimages/curl:latest", "-n", "doki-data",
		"--", "curl", "-sf",
		"http://vault.doki-data.svc.cluster.local:8200/v1/sys/seal-status",
	)
	if !res.Success() {
		return CheckResult{Name: "Vault status", Status: "fail"}
	}
	if strings.Contains(res.Stdout, `"sealed":false`) {
		return CheckResult{Name: "Vault unsealed", Status: "pass"}
	}
	if strings.Contains(res.Stdout, `"sealed":true`) {
		return CheckResult{Name: "Vault sealed", Status: "fail", Detail: "vault is sealed"}
	}
	return CheckResult{Name: "Vault status", Status: "fail"}
}

func checkKong() CheckResult {
	return curlCheck("Kong admin API", "doki-system",
		"http://kong-kong-admin.doki-system.svc.cluster.local:8001/status")
}
