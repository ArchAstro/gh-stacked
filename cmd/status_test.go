// cmd/status_test.go
package cmd

import (
	"errors"
	"testing"

	"github.com/ArchAstro/gh-stacked/internal/git"
	"github.com/ArchAstro/gh-stacked/internal/github"
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

func TestRunStatus_WithAllPRs(t *testing.T) {
	// Scenario: 3 commits, all with PRs -> shows all PR info
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234567890", Subject: "Add user model"},
		{Hash: "def5678901234", Subject: "Add user API"},
		{Hash: "ghi9012345678", Subject: "Add user tests"},
	}
	// Set up remote branches that exist on origin
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
		"stack/feature/auth/02-add-user-api",
		"stack/feature/auth/03-add-user-tests",
	}

	mockGH := github.NewMockGitHub()
	// Add existing PRs for all 3 branches
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 101, "Add user API")
	mockGH.AddExistingPR("stack/feature/auth/03-add-user-tests", 102, "Add user tests")

	// Save and restore original DI functions
	oldNewGit := statusNewGit
	oldNewGitHub := statusNewGitHub
	defer func() {
		statusNewGit = oldNewGit
		statusNewGitHub = oldNewGitHub
	}()

	statusNewGit = func() git.GitOperations { return mockGit }
	statusNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runStatus(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify GetCurrentBranch was called
	if !mockGit.WasCalled("GetCurrentBranch") {
		t.Error("expected GetCurrentBranch to be called")
	}

	// Verify GetBaseBranch was called
	if !mockGit.WasCalled("GetBaseBranch") {
		t.Error("expected GetBaseBranch to be called")
	}

	// Verify GetCommitsSinceBase was called
	if !mockGit.WasCalled("GetCommitsSinceBase") {
		t.Error("expected GetCommitsSinceBase to be called")
	}

	// Verify ListAllPullRequests was called for each branch (3 times via AnalyzeStack)
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

func TestRunStatus_NoPRs(t *testing.T) {
	// Scenario: 2 commits, no remote branches yet -> shows pending status
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/payments"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234567890", Subject: "Add payment model"},
		{Hash: "def5678901234", Subject: "Add payment service"},
	}
	// No remote branches - simulates commits that haven't been pushed yet
	mockGit.RemoteBranches = nil

	mockGH := github.NewMockGitHub()
	// No PRs added

	// Save and restore original DI functions
	oldNewGit := statusNewGit
	oldNewGitHub := statusNewGitHub
	defer func() {
		statusNewGit = oldNewGit
		statusNewGitHub = oldNewGitHub
	}()

	statusNewGit = func() git.GitOperations { return mockGit }
	statusNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runStatus(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With no remote branches, ListAllPullRequests should not be called
	// (we only check PRs for branches that exist on remote)
	listAllCalls := 0
	for _, call := range mockGH.Calls {
		if call.Method == "ListAllPullRequests" {
			listAllCalls++
		}
	}
	if listAllCalls != 0 {
		t.Errorf("expected 0 ListAllPullRequests calls (no remote branches), got %d", listAllCalls)
	}
}

func TestRunStatus_NoCommits(t *testing.T) {
	// Scenario: No commits -> shows empty message
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/empty"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{} // No commits

	// Save and restore original DI functions
	oldNewGit := statusNewGit
	defer func() {
		statusNewGit = oldNewGit
	}()

	statusNewGit = func() git.GitOperations { return mockGit }

	err := runStatus(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify git operations were called
	if !mockGit.WasCalled("GetCurrentBranch") {
		t.Error("expected GetCurrentBranch to be called")
	}
	if !mockGit.WasCalled("GetBaseBranch") {
		t.Error("expected GetBaseBranch to be called")
	}
	if !mockGit.WasCalled("GetCommitsSinceBase") {
		t.Error("expected GetCommitsSinceBase to be called")
	}
}

func TestRunStatus_RejectsStackBranch(t *testing.T) {
	// Scenario: Running from stack branch -> error
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "stack/feature/auth/01-add-user-model"

	// Save and restore original DI functions
	oldNewGit := statusNewGit
	defer func() {
		statusNewGit = oldNewGit
	}()

	statusNewGit = func() git.GitOperations { return mockGit }

	err := runStatus(nil, []string{})
	if err == nil {
		t.Fatal("expected error when running from stack branch")
	}

	// Verify the error message mentions running from feature branch
	expectedMsg := "run status from your feature branch"
	if err.Error() != "run status from your feature branch, not a stack branch" {
		t.Errorf("expected error message to contain %q, got %q", expectedMsg, err.Error())
	}
}

func TestRunStatus_MixedPRStatus(t *testing.T) {
	// Scenario: Some commits have PRs, some are new (no remote branches)
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/mixed"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234567890", Subject: "First commit"},
		{Hash: "def5678901234", Subject: "Second commit"},
		{Hash: "ghi9012345678", Subject: "Third commit"},
	}
	// Only first commit has a remote branch
	mockGit.RemoteBranches = []string{
		"stack/feature/mixed/01-first-commit",
	}

	mockGH := github.NewMockGitHub()
	// Only add PR for the first commit (the one with a remote branch)
	mockGH.AddExistingPR("stack/feature/mixed/01-first-commit", 100, "First commit")
	// No PR for second and third commits (they don't have remote branches yet)

	// Save and restore original DI functions
	oldNewGit := statusNewGit
	oldNewGitHub := statusNewGitHub
	defer func() {
		statusNewGit = oldNewGit
		statusNewGitHub = oldNewGitHub
	}()

	statusNewGit = func() git.GitOperations { return mockGit }
	statusNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runStatus(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only 1 remote branch exists, so only 1 ListAllPullRequests call
	listAllCalls := 0
	for _, call := range mockGH.Calls {
		if call.Method == "ListAllPullRequests" {
			listAllCalls++
		}
	}
	if listAllCalls != 1 {
		t.Errorf("expected 1 ListAllPullRequests calls (1 remote branch), got %d", listAllCalls)
	}
}

func TestRunStatus_GitError(t *testing.T) {
	// Test error handling when git operations fail
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/test"
	mockGit.Errors["GetBaseBranch"] = errors.New("not a git repository")

	// Save and restore original DI functions
	oldNewGit := statusNewGit
	defer func() {
		statusNewGit = oldNewGit
	}()

	statusNewGit = func() git.GitOperations { return mockGit }

	err := runStatus(nil, []string{})
	if err == nil {
		t.Fatal("expected error when GetBaseBranch fails")
	}
}
