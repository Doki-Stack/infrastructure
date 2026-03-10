package cli

import (
	"fmt"
	"os"

	"github.com/doki-stack/infrastructure/internal/steps"
	"github.com/doki-stack/infrastructure/internal/ui"
	"github.com/spf13/cobra"
)

func newHealthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Run health checks on all Doki Stack services",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(ui.SectionHeader("Doki Stack Health Check"))

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
				os.Exit(1)
			}
			return nil
		},
	}
}
