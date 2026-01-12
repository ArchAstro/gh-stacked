package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var amendCmd = &cobra.Command{
	Use:   "amend <pr-number>",
	Short: "Amend a specific PR with staged changes",
	Long: `Amend a specific PR in the stack with your staged changes.

This command:
  1. Creates a fixup commit targeting the specified PR's commit
  2. Runs an autosquash rebase to fold it in
  3. Force-pushes all affected PR branches

You must stage your changes before running this command.`,
	Example: `  # Fix PR 2 after code review
  git add .
  gh stacked amend 2`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: Implement amend command
		fmt.Printf("amend command not yet implemented (PR %s)\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(amendCmd)
}
