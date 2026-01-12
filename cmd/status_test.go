// cmd/status_test.go
package cmd

import (
	"testing"
)

func TestStatusCommand(t *testing.T) {
	// Test that status command exists and has correct flags
	cmd := statusCmd

	if cmd.Use != "status" {
		t.Errorf("expected Use 'status', got %s", cmd.Use)
	}

	// Verify json flag exists on root
	jsonFlag := rootCmd.PersistentFlags().Lookup("json")
	if jsonFlag == nil {
		t.Error("expected --json flag on root command")
	}
}
