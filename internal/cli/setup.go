package cli

import (
	"fmt"

	"github.com/doki-stack/infrastructure/internal/runner"
	"github.com/doki-stack/infrastructure/internal/steps"
	"github.com/doki-stack/infrastructure/internal/ui"
	"github.com/spf13/cobra"
)

var allDataServices = []string{"postgresql", "minio", "qdrant", "dragonfly", "rabbitmq"}

func newSetupCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up the Doki Stack Kubernetes cluster",
		Long:  "Interactive wizard to create and configure a kind cluster with all infrastructure components.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runner.RequireCommands("kubectl", "helm", "kind"); err != nil {
				return err
			}

			fmt.Println(ui.PrintBanner())
			fmt.Println()

			var cfg ui.SetupConfig
			if all {
				cfg = ui.DefaultSetupConfig()
				fmt.Println(ui.DimStyle.Render("Running with --all defaults (non-interactive, recommended profile)"))
				fmt.Println()
			} else {
				var err error
				cfg, err = ui.RunSetupForm()
				if err != nil {
					return fmt.Errorf("setup cancelled: %w", err)
				}
				fmt.Println()
			}

			return runSetup(cfg)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Install everything with recommended defaults (non-interactive)")
	return cmd
}

func runSetup(cfg ui.SetupConfig) error {
	path := InfraPath()

	profileDesc := map[string]string{
		"minimal":     "Minimal",
		"recommended": "Recommended",
		"full":        "Full",
		"custom":      "Custom",
	}
	fmt.Println(ui.StepHeader("Profile: " + profileDesc[cfg.Profile]))
	fmt.Println()

	// --- Cluster ---
	if steps.ClusterExists() {
		fmt.Println(ui.Warn("Cluster 'doki-stack' already exists, skipping creation"))
	} else {
		if err := ui.RunWithSpinner("Creating kind cluster...", func() error {
			return steps.CreateCluster(path)
		}); err != nil {
			return err
		}
		fmt.Println(ui.Pass("Kind cluster created"))
	}

	// --- Cilium (before namespaces, as CNI must be ready) ---
	if cfg.WantsCilium() {
		if err := ui.RunWithSpinner("Installing Cilium CNI...", func() error {
			return steps.InstallCilium()
		}); err != nil {
			return err
		}
		fmt.Println(ui.Pass("Cilium installed and ready"))
	} else {
		fmt.Println(ui.Skip("Cilium CNI"))
	}

	// --- Namespaces (always) ---
	if err := ui.RunWithSpinner("Applying namespaces...", func() error {
		return steps.ApplyNamespaces(path)
	}); err != nil {
		return err
	}
	fmt.Println(ui.Pass("Namespaces applied"))

	// --- Data services (always — all 5 are required) ---
	if err := ui.RunWithSpinner("Installing data services (PostgreSQL, MinIO, Qdrant, Dragonfly, RabbitMQ)...", func() error {
		return steps.InstallDataServices(path, allDataServices)
	}); err != nil {
		return err
	}
	fmt.Println(ui.Pass("All data services installed"))

	// --- Vault ---
	if cfg.WantsVault() {
		if err := ui.RunWithSpinner("Installing Vault...", func() error {
			return steps.InstallVault(path)
		}); err != nil {
			return err
		}
		fmt.Println(ui.Pass("Vault installed"))
	} else {
		fmt.Println(ui.Skip("Vault"))
	}

	// --- Monitoring ---
	if cfg.WantsMonitoring() {
		if err := ui.RunWithSpinner("Installing monitoring stack (Prometheus, Grafana, Loki, Tempo)...", func() error {
			return steps.InstallMonitoring(path)
		}); err != nil {
			return err
		}
		fmt.Println(ui.Pass("Monitoring stack installed"))
	} else {
		fmt.Println(ui.Skip("Monitoring stack"))
	}

	// --- LLM ---
	if cfg.WantsOllama() {
		if cfg.OllamaIsLocal() {
			if err := ui.RunWithSpinner("Setting up Ollama (local)...", func() error {
				return steps.SetupOllamaLocal(path, cfg.OllamaModels)
			}); err != nil {
				return err
			}
			fmt.Println(ui.Pass("Ollama running locally in cluster"))
		} else if cfg.OllamaIsExternal() {
			if err := ui.RunWithSpinner("Setting up Ollama (external)...", func() error {
				return steps.SetupOllamaExternal(path)
			}); err != nil {
				fmt.Println(ui.Warn("Ollama external setup incomplete — ensure host Ollama is running with OLLAMA_HOST=0.0.0.0:11434"))
			} else {
				fmt.Println(ui.Pass("Ollama configured (external host)"))
			}
		}
	}

	if cfg.WantsVLLM() {
		if err := ui.RunWithSpinner("Deploying vLLM (production GPU inference)...", func() error {
			return steps.SetupVLLM(path)
		}); err != nil {
			fmt.Println(ui.Warn("vLLM deployment applied but may need GPU nodes to schedule: " + err.Error()))
		} else {
			fmt.Println(ui.Pass("vLLM deployed and ready"))
		}
	}

	if cfg.LLMBackend == "skip" {
		fmt.Println(ui.Skip("LLM setup"))
	}

	// --- Health check ---
	if cfg.RunHealthCheck {
		fmt.Println(ui.SectionHeader("Health Check"))
		var results []steps.CheckResult
		if err := ui.RunWithSpinner("Running health checks...", func() error {
			results = steps.RunAllHealthChecks()
			return nil
		}); err != nil {
			return err
		}
		fmt.Println()
		for _, r := range results {
			fmt.Println("  " + r.Render())
		}
		printSummary(results)
	}

	fmt.Println()
	fmt.Println(ui.PassStyle.Render("Setup complete!"))
	return nil
}

func printSummary(results []steps.CheckResult) {
	var failed int
	for _, r := range results {
		if r.Status == "fail" {
			failed++
		}
	}
	fmt.Println()
	if failed == 0 {
		fmt.Println(ui.Pass("All checks passed"))
	} else {
		fmt.Println(ui.Fail(fmt.Sprintf("%d check(s) failed", failed)))
	}
}
