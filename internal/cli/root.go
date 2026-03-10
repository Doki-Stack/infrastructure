package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	infraPath string
)

func InfraPath() string {
	return infraPath
}

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "dokictl",
		Short:   "Doki Stack infrastructure management CLI",
		Long:    "Interactive CLI for setting up, managing, and testing the Doki Stack Kubernetes infrastructure.",
		Version: Version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if infraPath != "" {
				abs, err := filepath.Abs(infraPath)
				if err != nil {
					return fmt.Errorf("invalid infra path: %w", err)
				}
				infraPath = abs
				return nil
			}

			resolved, err := resolveInfraPath()
			if err != nil {
				return fmt.Errorf("could not find infrastructure repo root: %w\nUse --infra-path to specify it manually", err)
			}
			infraPath = resolved
			return nil
		},
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&infraPath, "infra-path", "", "Path to infrastructure repo root (auto-detected if omitted)")

	root.AddCommand(
		newSetupCmd(),
		newTeardownCmd(),
		newHealthCmd(),
		newTestCmd(),
	)

	return root
}

// resolveInfraPath walks up from the current working directory looking for
// cluster/kind-config.yaml, which is the marker for the infrastructure repo root.
func resolveInfraPath() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		marker := filepath.Join(dir, "cluster", "kind-config.yaml")
		if _, err := os.Stat(marker); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	exe, err := os.Executable()
	if err == nil {
		dir = filepath.Dir(exe)
		for range 5 {
			marker := filepath.Join(dir, "cluster", "kind-config.yaml")
			if _, err := os.Stat(marker); err == nil {
				return dir, nil
			}
			dir = filepath.Dir(dir)
		}
	}

	return "", fmt.Errorf("no infrastructure repo found (looked for cluster/kind-config.yaml)")
}
