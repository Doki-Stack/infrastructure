package cli

import (
	"fmt"
	"strings"

	"github.com/doki-stack/infrastructure/internal/runner"
	"github.com/doki-stack/infrastructure/internal/steps"
	"github.com/doki-stack/infrastructure/internal/ui"
	"github.com/spf13/cobra"
)

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
				fmt.Println(ui.DimStyle.Render("Running with --all defaults (non-interactive)"))
			} else {
				var err error
				cfg, err = ui.RunSetupForm()
				if err != nil {
					return fmt.Errorf("setup cancelled: %w", err)
				}
			}

			return runSetup(cfg)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Install everything with defaults (non-interactive)")
	return cmd
}

func runSetup(cfg ui.SetupConfig) error {
	path := InfraPath()

	if cfg.CreateCluster {
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
	}

	if cfg.InstallCilium {
		if err := ui.RunWithSpinner("Installing Cilium CNI...", func() error {
			return steps.InstallCilium()
		}); err != nil {
			return err
		}
		fmt.Println(ui.Pass("Cilium installed and ready"))
	}

	if err := ui.RunWithSpinner("Applying namespaces...", func() error {
		return steps.ApplyNamespaces(path)
	}); err != nil {
		return err
	}
	fmt.Println(ui.Pass("Namespaces applied"))

	if len(cfg.DataServices) > 0 {
		names := steps.DataServiceNames(cfg.DataServices)
		label := "Installing data services (" + strings.Join(names, ", ") + ")..."
		if err := ui.RunWithSpinner(label, func() error {
			return steps.InstallDataServices(path, cfg.DataServices)
		}); err != nil {
			return err
		}
		fmt.Println(ui.Pass("Data services installed"))
	}

	if cfg.InstallVault {
		if err := ui.RunWithSpinner("Installing Vault...", func() error {
			return steps.InstallVault(path)
		}); err != nil {
			return err
		}
		fmt.Println(ui.Pass("Vault installed"))
	}

	if cfg.InstallMonitor {
		if err := ui.RunWithSpinner("Installing monitoring stack...", func() error {
			return steps.InstallMonitoring(path)
		}); err != nil {
			return err
		}
		fmt.Println(ui.Pass("Monitoring stack installed"))
	}

	switch cfg.OllamaMode {
	case "local":
		if err := ui.RunWithSpinner("Setting up Ollama (local)...", func() error {
			return steps.SetupOllamaLocal(path, cfg.OllamaModels)
		}); err != nil {
			return err
		}
		fmt.Println(ui.Pass("Ollama running locally in cluster"))
	case "external":
		if err := ui.RunWithSpinner("Setting up Ollama (external)...", func() error {
			return steps.SetupOllamaExternal(path)
		}); err != nil {
			fmt.Println(ui.Warn("Ollama external setup failed — host Ollama may not be running"))
		} else {
			fmt.Println(ui.Pass("Ollama configured (external host)"))
		}
	case "skip":
		fmt.Println(ui.Skip("Ollama setup skipped"))
	}

	if cfg.RunHealthCheck {
		fmt.Println(ui.SectionHeader("Health Check"))
		results := steps.RunAllHealthChecks()
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
