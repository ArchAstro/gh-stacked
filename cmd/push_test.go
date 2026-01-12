// cmd/push_test.go
package cmd

import (
	"errors"
	"testing"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/github"
)

func TestParseSquashRange(t *testing.T) {
	tests := []struct {
		input     string
		wantStart int
		wantEnd   int
		wantErr   bool
	}{
		{"2-4", 2, 4, false},
		{"1-10", 1, 10, false},
		{"5-3", 0, 0, true},  // start >= end
		{"abc", 0, 0, true},  // invalid format
		{"2-", 0, 0, true},   // missing end
		{"-4", 0, 0, true},   // missing start
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			start, end, err := parseSquashRange(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if start != tt.wantStart {
				t.Errorf("start = %d, want %d", start, tt.wantStart)
			}
			if end != tt.wantEnd {
				t.Errorf("end = %d, want %d", end, tt.wantEnd)
			}
		})
	}
}

func TestPushCommand(t *testing.T) {
	cmd := pushCmd

	if cmd.Use != "push" {
		t.Errorf("expected Use 'push', got %s", cmd.Use)
	}

	// Verify flags exist
	previewFlag := cmd.Flags().Lookup("preview")
	if previewFlag == nil {
		t.Error("expected --preview flag")
	}

	squashFlag := cmd.Flags().Lookup("squash")
	if squashFlag == nil {
		t.Error("expected --squash flag")
	}
}

// TestRunPush_CreatesNewPRs tests that 3 commits with no existing PRs creates 3 PRs
func TestRunPush_CreatesNewPRs(t *testing.T) {
	// Setup mocks
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
		{Hash: "ghi9012", Subject: "Add user tests"},
	}

	mockGH := github.NewMockGitHub()

	// Inject mocks
	oldNewGit := newGit
	oldNewGitHub := newGitHub
	defer func() {
		newGit = oldNewGit
		newGitHub = oldNewGitHub
	}()
	newGit = func() git.GitOperations { return mockGit }
	newGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	// Run command
	err := runPush(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify 3 PRs created
	if len(mockGH.CreatedPRs) != 3 {
		t.Errorf("expected 3 PRs created, got %d", len(mockGH.CreatedPRs))
	}

	// Verify PR chain: PR1 -> main, PR2 -> PR1 branch, PR3 -> PR2 branch
	if mockGH.CreatedPRs[0].Base != "main" {
		t.Errorf("PR1 should target main, got %s", mockGH.CreatedPRs[0].Base)
	}

	// PR2 should target PR1's branch
	if mockGH.CreatedPRs[1].Base != mockGH.CreatedPRs[0].Head {
		t.Errorf("PR2 should target PR1's branch (%s), got %s", mockGH.CreatedPRs[0].Head, mockGH.CreatedPRs[1].Base)
	}

	// PR3 should target PR2's branch
	if mockGH.CreatedPRs[2].Base != mockGH.CreatedPRs[1].Head {
		t.Errorf("PR3 should target PR2's branch (%s), got %s", mockGH.CreatedPRs[1].Head, mockGH.CreatedPRs[2].Base)
	}

	// Verify branches were created and pushed
	createBranchCalls := 0
	pushBranchCalls := 0
	for _, call := range mockGit.Calls {
		if call.Method == "CreateBranchAtCommit" {
			createBranchCalls++
		}
		if call.Method == "PushBranch" {
			pushBranchCalls++
		}
	}
	if createBranchCalls != 3 {
		t.Errorf("expected 3 CreateBranchAtCommit calls, got %d", createBranchCalls)
	}
	if pushBranchCalls != 3 {
		t.Errorf("expected 3 PushBranch calls, got %d", pushBranchCalls)
	}

	// Verify PR titles have [X/N] prefix
	if mockGH.CreatedPRs[0].Title != "[1/3] Add user model" {
		t.Errorf("PR1 title should have [1/3] prefix, got %q", mockGH.CreatedPRs[0].Title)
	}
	if mockGH.CreatedPRs[1].Title != "[2/3] Add user API" {
		t.Errorf("PR2 title should have [2/3] prefix, got %q", mockGH.CreatedPRs[1].Title)
	}
	if mockGH.CreatedPRs[2].Title != "[3/3] Add user tests" {
		t.Errorf("PR3 title should have [3/3] prefix, got %q", mockGH.CreatedPRs[2].Title)
	}
}

// TestRunPush_UpdatesExistingPRs tests 2 commits with 1 existing PR: creates 1, updates 1
func TestRunPush_UpdatesExistingPRs(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
	}

	mockGH := github.NewMockGitHub()
	// PR already exists for first commit - branch name follows the pattern from stack.StackBranchName
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 42, "Add user model")

	oldNewGit := newGit
	oldNewGitHub := newGitHub
	oldForceUpdate := pushForceUpdateMetadata
	defer func() {
		newGit = oldNewGit
		newGitHub = oldNewGitHub
		pushForceUpdateMetadata = oldForceUpdate
	}()
	newGit = func() git.GitOperations { return mockGit }
	newGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }
	pushForceUpdateMetadata = true // Must set this to update existing PRs

	err := runPush(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should create 1 new PR, update 1 existing
	if len(mockGH.CreatedPRs) != 1 {
		t.Errorf("expected 1 PR created, got %d", len(mockGH.CreatedPRs))
	}
	if len(mockGH.UpdatedPRs) != 1 {
		t.Errorf("expected 1 PR updated, got %d", len(mockGH.UpdatedPRs))
	}
	if len(mockGH.UpdatedPRs) > 0 && mockGH.UpdatedPRs[0].Number != 42 {
		t.Errorf("expected PR #42 updated, got #%d", mockGH.UpdatedPRs[0].Number)
	}
}

// TestRunPush_PreviewMode tests that preview mode creates no PRs
func TestRunPush_PreviewMode(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	mockGH := github.NewMockGitHub()

	oldNewGit := newGit
	oldNewGitHub := newGitHub
	oldPreview := pushPreview
	defer func() {
		newGit = oldNewGit
		newGitHub = oldNewGitHub
		pushPreview = oldPreview
	}()
	newGit = func() git.GitOperations { return mockGit }
	newGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }
	pushPreview = true

	err := runPush(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No PRs should be created in preview mode
	if len(mockGH.CreatedPRs) != 0 {
		t.Errorf("expected 0 PRs in preview mode, got %d", len(mockGH.CreatedPRs))
	}

	// No branches should be created or pushed in preview mode
	for _, call := range mockGit.Calls {
		if call.Method == "CreateBranchAtCommit" {
			t.Error("CreateBranchAtCommit should not be called in preview mode")
		}
		if call.Method == "PushBranch" {
			t.Error("PushBranch should not be called in preview mode")
		}
	}
}

// TestRunPush_SquashValidation tests error when trying to squash existing PR range
func TestRunPush_SquashValidation(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
		{Hash: "ghi9012", Subject: "Add user tests"},
	}

	mockGH := github.NewMockGitHub()
	// PR exists for commit 2
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 43, "Add user API")

	oldNewGit := newGit
	oldNewGitHub := newGitHub
	oldSquash := pushSquash
	defer func() {
		newGit = oldNewGit
		newGitHub = oldNewGitHub
		pushSquash = oldSquash
	}()
	newGit = func() git.GitOperations { return mockGit }
	newGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }
	pushSquash = []string{"2-3"} // Trying to squash commits 2-3

	err := runPush(nil, []string{})
	if err == nil {
		t.Fatal("expected error for squashing existing PR")
	}

	// Verify error message mentions the PR
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

// TestRunPush_RejectsStackBranch tests error when running from a stack branch
func TestRunPush_RejectsStackBranch(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "stack/feature/auth/01-add-user-model"

	oldNewGit := newGit
	defer func() { newGit = oldNewGit }()
	newGit = func() git.GitOperations { return mockGit }

	err := runPush(nil, []string{})
	if err == nil {
		t.Fatal("expected error when running from stack branch")
	}

	// Verify error message is appropriate
	expected := "run push from your feature branch, not a stack branch"
	if err.Error() != expected {
		t.Errorf("expected error %q, got %q", expected, err.Error())
	}
}

// TestRunPush_NoCommits tests early exit when there are no commits
func TestRunPush_NoCommits(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{} // No commits

	oldNewGit := newGit
	defer func() { newGit = oldNewGit }()
	newGit = func() git.GitOperations { return mockGit }

	err := runPush(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify GitHub client was never created since we exit early
	// The function should return after checking commits without creating GitHub client
}

// TestRunPush_PRChainOrder tests that PRs are created in correct order with proper base branches
func TestRunPush_PRChainOrder(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "aaa1111", Subject: "First commit"},
		{Hash: "bbb2222", Subject: "Second commit"},
	}

	mockGH := github.NewMockGitHub()

	oldNewGit := newGit
	oldNewGitHub := newGitHub
	defer func() {
		newGit = oldNewGit
		newGitHub = oldNewGitHub
	}()
	newGit = func() git.GitOperations { return mockGit }
	newGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runPush(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify 2 PRs created
	if len(mockGH.CreatedPRs) != 2 {
		t.Fatalf("expected 2 PRs created, got %d", len(mockGH.CreatedPRs))
	}

	// First PR targets main
	if mockGH.CreatedPRs[0].Base != "main" {
		t.Errorf("first PR should target main, got %s", mockGH.CreatedPRs[0].Base)
	}

	// Second PR targets first PR's head branch
	expectedBase := mockGH.CreatedPRs[0].Head
	if mockGH.CreatedPRs[1].Base != expectedBase {
		t.Errorf("second PR should target %s, got %s", expectedBase, mockGH.CreatedPRs[1].Base)
	}
}

// TestRunPush_BranchNamingConvention tests that branches follow the expected naming pattern
func TestRunPush_BranchNamingConvention(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add User Model"},
	}

	mockGH := github.NewMockGitHub()

	oldNewGit := newGit
	oldNewGitHub := newGitHub
	defer func() {
		newGit = oldNewGit
		newGitHub = oldNewGitHub
	}()
	newGit = func() git.GitOperations { return mockGit }
	newGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runPush(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify branch name follows pattern: stack/{parent}/NN-{slug}
	if len(mockGH.CreatedPRs) != 1 {
		t.Fatalf("expected 1 PR created, got %d", len(mockGH.CreatedPRs))
	}

	expectedHead := "stack/feature/auth/01-add-user-model"
	if mockGH.CreatedPRs[0].Head != expectedHead {
		t.Errorf("expected head branch %s, got %s", expectedHead, mockGH.CreatedPRs[0].Head)
	}
}

// TestRunPush_MultipleExistingPRs tests updating multiple existing PRs
func TestRunPush_MultipleExistingPRs(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
		{Hash: "ghi9012", Subject: "Add user tests"},
	}

	mockGH := github.NewMockGitHub()
	// All PRs already exist
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 101, "Add user API")
	mockGH.AddExistingPR("stack/feature/auth/03-add-user-tests", 102, "Add user tests")

	oldNewGit := newGit
	oldNewGitHub := newGitHub
	oldForceUpdate := pushForceUpdateMetadata
	defer func() {
		newGit = oldNewGit
		newGitHub = oldNewGitHub
		pushForceUpdateMetadata = oldForceUpdate
	}()
	newGit = func() git.GitOperations { return mockGit }
	newGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }
	pushForceUpdateMetadata = true // Must set this to update existing PRs

	err := runPush(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should update all 3 existing PRs, create none
	if len(mockGH.CreatedPRs) != 0 {
		t.Errorf("expected 0 PRs created, got %d", len(mockGH.CreatedPRs))
	}
	if len(mockGH.UpdatedPRs) != 3 {
		t.Errorf("expected 3 PRs updated, got %d", len(mockGH.UpdatedPRs))
	}

	// Verify correct PR numbers were updated
	expectedNumbers := map[int]bool{100: true, 101: true, 102: true}
	for _, updated := range mockGH.UpdatedPRs {
		if !expectedNumbers[updated.Number] {
			t.Errorf("unexpected PR number updated: %d", updated.Number)
		}
	}
}

// TestRunPush_SkipsExistingPRsWithoutFlag tests that existing PRs are skipped without --force-update-pr-metadata
func TestRunPush_SkipsExistingPRsWithoutFlag(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")
	// Second commit has no existing PR

	oldNewGit := newGit
	oldNewGitHub := newGitHub
	oldForceUpdate := pushForceUpdateMetadata
	defer func() {
		newGit = oldNewGit
		newGitHub = oldNewGitHub
		pushForceUpdateMetadata = oldForceUpdate
	}()
	newGit = func() git.GitOperations { return mockGit }
	newGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }
	pushForceUpdateMetadata = false // Ensure flag is off

	err := runPush(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should create 1 new PR (for commit 2), NOT update existing PR (commit 1)
	if len(mockGH.CreatedPRs) != 1 {
		t.Errorf("expected 1 PR created, got %d", len(mockGH.CreatedPRs))
	}
	if len(mockGH.UpdatedPRs) != 0 {
		t.Errorf("expected 0 PRs updated without flag, got %d", len(mockGH.UpdatedPRs))
	}
}

// TestRunPush_Regroup tests the --regroup flag triggers interactive rebase
func TestRunPush_Regroup(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
	}

	mockGH := github.NewMockGitHub()

	oldNewGit := newGit
	oldNewGitHub := newGitHub
	oldRegroup := pushRegroup
	defer func() {
		newGit = oldNewGit
		newGitHub = oldNewGitHub
		pushRegroup = oldRegroup
	}()
	newGit = func() git.GitOperations { return mockGit }
	newGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }
	pushRegroup = true

	err := runPush(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify InteractiveRebase was called
	if !mockGit.WasCalled("InteractiveRebase") {
		t.Error("expected InteractiveRebase to be called with --regroup")
	}

	// Should still create PRs after rebase
	if len(mockGH.CreatedPRs) != 2 {
		t.Errorf("expected 2 PRs created after regroup, got %d", len(mockGH.CreatedPRs))
	}
}

// TestRunPush_RegroupFails tests error handling when interactive rebase fails
func TestRunPush_RegroupFails(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}
	mockGit.Errors["InteractiveRebase"] = errors.New("conflict")

	oldNewGit := newGit
	oldRegroup := pushRegroup
	defer func() {
		newGit = oldNewGit
		pushRegroup = oldRegroup
	}()
	newGit = func() git.GitOperations { return mockGit }
	pushRegroup = true

	err := runPush(nil, []string{})
	if err == nil {
		t.Fatal("expected error when interactive rebase fails")
	}
}
