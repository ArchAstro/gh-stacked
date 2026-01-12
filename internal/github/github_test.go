// internal/github/github_test.go
package github

import "testing"

func TestNewClient(t *testing.T) {
	// This test verifies the client can be created
	// Actual API calls would require mocking
	c, err := NewClient()
	if err != nil {
		t.Skipf("Skipping test - gh auth not configured: %v", err)
	}
	if c == nil {
		t.Error("expected non-nil client")
	}
}
