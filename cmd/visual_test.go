// cmd/visual_test.go
package cmd

import (
	"testing"

	"github.com/ArchAstro/gh-stacked/internal/git"
	"github.com/ArchAstro/gh-stacked/internal/github"
)

func TestVisualCommand(t *testing.T) {
	cmd := visualCmd

	if cmd.Use != "show" {
		t.Errorf("expected Use 'show', got %s", cmd.Use)
	}
}

func TestRunVisual_WithPRs(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234567890", Subject: "Add user model"},
		{Hash: "def5678901234", Subject: "Add user API"},
		{Hash: "ghi9012345678", Subject: "Add user tests"},
	}
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
		"stack/feature/auth/02-add-user-api",
		"stack/feature/auth/03-add-user-tests",
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 101, "Add user API")
	mockGH.AddExistingPR("stack/feature/auth/03-add-user-tests", 102, "Add user tests")

	oldNewGit := visualNewGit
	oldNewGitHub := visualNewGitHub
	defer func() {
		visualNewGit = oldNewGit
		visualNewGitHub = oldNewGitHub
	}()

	visualNewGit = func() git.GitOperations { return mockGit }
	visualNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runVisual(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !mockGit.WasCalled("GetCurrentBranch") {
		t.Error("expected GetCurrentBranch to be called")
	}
	if !mockGit.WasCalled("GetBaseBranch") {
		t.Error("expected GetBaseBranch to be called")
	}
	if !mockGit.WasCalled("GetCommitsSinceBase") {
		t.Error("expected GetCommitsSinceBase to be called")
	}

	listAllCalls := 0
	for _, call := range mockGH.Calls {
		if call.Method == "ListAllPullRequests" {
			listAllCalls++
		}
	}
	if listAllCalls != 3 {
		t.Errorf("expected 3 ListAllPullRequests calls, got %d", listAllCalls)
	}
}
