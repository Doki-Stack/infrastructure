package ui

import (
	"github.com/charmbracelet/huh"
)

type SetupConfig struct {
	CreateCluster  bool
	InstallCilium  bool
	DataServices   []string
	InstallVault   bool
	InstallMonitor bool
	OllamaMode     string // "local", "external", "skip"
	OllamaModels   string
	RunHealthCheck bool
}

func DefaultSetupConfig() SetupConfig {
	return SetupConfig{
		CreateCluster:  true,
		InstallCilium:  true,
		DataServices:   []string{"postgresql", "minio", "qdrant", "dragonfly", "rabbitmq"},
		InstallVault:   true,
		InstallMonitor: true,
		OllamaMode:     "local",
		OllamaModels:   "qwen2.5-coder:14b nomic-embed-text",
		RunHealthCheck: true,
	}
}

func RunSetupForm() (SetupConfig, error) {
	cfg := DefaultSetupConfig()

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Create kind cluster?").
				Description("Creates a new 'doki-stack' kind cluster (skips if already exists)").
				Value(&cfg.CreateCluster),
		).Title("Cluster"),

		huh.NewGroup(
			huh.NewConfirm().
				Title("Install Cilium CNI?").
				Description("Cilium replaces the default kube-proxy for networking and network policies").
				Value(&cfg.InstallCilium),
		).Title("Networking"),

		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Which data services to install?").
				Options(
					huh.NewOption("PostgreSQL", "postgresql").Selected(true),
					huh.NewOption("MinIO (object storage)", "minio").Selected(true),
					huh.NewOption("Qdrant (vector DB)", "qdrant").Selected(true),
					huh.NewOption("Dragonfly (Redis-compatible cache)", "dragonfly").Selected(true),
					huh.NewOption("RabbitMQ (message queue)", "rabbitmq").Selected(true),
				).
				Value(&cfg.DataServices),
		).Title("Data Services"),

		huh.NewGroup(
			huh.NewConfirm().
				Title("Install Vault?").
				Description("HashiCorp Vault in dev mode for secrets management").
				Value(&cfg.InstallVault),
		).Title("Security"),

		huh.NewGroup(
			huh.NewConfirm().
				Title("Install monitoring stack?").
				Description("Prometheus, Grafana, Loki, and Tempo").
				Value(&cfg.InstallMonitor),
		).Title("Monitoring"),

		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Ollama LLM setup").
				Description("Local runs Ollama as a pod inside the cluster. External points to host.").
				Options(
					huh.NewOption("Local (in-cluster pod)", "local"),
					huh.NewOption("External (host machine)", "external"),
					huh.NewOption("Skip", "skip"),
				).
				Value(&cfg.OllamaMode),

			huh.NewInput().
				Title("Models to pull").
				Description("Space-separated list of Ollama models").
				Value(&cfg.OllamaModels),
		).Title("LLM Infrastructure"),

		huh.NewGroup(
			huh.NewConfirm().
				Title("Run health check after setup?").
				Value(&cfg.RunHealthCheck),
		).Title("Post-Setup"),
	)

	err := form.Run()
	return cfg, err
}
