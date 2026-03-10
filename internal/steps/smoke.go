package steps

import (
	"fmt"
	"strings"

	"github.com/doki-stack/infrastructure/internal/runner"
)

func RunSmokeTests() []CheckResult {
	var results []CheckResult
	results = append(results, checkDeployments())
	results = append(results, checkServicesEndpoints()...)
	results = append(results, checkPVCs())
	results = append(results, checkKongRoute())
	return results
}

func checkDeployments() CheckResult {
	res := runner.Run("kubectl", "get", "deploy", "-A",
		"-l", "app.kubernetes.io/part-of=doki-stack",
		"-o", `jsonpath={range .items[*]}{.metadata.namespace}/{.metadata.name} {.status.readyReplicas}/{.spec.replicas}{"\n"}{end}`,
	)
	if !res.Success() {
		return CheckResult{Name: "Deployments ready", Status: "fail", Detail: "could not list"}
	}

	var bad []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		rp := strings.Split(parts[1], "/")
		if len(rp) == 2 && rp[1] != "0" && rp[0] != rp[1] {
			bad = append(bad, parts[0])
		}
	}

	if len(bad) > 0 {
		return CheckResult{
			Name: "All deployments have ready replicas", Status: "fail",
			Detail: strings.Join(bad, ", "),
		}
	}
	return CheckResult{Name: "All deployments have ready replicas", Status: "pass"}
}

var smokeNamespaces = []string{
	"doki-data", "doki-mcp", "doki-platform", "doki-agents",
	"doki-system", "doki-monitoring", "doki-ai",
}

func checkServicesEndpoints() []CheckResult {
	var results []CheckResult
	allGood := true

	for _, ns := range smokeNamespaces {
		res := runner.Run("kubectl", "get", "svc", "-n", ns, "-o", "name")
		if !res.Success() {
			continue
		}
		for _, svcName := range strings.Split(res.Stdout, "\n") {
			svcName = strings.TrimSpace(svcName)
			if svcName == "" {
				continue
			}

			sel := runner.Run("kubectl", "get", svcName, "-n", ns,
				"-o", "jsonpath={.spec.selector}")
			if !sel.Success() || sel.Stdout == "" || sel.Stdout == "{}" {
				continue
			}

			ep := runner.Run("kubectl", "get", svcName, "-n", ns,
				"-o", "jsonpath={.subsets[*].addresses[*].ip}")
			if !ep.Success() || ep.Stdout == "" {
				results = append(results, CheckResult{
					Name:   fmt.Sprintf("No endpoints: %s (ns %s)", svcName, ns),
					Status: "fail",
				})
				allGood = false
			}
		}
	}

	if allGood {
		results = append(results, CheckResult{
			Name: "All services have endpoints", Status: "pass",
		})
	}
	return results
}

func checkPVCs() CheckResult {
	res := runner.Run("kubectl", "get", "pvc", "-A",
		"-o", `jsonpath={range .items[?(@.status.phase!="Bound")]}{.metadata.namespace}/{.metadata.name} {.status.phase}{"\n"}{end}`,
	)
	if !res.Success() {
		return CheckResult{Name: "All PVCs bound", Status: "fail", Detail: "could not list"}
	}
	unbound := strings.TrimSpace(res.Stdout)
	if unbound != "" {
		return CheckResult{Name: "All PVCs bound", Status: "fail", Detail: unbound}
	}
	return CheckResult{Name: "All PVCs are bound", Status: "pass"}
}

func checkKongRoute() CheckResult {
	res := runner.RunShell(`curl -sf -o /dev/null -w "%{http_code}" http://localhost:30080/health 2>/dev/null || echo "000"`)
	code := strings.TrimSpace(res.Stdout)
	if code == "200" || code == "404" {
		return CheckResult{Name: "Kong routes reachable", Status: "pass"}
	}
	return CheckResult{Name: "Kong routes reachable", Status: "fail", Detail: "HTTP " + code}
}
