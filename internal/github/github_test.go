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

func TestCreatePullRequestInput(t *testing.T) {
	// Test the PR input struct construction (no actual API call)
	input := CreatePullRequestInput{
		Title:  "Add User model",
		Body:   "Description here",
		Head:   "stack/feature/01-add-user",
		Base:   "main",
	}

	if input.Title != "Add User model" {
		t.Errorf("unexpected title: %s", input.Title)
	}
}
