package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var mergeNoRebase bool

var mergeCmd = &cobra.Command{
	Use:   "merge <pr-number>",
	Short: "Merge a PR and update the remaining stack",
	Long: `Merge a PR and update the remaining stack.

This command:
  1. Merges the specified PR via GitHub API
  2. Retargets the next PR to main
  3. Rebases remaining local commits onto main
  4. Force-pushes all remaining PR branches
  5. Updates PR body footers

PRs must be merged in order - you cannot merge PR 2 before PR 1.`,
	Example: `  # Merge the first PR when ready
  gh stacked merge 1

  # Merge without rebasing the rest (manual sync later)
  gh stacked merge 1 --no-rebase`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: Implement merge command
		fmt.Printf("merge command not yet implemented (PR %s)\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(mergeCmd)

	mergeCmd.Flags().BoolVar(&mergeNoRebase, "no-rebase", false, "Just merge, don't rebase remaining stack")
}
