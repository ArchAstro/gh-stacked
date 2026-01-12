package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	pushPreview bool
	pushRegroup bool
	pushSquash  []string
)

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Create or update stack of PRs from commits",
	Long: `Create or update a stack of PRs from commits on current branch.

Each commit becomes a separate PR, with each PR targeting the previous one.
This creates clean diffs for reviewers - each PR shows only its commit's changes.

The command is idempotent:
  - New commits create new PRs
  - Changed commits update existing PRs
  - Removed commits close orphaned PRs (with confirmation)`,
	Example: `  # Push current commits as stacked PRs
  gh stacked push

  # Preview without creating PRs
  gh stacked push --preview

  # Squash commits 2-4 into one PR before pushing
  gh stacked push --squash 2-4

  # Interactive reorganization
  gh stacked push --regroup`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: Implement push command
		fmt.Println("push command not yet implemented")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(pushCmd)

	pushCmd.Flags().BoolVar(&pushPreview, "preview", false, "Show what would happen without making changes")
	pushCmd.Flags().BoolVar(&pushRegroup, "regroup", false, "Interactively reorganize commits before pushing")
	pushCmd.Flags().StringArrayVar(&pushSquash, "squash", nil, "Squash commits N-M into single PR (e.g., --squash 2-4)")
}
