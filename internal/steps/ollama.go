package steps

import (
	"fmt"
	"os"
	"strings"

	"github.com/doki-stack/infrastructure/internal/runner"
)

func SetupOllamaLocal(infraPath string, models string) error {
	if err := writeOllamaKustomization(infraPath, "./local"); err != nil {
		return err
	}

	res := runner.Run("kubectl", "apply", "-k", infraPath+"/base/ollama")
	if !res.Success() {
		return fmt.Errorf("apply ollama: %s", res.Stderr)
	}

	res = runner.Run("kubectl", "wait",
		"--for=condition=ready", "pod", "-l", "app=ollama",
		"-n", "doki-ai", "--timeout=300s",
	)
	if !res.Success() {
		return fmt.Errorf("ollama readiness: %s", res.Stderr)
	}

	for _, model := range strings.Fields(models) {
		res = runner.Run("kubectl", "exec", "-n", "doki-ai", "ollama-0", "--",
			"ollama", "pull", model,
		)
		if !res.Success() {
			return fmt.Errorf("pull model %s: %s", model, res.Stderr)
		}
	}

	return nil
}

func SetupOllamaExternal(infraPath string) error {
	if err := writeOllamaKustomization(infraPath, "./external"); err != nil {
		return err
	}

	hostIP := detectHostIP()
	if hostIP == "" {
		return fmt.Errorf("could not detect host IP for Ollama endpoints")
	}

	res := runner.Run("kubectl", "apply", "-k", infraPath+"/base/ollama")
	if !res.Success() {
		return fmt.Errorf("apply ollama: %s", res.Stderr)
	}

	patch := fmt.Sprintf(
		`{"subsets":[{"addresses":[{"ip":"%s"}],"ports":[{"port":11434,"protocol":"TCP"}]}]}`,
		hostIP,
	)
	res = runner.Run("kubectl", "patch", "endpoints", "ollama",
		"-n", "doki-ai", "--type=merge", "-p", patch,
	)
	if !res.Success() {
		return fmt.Errorf("patch endpoints: %s", res.Stderr)
	}

	return nil
}

func writeOllamaKustomization(infraPath, ref string) error {
	content := fmt.Sprintf(`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - %s
`, ref)
	return os.WriteFile(infraPath+"/base/ollama/kustomization.yaml", []byte(content), 0644)
}

func SetupVLLM(infraPath string) error {
	res := runner.Run("kubectl", "apply", "-k", infraPath+"/base/vllm")
	if !res.Success() {
		return fmt.Errorf("apply vllm: %s", res.Stderr)
	}

	res = runner.Run("kubectl", "wait",
		"--for=condition=available", "deployment/vllm",
		"-n", "doki-ai", "--timeout=600s",
	)
	if !res.Success() {
		return fmt.Errorf("vllm readiness (may need GPU nodes): %s", res.Stderr)
	}

	return nil
}

func detectHostIP() string {
	if runner.CommandExists("docker") {
		res := runner.Run("docker", "network", "inspect", "kind",
			"-f", "{{range .IPAM.Config}}{{.Gateway}}{{end}}")
		if res.Success() && res.Stdout != "" {
			return strings.TrimSpace(res.Stdout)
		}
	}
	res := runner.RunShell("ip route | grep default | awk '{print $3}' | head -1")
	if res.Success() && res.Stdout != "" {
		return strings.TrimSpace(res.Stdout)
	}
	return ""
}
