package steps

import (
	"fmt"

	"github.com/doki-stack/infrastructure/internal/runner"
)

func ApplyNamespaces(infraPath string) error {
	res := runner.Run("kubectl", "apply", "-f", infraPath+"/cluster/namespaces.yaml")
	if !res.Success() {
		return fmt.Errorf("apply namespaces failed: %s", res.Stderr)
	}
	return nil
}
