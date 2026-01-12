package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var jsonOutput bool

var rootCmd = &cobra.Command{
	Use:   "gh-stacked",
	Short: "Manage stacked PRs from your commit history",
	Long: `Manage stacked PRs from your commit history.

Each commit on your branch becomes a separate PR, chained together
so reviewers see clean, incremental diffs.

Examples:
  # Create a stack from your branch
  gh stacked push

  # Preview what would be created
  gh stacked push --preview

  # Fix PR 2 after code review
  git add .
  gh stacked amend 2

  # Merge the first PR when ready
  gh stacked merge 1

  # Check stack state
  gh stacked status`,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
}
