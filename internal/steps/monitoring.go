package steps

import (
	"fmt"

	"github.com/doki-stack/infrastructure/internal/runner"
)

func InstallMonitoring(infraPath string) error {
	runner.Run("helm", "repo", "add", "prometheus-community", "https://prometheus-community.github.io/helm-charts")
	runner.Run("helm", "repo", "add", "grafana", "https://grafana.github.io/helm-charts")
	runner.Run("helm", "repo", "update")

	res := runner.Run("helm", "upgrade", "--install", "monitoring",
		"prometheus-community/kube-prometheus-stack",
		"-n", "doki-monitoring", "-f", infraPath+"/helm-values/prometheus.yaml",
		"--create-namespace", "--wait",
	)
	if !res.Success() {
		return fmt.Errorf("kube-prometheus-stack: %s", res.Stderr)
	}

	res = runner.Run("helm", "upgrade", "--install", "loki", "grafana/loki",
		"-n", "doki-monitoring", "-f", infraPath+"/helm-values/loki.yaml", "--wait",
	)
	if !res.Success() {
		return fmt.Errorf("loki: %s", res.Stderr)
	}

	res = runner.Run("helm", "upgrade", "--install", "tempo", "grafana/tempo",
		"-n", "doki-monitoring", "-f", infraPath+"/helm-values/tempo.yaml", "--wait",
	)
	if !res.Success() {
		return fmt.Errorf("tempo: %s", res.Stderr)
	}

	return nil
}
