package cli

import (
	"fmt"
	"os"

	"github.com/doki-stack/infrastructure/internal/steps"
	"github.com/doki-stack/infrastructure/internal/ui"
	"github.com/spf13/cobra"
)

func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run infrastructure tests",
	}

	cmd.AddCommand(newSmokeCmd(), newValidateCmd())
	return cmd
}

func newSmokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "smoke",
		Short: "Run smoke tests (deployments, services, PVCs, routes)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(ui.SectionHeader("Smoke Tests"))

			var results []steps.CheckResult
			if err := ui.RunWithSpinner("Running smoke tests...", func() error {
				results = steps.RunSmokeTests()
				return nil
			}); err != nil {
				return err
			}

			fmt.Println()
			for _, r := range results {
				fmt.Println("  " + r.Render())
			}

			return exitOnFailures(results)
		},
	}
}

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Run validation tests (DNS, connectivity, data service CRUD)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(ui.SectionHeader("Infrastructure Validation"))

			var results []steps.CheckResult
			if err := ui.RunWithSpinner("Running validation tests...", func() error {
				results = steps.RunValidationTests()
				return nil
			}); err != nil {
				return err
			}

			fmt.Println()
			for _, r := range results {
				fmt.Println("  " + r.Render())
			}

			return exitOnFailures(results)
		},
	}
}

func exitOnFailures(results []steps.CheckResult) error {
	var failed int
	for _, r := range results {
		if r.Status == "fail" {
			failed++
		}
	}

	fmt.Println()
	if failed == 0 {
		fmt.Println(ui.Pass("All tests passed"))
	} else {
		fmt.Println(ui.Fail(fmt.Sprintf("%d test(s) failed", failed)))
		os.Exit(1)
	}
	return nil
}
