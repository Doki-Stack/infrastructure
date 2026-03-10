package steps

import (
	"fmt"

	"github.com/doki-stack/infrastructure/internal/runner"
)

func InstallVault(infraPath string) error {
	runner.Run("helm", "repo", "add", "hashicorp", "https://helm.releases.hashicorp.com")
	runner.Run("helm", "repo", "update", "hashicorp")

	res := runner.Run("helm", "upgrade", "--install", "vault", "hashicorp/vault",
		"-n", "doki-data", "-f", infraPath+"/helm-values/vault.yaml", "--wait",
	)
	if !res.Success() {
		return fmt.Errorf("vault install failed: %s", res.Stderr)
	}
	return nil
}
