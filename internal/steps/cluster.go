package steps

import (
	"fmt"
	"strings"

	"github.com/doki-stack/infrastructure/internal/runner"
)

const ClusterName = "doki-stack"

func ClusterExists() bool {
	res := runner.Run("kind", "get", "clusters")
	if !res.Success() {
		return false
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if strings.TrimSpace(line) == ClusterName {
			return true
		}
	}
	return false
}

func CreateCluster(infraPath string) error {
	if ClusterExists() {
		return nil
	}

	res := runner.Run("kind", "create", "cluster",
		"--name", ClusterName,
		"--config", infraPath+"/cluster/kind-config.yaml",
	)
	if !res.Success() {
		return fmt.Errorf("kind create cluster failed: %s", res.Stderr)
	}
	return nil
}

func DeleteCluster() error {
	res := runner.Run("kind", "delete", "cluster", "--name", ClusterName)
	if !res.Success() {
		return fmt.Errorf("kind delete cluster failed: %s", res.Stderr)
	}
	return nil
}
