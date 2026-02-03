package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var Version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of gh-stacked",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if jsonOutput {
			out, err := json.Marshal(map[string]string{"version": Version})
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		}
		fmt.Printf("gh-stacked version %s\n", Version)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
