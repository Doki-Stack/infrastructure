package steps

import (
	"fmt"

	"github.com/doki-stack/infrastructure/internal/runner"
)

func InstallCilium() error {
	runner.Run("helm", "repo", "add", "cilium", "https://helm.cilium.io")
	runner.Run("helm", "repo", "update", "cilium")

	res := runner.Run("helm", "upgrade", "--install", "cilium", "cilium/cilium",
		"--namespace", "kube-system", "--wait",
	)
	if !res.Success() {
		return fmt.Errorf("cilium install failed: %s", res.Stderr)
	}

	res = runner.Run("kubectl", "wait",
		"--for=condition=Ready", "pods", "-l", "k8s-app=cilium",
		"-n", "kube-system", "--timeout=300s",
	)
	if !res.Success() {
		return fmt.Errorf("cilium readiness wait failed: %s", res.Stderr)
	}

	res = runner.Run("kubectl", "wait",
		"--for=condition=Ready", "nodes", "--all", "--timeout=300s",
	)
	if !res.Success() {
		return fmt.Errorf("node readiness wait failed: %s", res.Stderr)
	}

	return nil
}
