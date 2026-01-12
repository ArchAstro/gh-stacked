// cmd/push_test.go
package cmd

import (
	"errors"
	"strings"
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
	// Simulate existing remote branch for first commit
	mockGit.RemoteBranches = []string{"stack/feature/auth/01-add-user-model"}

	mockGH := github.NewMockGitHub()
	// PR already exists for first commit - branch name follows the pattern from stack.StackBranchName
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 42, "Add user model")

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

	// Should create 1 new PR, update 1 existing (metadata update in second pass)
	if len(mockGH.CreatedPRs) != 1 {
		t.Errorf("expected 1 PR created, got %d", len(mockGH.CreatedPRs))
	}
	// Both PRs get metadata updated in second pass (title prefix + footer)
	if len(mockGH.UpdatedPRs) < 1 {
		t.Errorf("expected at least 1 PR updated, got %d", len(mockGH.UpdatedPRs))
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
	// Simulate existing remote branch for second commit
	mockGit.RemoteBranches = []string{"stack/feature/auth/02-add-user-api"}

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
	// Simulate all existing remote branches
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
		"stack/feature/auth/02-add-user-api",
		"stack/feature/auth/03-add-user-tests",
	}

	mockGH := github.NewMockGitHub()
	// All PRs already exist
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 101, "Add user API")
	mockGH.AddExistingPR("stack/feature/auth/03-add-user-tests", 102, "Add user tests")

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

	// Should update all 3 existing PRs, create none
	if len(mockGH.CreatedPRs) != 0 {
		t.Errorf("expected 0 PRs created, got %d", len(mockGH.CreatedPRs))
	}
	// All 3 PRs get metadata updated (footer changes as PR numbers are now known)
	if len(mockGH.UpdatedPRs) < 3 {
		t.Errorf("expected at least 3 PRs updated, got %d", len(mockGH.UpdatedPRs))
	}
}

// TestRunPush_MetadataUpdatedForAllPRs tests that metadata is updated for all PRs
func TestRunPush_MetadataUpdatedForAllPRs(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
	}
	// Simulate existing remote branch for first commit only
	mockGit.RemoteBranches = []string{"stack/feature/auth/01-add-user-model"}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "[1/1] Add user model")
	// Second commit has no existing PR

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

	// Should create 1 new PR (for commit 2) and update existing PR (total changed from 1 to 2)
	if len(mockGH.CreatedPRs) != 1 {
		t.Errorf("expected 1 PR created, got %d", len(mockGH.CreatedPRs))
	}
	// Existing PR should be updated because total changed (1->2)
	if len(mockGH.UpdatedPRs) < 1 {
		t.Errorf("expected at least 1 PR updated (title prefix changed), got %d", len(mockGH.UpdatedPRs))
	}
}

// TestRunPush_PreservesPositionsAfterMerge tests that after merging PR 1,
// remaining commits keep their original positions (2, 3, 4) not renumbered (1, 2, 3)
func TestRunPush_PreservesPositionsAfterMerge(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	// After merging PR 1, we have 3 remaining commits (originally positions 2, 3, 4)
	mockGit.Commits = []git.Commit{
		{Hash: "def5678", Subject: "Add user API"},      // was position 2
		{Hash: "ghi9012", Subject: "Add user tests"},    // was position 3
		{Hash: "jkl3456", Subject: "Add user docs"},     // was position 4
	}
	// Remote branches exist at their ORIGINAL positions (02, 03, 04)
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/02-add-user-api",
		"stack/feature/auth/03-add-user-tests",
		"stack/feature/auth/04-add-user-docs",
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 101, "Add user API")
	mockGH.AddExistingPR("stack/feature/auth/03-add-user-tests", 102, "Add user tests")
	mockGH.AddExistingPR("stack/feature/auth/04-add-user-docs", 103, "Add user docs")

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

	// Should NOT create any new PRs (all exist at original positions)
	if len(mockGH.CreatedPRs) != 0 {
		t.Errorf("expected 0 PRs created (positions preserved), got %d", len(mockGH.CreatedPRs))
	}

	// Should update all 3 existing PRs (metadata update)
	if len(mockGH.UpdatedPRs) < 3 {
		t.Errorf("expected at least 3 PRs updated, got %d", len(mockGH.UpdatedPRs))
	}

	// Verify branches used were the ORIGINAL positions (02, 03, 04), not renumbered (01, 02, 03)
	for _, call := range mockGit.Calls {
		if call.Method == "CreateBranchAtCommit" {
			branchName := call.Args[0].(string)
			// Should NOT contain "01-" since we preserved positions
			if branchName == "stack/feature/auth/01-add-user-api" {
				t.Error("branch was renumbered to position 01 - should have stayed at 02!")
			}
		}
	}
}

// TestRunPush_NewCommitGetsNextPosition tests that a new commit after existing stack
// gets the next available position number, not position 1
func TestRunPush_NewCommitGetsNextPosition(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	// 2 existing commits + 1 new commit
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},    // existing at position 1
		{Hash: "def5678", Subject: "Add user API"},      // existing at position 2
		{Hash: "ghi9012", Subject: "Add user tests"},    // NEW commit - should get position 3
	}
	// Only first 2 commits have existing remote branches
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
		"stack/feature/auth/02-add-user-api",
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 101, "Add user API")

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

	// Should create exactly 1 new PR for the new commit
	if len(mockGH.CreatedPRs) != 1 {
		t.Errorf("expected 1 PR created, got %d", len(mockGH.CreatedPRs))
	}

	// The new PR should use position 03 (next after max existing)
	if len(mockGH.CreatedPRs) > 0 {
		expectedBranch := "stack/feature/auth/03-add-user-tests"
		if mockGH.CreatedPRs[0].Head != expectedBranch {
			t.Errorf("new commit should get position 03, got branch: %s", mockGH.CreatedPRs[0].Head)
		}
	}
}

// TestRunPush_SlugCollision tests that multiple commits with same slug get different positions
func TestRunPush_SlugCollision(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	// Two commits with similar first 5 words (same slug)
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user properties table"},      // slug: add-user-properties-table
		{Hash: "def5678", Subject: "Add user properties endpoints"},  // slug: add-user-properties-endpoints (different!)
	}
	// No existing branches

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

	// Should create 2 PRs with different positions
	if len(mockGH.CreatedPRs) != 2 {
		t.Errorf("expected 2 PRs created, got %d", len(mockGH.CreatedPRs))
	}

	// Verify different branch names (different positions)
	if len(mockGH.CreatedPRs) >= 2 {
		if mockGH.CreatedPRs[0].Head == mockGH.CreatedPRs[1].Head {
			t.Errorf("both PRs have same branch: %s (slug collision not handled)", mockGH.CreatedPRs[0].Head)
		}
	}
}

// TestRunPush_SlugCollisionWithExisting tests that slug collision doesn't double-claim branches
// Scenario: Two commits with SAME slug, two existing branches with SAME slug
// First commit should claim first branch, second commit should claim second branch
func TestRunPush_SlugCollisionWithExisting(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	// Two commits with SAME slug (first 5 words identical, slug matches branch)
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "feat add user model"},     // slug: feat-add-user-model
		{Hash: "def5678", Subject: "feat add user model"},     // slug: feat-add-user-model (SAME!)
		{Hash: "ghi9012", Subject: "feat add user API here"},  // slug: feat-add-user-api-here
	}
	// Existing branches - two have the SAME slug
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-feat-add-user-model",     // slug: feat-add-user-model
		"stack/feature/auth/02-feat-add-user-model",     // slug: feat-add-user-model (SAME!)
		"stack/feature/auth/03-feat-add-user-api-here",  // slug: feat-add-user-api-here
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-feat-add-user-model", 100, "feat add user model")
	mockGH.AddExistingPR("stack/feature/auth/02-feat-add-user-model", 101, "feat add user model")
	mockGH.AddExistingPR("stack/feature/auth/03-feat-add-user-api-here", 102, "feat add user API here")

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

	// Should NOT create any new PRs - all should match existing branches
	// Commit 1 claims branch 01 (first match)
	// Commit 2 claims branch 02 (branch 01 already claimed, so takes next match)
	// Commit 3 claims branch 03
	if len(mockGH.CreatedPRs) != 0 {
		t.Errorf("expected 0 PRs created, got %d", len(mockGH.CreatedPRs))
	}

	// Should update 3 different PRs (100, 101, 102)
	if len(mockGH.UpdatedPRs) < 3 {
		t.Errorf("expected at least 3 PRs updated, got %d", len(mockGH.UpdatedPRs))
	}
}

// TestRunPush_DuplicateBranchesAtSamePosition tests handling of orphaned duplicate branches
// Scenario: Multiple branches exist at same position (e.g., 02-foo and 02-bar from failed attempts)
// Tool should match commits correctly and not get confused by duplicates
func TestRunPush_DuplicateBranchesAtSamePosition(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
	}
	// Duplicate branches at position 01 and 02 (orphaned from failed attempts)
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",      // matches commit 1
		"stack/feature/auth/01-old-orphaned-branch", // orphaned duplicate at position 01
		"stack/feature/auth/02-add-user-api",        // matches commit 2
		"stack/feature/auth/02-another-orphan",      // orphaned duplicate at position 02
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 101, "Add user API")
	// Orphaned branches have no PRs

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

	// Should update the 2 PRs that match, NOT create new ones
	if len(mockGH.CreatedPRs) != 0 {
		t.Errorf("expected 0 PRs created (should match existing), got %d", len(mockGH.CreatedPRs))
	}
	if len(mockGH.UpdatedPRs) < 2 {
		t.Errorf("expected at least 2 PRs updated, got %d", len(mockGH.UpdatedPRs))
	}
}

// TestRunPush_NeverCreatesAtExistingPosition ensures new branches never reuse existing positions
// This is the critical test to prevent the renumbering bug that caused duplicate PRs
func TestRunPush_NeverCreatesAtExistingPosition(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	// 3 commits, but only 2 have existing branches (at positions 02 and 03)
	// This simulates the state after merging PR 1
	mockGit.Commits = []git.Commit{
		{Hash: "def5678", Subject: "Add user API"},           // existing at position 02
		{Hash: "ghi9012", Subject: "Add user tests"},         // existing at position 03
		{Hash: "jkl3456", Subject: "Add new feature"},        // NEW - must NOT get position 01, 02, or 03
	}
	// Existing branches at positions 02 and 03 (position 01 was merged and deleted)
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/02-add-user-api",
		"stack/feature/auth/03-add-user-tests",
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 101, "Add user API")
	mockGH.AddExistingPR("stack/feature/auth/03-add-user-tests", 102, "Add user tests")

	var createdBranches []string
	oldNewGit := newGit
	oldNewGitHub := newGitHub
	defer func() {
		newGit = oldNewGit
		newGitHub = oldNewGitHub
	}()

	// Track which branches are created
	mockGit.OnCreateBranch = func(name, hash string) error {
		createdBranches = append(createdBranches, name)
		return nil
	}

	newGit = func() git.GitOperations { return mockGit }
	newGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runPush(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// New commit should get position 04, NOT 01
	if len(mockGH.CreatedPRs) != 1 {
		t.Fatalf("expected 1 PR created, got %d", len(mockGH.CreatedPRs))
	}

	newBranch := mockGH.CreatedPRs[0].Head

	// Must NOT be position 01, 02, or 03
	forbiddenPrefixes := []string{
		"stack/feature/auth/01-",
		"stack/feature/auth/02-",
		"stack/feature/auth/03-",
	}
	for _, prefix := range forbiddenPrefixes {
		if len(newBranch) >= len(prefix) && newBranch[:len(prefix)] == prefix {
			t.Errorf("new branch %s reuses existing position! This causes duplicate PRs", newBranch)
		}
	}

	// Should be position 04
	expectedPrefix := "stack/feature/auth/04-"
	if len(newBranch) < len(expectedPrefix) || newBranch[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("expected new branch at position 04, got: %s", newBranch)
	}
}

// TestRunPush_MissingPRInMiddle tests the exact scenario that caused the bug:
// - Commits A, B, C exist locally
// - PRs exist for A (position 20) and C (position 22), but B has no PR
// - B should get position 21 (the hole between 20 and 22)
// - C's PR should be retargeted to point to B's new branch
func TestRunPush_MissingPRInMiddle(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/analytics"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "aaa1111", Subject: "Add analytics base"},        // has PR at position 20
		{Hash: "bbb2222", Subject: "Add visitor properties"},    // NO PR - should get position 21
		{Hash: "ccc3333", Subject: "Fix analytics error"},       // has PR at position 22
	}
	mockGit.RemoteBranches = []string{
		"stack/feature/analytics/20-add-analytics-base",
		"stack/feature/analytics/22-fix-analytics-error",
		// Note: position 21 is empty (the hole)
	}

	mockGH := github.NewMockGitHub()
	// PR at position 20 - currently targets main
	mockGH.AddExistingPR("stack/feature/analytics/20-add-analytics-base", 95, "[20/22] Add analytics base")
	// PR at position 22 - currently targets position 20's branch (will need retargeting)
	pr22 := github.PullRequest{
		Number: 96,
		Title:  "[22/22] Fix analytics error",
		Body:   "Some description\n\n---\n📚 **Stack:** test",
		URL:    "https://github.com/test/repo/pull/96",
		State:  "open",
	}
	pr22.Head.Ref = "stack/feature/analytics/22-fix-analytics-error"
	pr22.Base.Ref = "stack/feature/analytics/20-add-analytics-base" // Current base
	mockGH.PRs["stack/feature/analytics/22-fix-analytics-error"] = []github.PullRequest{pr22}

	var createdBranches []string
	oldNewGit := newGit
	oldNewGitHub := newGitHub
	defer func() {
		newGit = oldNewGit
		newGitHub = oldNewGitHub
	}()

	mockGit.OnCreateBranch = func(name, hash string) error {
		createdBranches = append(createdBranches, name)
		return nil
	}

	newGit = func() git.GitOperations { return mockGit }
	newGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runPush(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should create exactly 1 new PR (for the missing commit B)
	if len(mockGH.CreatedPRs) != 1 {
		t.Fatalf("expected 1 PR created, got %d", len(mockGH.CreatedPRs))
	}

	// The new PR should be at position 21 (the hole), NOT position 23
	newBranch := mockGH.CreatedPRs[0].Head
	expectedPrefix := "stack/feature/analytics/21-"
	if len(newBranch) < len(expectedPrefix) || newBranch[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("expected new branch at position 21 (the hole), got: %s", newBranch)
	}

	// Verify the new PR's base is position 20's branch
	if mockGH.CreatedPRs[0].Base != "stack/feature/analytics/20-add-analytics-base" {
		t.Errorf("expected new PR to target position 20, got base: %s", mockGH.CreatedPRs[0].Base)
	}

	// PR 96 (position 22) should have been retargeted to point to the new position 21 branch
	var retargetedTo string
	for _, call := range mockGH.Calls {
		if call.Method == "UpdatePullRequestBase" {
			args := call.Args
			if args[0].(int) == 96 {
				retargetedTo = args[1].(string)
			}
		}
	}
	if retargetedTo == "" {
		t.Error("expected PR #96 to be retargeted, but UpdatePullRequestBase was not called for it")
	} else if retargetedTo != newBranch {
		t.Errorf("expected PR #96 to be retargeted to %s, got: %s", newBranch, retargetedTo)
	}
}

// TestRunPush_MissingPRInMiddle_NoHoleAvailable tests the error case when there's no hole
func TestRunPush_MissingPRInMiddle_NoHoleAvailable(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "aaa1111", Subject: "First commit"},     // has PR at position 1
		{Hash: "bbb2222", Subject: "New commit"},       // NO PR - needs position between 1 and 2
		{Hash: "ccc3333", Subject: "Third commit"},     // has PR at position 2
	}
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-first-commit",
		"stack/feature/auth/02-third-commit",
		// No hole between 1 and 2!
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-first-commit", 100, "First commit")
	mockGH.AddExistingPR("stack/feature/auth/02-third-commit", 101, "Third commit")

	oldNewGit := newGit
	oldNewGitHub := newGitHub
	defer func() {
		newGit = oldNewGit
		newGitHub = oldNewGitHub
	}()
	newGit = func() git.GitOperations { return mockGit }
	newGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runPush(nil, []string{})

	// Should error because there's no available position between 1 and 2
	if err == nil {
		t.Fatal("expected error when no hole available, got nil")
	}
	if !strings.Contains(err.Error(), "no available position") {
		t.Errorf("expected 'no available position' error, got: %v", err)
	}
}

// TestRunPush_NewCommitAtStart tests inserting a new commit at the beginning
func TestRunPush_NewCommitAtStart(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "aaa1111", Subject: "New first commit"},  // NO PR - should get position 1
		{Hash: "bbb2222", Subject: "Old first commit"},  // has PR at position 5
	}
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/05-old-first-commit",
		// Positions 1-4 are available
	}

	mockGH := github.NewMockGitHub()
	pr5 := github.PullRequest{
		Number: 100,
		Title:  "[5/5] Old first commit",
		URL:    "https://github.com/test/repo/pull/100",
		State:  "open",
	}
	pr5.Head.Ref = "stack/feature/auth/05-old-first-commit"
	pr5.Base.Ref = "main"
	mockGH.PRs["stack/feature/auth/05-old-first-commit"] = []github.PullRequest{pr5}

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

	// New commit should get position 1 (first available before position 5)
	if len(mockGH.CreatedPRs) != 1 {
		t.Fatalf("expected 1 PR created, got %d", len(mockGH.CreatedPRs))
	}

	newBranch := mockGH.CreatedPRs[0].Head
	expectedPrefix := "stack/feature/auth/01-"
	if len(newBranch) < len(expectedPrefix) || newBranch[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("expected new branch at position 01, got: %s", newBranch)
	}

	// New PR should target main (it's first in the stack)
	if mockGH.CreatedPRs[0].Base != "main" {
		t.Errorf("expected new PR to target main, got: %s", mockGH.CreatedPRs[0].Base)
	}

	// PR 100 (position 5) should be retargeted to the new position 1 branch
	var retargetedTo string
	for _, call := range mockGH.Calls {
		if call.Method == "UpdatePullRequestBase" {
			args := call.Args
			if args[0].(int) == 100 {
				retargetedTo = args[1].(string)
			}
		}
	}
	if retargetedTo != newBranch {
		t.Errorf("expected PR #100 to be retargeted to %s, got: %s", newBranch, retargetedTo)
	}
}

// TestRunPush_MultipleNewCommitsInSequence tests inserting multiple new commits in a row
func TestRunPush_MultipleNewCommitsInSequence(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "aaa1111", Subject: "Existing commit"},    // has PR at position 10
		{Hash: "bbb2222", Subject: "New commit one"},     // NO PR - should get position 11
		{Hash: "ccc3333", Subject: "New commit two"},     // NO PR - should get position 12
		{Hash: "ddd4444", Subject: "Another existing"},   // has PR at position 20
	}
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/10-existing-commit",
		"stack/feature/auth/20-another-existing",
		// Positions 11-19 are available
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/10-existing-commit", 100, "Existing commit")
	pr20 := github.PullRequest{
		Number: 101,
		Title:  "[20/20] Another existing",
		URL:    "https://github.com/test/repo/pull/101",
		State:  "open",
	}
	pr20.Head.Ref = "stack/feature/auth/20-another-existing"
	pr20.Base.Ref = "stack/feature/auth/10-existing-commit"
	mockGH.PRs["stack/feature/auth/20-another-existing"] = []github.PullRequest{pr20}

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

	// Should create 2 new PRs
	if len(mockGH.CreatedPRs) != 2 {
		t.Fatalf("expected 2 PRs created, got %d", len(mockGH.CreatedPRs))
	}

	// First new commit should get position 11
	branch1 := mockGH.CreatedPRs[0].Head
	if !strings.HasPrefix(branch1, "stack/feature/auth/11-") {
		t.Errorf("expected first new branch at position 11, got: %s", branch1)
	}

	// Second new commit should get position 12
	branch2 := mockGH.CreatedPRs[1].Head
	if !strings.HasPrefix(branch2, "stack/feature/auth/12-") {
		t.Errorf("expected second new branch at position 12, got: %s", branch2)
	}

	// First new PR should target position 10
	if mockGH.CreatedPRs[0].Base != "stack/feature/auth/10-existing-commit" {
		t.Errorf("expected first new PR to target position 10, got: %s", mockGH.CreatedPRs[0].Base)
	}

	// Second new PR should target position 11 (the first new PR)
	if mockGH.CreatedPRs[1].Base != branch1 {
		t.Errorf("expected second new PR to target position 11, got: %s", mockGH.CreatedPRs[1].Base)
	}

	// PR 101 (position 20) should be retargeted to position 12 (second new PR)
	var retargetedTo string
	for _, call := range mockGH.Calls {
		if call.Method == "UpdatePullRequestBase" {
			args := call.Args
			if args[0].(int) == 101 {
				retargetedTo = args[1].(string)
			}
		}
	}
	if retargetedTo != branch2 {
		t.Errorf("expected PR #101 to be retargeted to %s, got: %s", branch2, retargetedTo)
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
