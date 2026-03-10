package steps

import (
	"fmt"

	"github.com/doki-stack/infrastructure/internal/runner"
)

type dataService struct {
	Name    string
	Install func(infraPath string) error
}

func dataServiceMap() map[string]dataService {
	return map[string]dataService{
		"postgresql": {Name: "PostgreSQL", Install: installPostgreSQL},
		"minio":      {Name: "MinIO", Install: installMinIO},
		"qdrant":     {Name: "Qdrant", Install: installQdrant},
		"dragonfly":  {Name: "Dragonfly", Install: installDragonfly},
		"rabbitmq":   {Name: "RabbitMQ", Install: installRabbitMQ},
	}
}

func DataServiceNames(keys []string) []string {
	m := dataServiceMap()
	var names []string
	for _, k := range keys {
		if svc, ok := m[k]; ok {
			names = append(names, svc.Name)
		}
	}
	return names
}

func InstallDataServices(infraPath string, selected []string) error {
	m := dataServiceMap()

	runner.Run("helm", "repo", "add", "bitnami", "https://charts.bitnami.com/bitnami")
	runner.Run("helm", "repo", "add", "qdrant", "https://qdrant.github.io/qdrant-helm")
	runner.Run("helm", "repo", "update")

	for _, key := range selected {
		svc, ok := m[key]
		if !ok {
			return fmt.Errorf("unknown data service: %s", key)
		}
		if err := svc.Install(infraPath); err != nil {
			return fmt.Errorf("%s install failed: %w", svc.Name, err)
		}
	}
	return nil
}

func installPostgreSQL(infraPath string) error {
	res := runner.Run("helm", "upgrade", "--install", "postgres", "bitnami/postgresql",
		"-n", "doki-data", "-f", infraPath+"/base/postgresql/values.yaml",
		"--version", "15", "--wait",
	)
	if !res.Success() {
		return fmt.Errorf("%s", res.Stderr)
	}
	return nil
}

func installMinIO(infraPath string) error {
	res := runner.Run("helm", "upgrade", "--install", "minio", "bitnami/minio",
		"-n", "doki-data", "-f", infraPath+"/base/minio/values.yaml",
		"--version", "14", "--wait",
	)
	if !res.Success() {
		return fmt.Errorf("%s", res.Stderr)
	}
	return nil
}

func installQdrant(infraPath string) error {
	res := runner.Run("helm", "upgrade", "--install", "qdrant", "qdrant/qdrant",
		"-n", "doki-data", "-f", infraPath+"/base/qdrant/values.yaml",
		"--version", "1.16", "--wait",
	)
	if !res.Success() {
		return fmt.Errorf("%s", res.Stderr)
	}
	return nil
}

func installDragonfly(infraPath string) error {
	res := runner.Run("helm", "upgrade", "--install", "dragonfly",
		"oci://ghcr.io/dragonflydb/dragonfly/helm",
		"-n", "doki-data", "-f", infraPath+"/base/dragonfly/values.yaml",
		"--version", "1.29.0", "--wait",
	)
	if !res.Success() {
		return fmt.Errorf("%s", res.Stderr)
	}
	return nil
}

func installRabbitMQ(infraPath string) error {
	res := runner.Run("kubectl", "apply", "-k", infraPath+"/base/rabbitmq")
	if !res.Success() {
		return fmt.Errorf("%s", res.Stderr)
	}
	res = runner.Run("kubectl", "wait",
		"--for=condition=ready", "pod",
		"-l", "app.kubernetes.io/name=rabbitmq",
		"-n", "doki-data", "--timeout=300s",
	)
	if !res.Success() {
		return fmt.Errorf("rabbitmq readiness wait: %s", res.Stderr)
	}
	return nil
}
