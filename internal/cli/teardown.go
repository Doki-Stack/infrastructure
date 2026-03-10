package cli

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/doki-stack/infrastructure/internal/steps"
	"github.com/doki-stack/infrastructure/internal/ui"
	"github.com/spf13/cobra"
)

func newTeardownCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "teardown",
		Short: "Delete the Doki Stack kind cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !steps.ClusterExists() {
				fmt.Println(ui.Warn("Cluster 'doki-stack' does not exist"))
				return nil
			}

			if !force {
				var confirm bool
				err := huh.NewConfirm().
					Title("Delete the 'doki-stack' kind cluster?").
					Description("This will destroy all data and workloads in the cluster.").
					Affirmative("Yes, delete").
					Negative("Cancel").
					Value(&confirm).
					Run()

				if err != nil || !confirm {
					fmt.Println(ui.DimStyle.Render("Aborted."))
					return nil
				}
			}

			if err := ui.RunWithSpinner("Deleting cluster...", func() error {
				return steps.DeleteCluster()
			}); err != nil {
				return err
			}

			fmt.Println(ui.Pass("Cluster 'doki-stack' deleted"))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	return cmd
}
