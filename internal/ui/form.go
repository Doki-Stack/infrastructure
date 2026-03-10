package ui

import (
	"github.com/charmbracelet/huh"
)

type SetupConfig struct {
	Profile        string // "minimal", "recommended", "full", "custom"
	InstallCilium  bool
	InstallVault   bool
	InstallMonitor bool
	OllamaMode     string // "local", "external", "skip"
	OllamaModels   string
	RunHealthCheck bool
}

func DefaultSetupConfig() SetupConfig {
	return SetupConfig{
		Profile:        "recommended",
		InstallCilium:  true,
		InstallVault:   true,
		InstallMonitor: true,
		OllamaMode:     "local",
		OllamaModels:   "qwen2.5-coder:14b nomic-embed-text",
		RunHealthCheck: true,
	}
}

func (c SetupConfig) WantsCilium() bool {
	return c.Profile == "full" || c.Profile == "recommended" || (c.Profile == "custom" && c.InstallCilium)
}

func (c SetupConfig) WantsVault() bool {
	return c.Profile == "full" || c.Profile == "recommended" || (c.Profile == "custom" && c.InstallVault)
}

func (c SetupConfig) WantsMonitoring() bool {
	return c.Profile == "full" || c.Profile == "recommended" || (c.Profile == "custom" && c.InstallMonitor)
}

func RunSetupForm() (SetupConfig, error) {
	cfg := DefaultSetupConfig()

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Setup Profile").
				Description("All profiles include the kind cluster, namespaces, and all 5 core data services\n(PostgreSQL, MinIO, Qdrant, Dragonfly, RabbitMQ).").
				Options(
					huh.NewOption("Minimal  — Core services only (~8 GB RAM)", "minimal"),
					huh.NewOption("Recommended  — Core + Vault + Monitoring (~12 GB RAM)", "recommended"),
					huh.NewOption("Full  — Recommended + Cilium CNI + Network Policies", "full"),
					huh.NewOption("Custom  — Pick individual components", "custom"),
				).
				Value(&cfg.Profile),
		).Title("Profile"),

		huh.NewGroup(
			huh.NewConfirm().
				Title("Install Cilium CNI?").
				Description("Replaces kube-proxy with Cilium for networking and network policies").
				Value(&cfg.InstallCilium),
			huh.NewConfirm().
				Title("Install Vault?").
				Description("HashiCorp Vault in dev mode for secrets management").
				Value(&cfg.InstallVault),
			huh.NewConfirm().
				Title("Install monitoring stack?").
				Description("Prometheus, Grafana, Loki, and Tempo (~2 GB additional RAM)").
				Value(&cfg.InstallMonitor),
		).Title("Custom Components").WithHideFunc(func() bool {
			return cfg.Profile != "custom"
		}),

		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Ollama LLM setup").
				Description("Local runs Ollama as a pod inside the cluster.\nExternal points to Ollama running on the host machine.").
				Options(
					huh.NewOption("Local  — Run Ollama inside the cluster", "local"),
					huh.NewOption("External  — Connect to host Ollama", "external"),
					huh.NewOption("Skip  — Set up LLM later", "skip"),
				).
				Value(&cfg.OllamaMode),
		).Title("LLM Infrastructure"),

		huh.NewGroup(
			huh.NewInput().
				Title("Models to pull").
				Description("Space-separated list of Ollama models to download").
				Value(&cfg.OllamaModels),
		).Title("LLM Models").WithHideFunc(func() bool {
			return cfg.OllamaMode != "local"
		}),

		huh.NewGroup(
			huh.NewConfirm().
				Title("Run health check after setup?").
				Description("Validates connectivity to all installed services").
				Value(&cfg.RunHealthCheck),
		).Title("Post-Setup"),
	)

	err := form.Run()
	return cfg, err
}
