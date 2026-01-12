package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current stack state with PR links",
	Long: `Show the current state of your PR stack.

Displays each PR with:
  - Position in stack
  - Merge status (MERGEABLE, BLOCKED, WAITING)
  - PR link
  - Target branch
  - Diff stats
  - Review status`,
	Example: `  # Show stack status
  gh stacked status

  # Machine-readable output
  gh stacked status --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: Implement status command
		fmt.Println("status command not yet implemented")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
