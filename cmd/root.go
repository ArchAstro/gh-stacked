package cmd

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var jsonOutput bool

var rootCmd = &cobra.Command{
	Use:   "stacked",
	Short: "Manage stacked PRs from your commit history",
	Long: `Manage stacked PRs from your commit history.

Each commit on your branch becomes a separate PR, chained together
so reviewers see clean, incremental diffs.

Examples:
  # Create a stack from your branch
  gh stacked push

  # Preview what would be created
  gh stacked push --mode=preview

  # Fix PR 2 after code review
  git add .
  gh stacked amend 2

  # Merge the first PR when ready
  gh stacked merge

  # Check stack state
  gh stacked status`,
}

func Execute() {
	rootCmd.Version = Version
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	// Customize usage template to show "gh stacked" instead of just "stacked"
	defaultUsageTemplate := rootCmd.UsageTemplate()
	ghUsageTemplate := strings.ReplaceAll(defaultUsageTemplate, "{{.UseLine}}", "gh {{.UseLine}}")
	ghUsageTemplate = strings.ReplaceAll(ghUsageTemplate, "{{.CommandPath}}", "gh {{.CommandPath}}")
	rootCmd.SetUsageTemplate(ghUsageTemplate)
}
