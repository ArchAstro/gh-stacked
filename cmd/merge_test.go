// cmd/merge_test.go
package cmd

import (
	"errors"
	"testing"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/github"
)

func TestMergeCommand(t *testing.T) {
	cmd := mergeCmd

	if cmd.Use != "merge <pr-number>" {
		t.Errorf("expected Use 'merge <pr-number>', got %s", cmd.Use)
	}

	// Verify the command takes exactly 1 argument
	if cmd.Args == nil {
		t.Error("expected Args validator")
	}
}

// TestRunMerge_Success tests merging PR 1 with remaining PRs triggers merge, retarget, and rebase
func TestRunMerge_Success(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
	}

	mockGH := github.NewMockGitHub()
	// Add PRs for both commits
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 101, "Add user API")

	oldNewGit := mergeNewGit
	oldNewGitHub := mergeNewGitHub
	oldNoRebase := mergeNoRebase
	defer func() {
		mergeNewGit = oldNewGit
		mergeNewGitHub = oldNewGitHub
		mergeNoRebase = oldNoRebase
	}()
	mergeNewGit = func() git.GitOperations { return mockGit }
	mergeNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }
	mergeNoRebase = false

	err := runMerge(nil, []string{"1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify PR #100 was merged
	if len(mockGH.MergedPRs) != 1 || mockGH.MergedPRs[0] != 100 {
		t.Errorf("expected PR #100 merged, got %v", mockGH.MergedPRs)
	}

	// Verify UpdatePullRequestBase was called for PR #101 to retarget to main
	foundRetarget := false
	for _, call := range mockGH.Calls {
		if call.Method == "UpdatePullRequestBase" {
			if len(call.Args) >= 2 {
				number, _ := call.Args[0].(int)
				base, _ := call.Args[1].(string)
				if number == 101 && base == "main" {
					foundRetarget = true
				}
			}
		}
	}
	if !foundRetarget {
		t.Error("expected PR #101 to be retargeted to main")
	}

	// Verify rebase was performed (fetch + rebase)
	foundFetch := false
	foundRebase := false
	for _, call := range mockGit.Calls {
		if call.Method == "Run" {
			args, ok := call.Args[0].([]string)
			if ok && len(args) >= 1 {
				if args[0] == "fetch" {
					foundFetch = true
				}
				if args[0] == "rebase" {
					foundRebase = true
				}
			}
		}
	}
	if !foundFetch {
		t.Error("expected fetch to be called during rebase")
	}
	if !foundRebase {
		t.Error("expected rebase to be called")
	}
}

// TestRunMerge_MustMergeInOrder tests error when trying to merge PR 2 before PR 1
func TestRunMerge_MustMergeInOrder(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
	}

	oldNewGit := mergeNewGit
	defer func() { mergeNewGit = oldNewGit }()
	mergeNewGit = func() git.GitOperations { return mockGit }

	err := runMerge(nil, []string{"2"}) // Try to merge PR 2 first
	if err == nil {
		t.Fatal("expected error for out-of-order merge")
	}

	// Error message should mention that PR #1 must be merged first
	if !containsSubstring(err.Error(), "PR #1 must be merged first") {
		t.Errorf("expected error mentioning PR #1 must be merged first, got %q", err.Error())
	}
}

// TestRunMerge_NoRebaseFlag tests merge with --no-rebase skips rebase
func TestRunMerge_NoRebaseFlag(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")

	oldNewGit := mergeNewGit
	oldNewGitHub := mergeNewGitHub
	oldNoRebase := mergeNoRebase
	defer func() {
		mergeNewGit = oldNewGit
		mergeNewGitHub = oldNewGitHub
		mergeNoRebase = oldNoRebase
	}()
	mergeNewGit = func() git.GitOperations { return mockGit }
	mergeNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }
	mergeNoRebase = true

	err := runMerge(nil, []string{"1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify PR was merged
	if len(mockGH.MergedPRs) != 1 {
		t.Errorf("expected 1 PR merged, got %d", len(mockGH.MergedPRs))
	}

	// Should not have called Run for rebase
	for _, call := range mockGit.Calls {
		if call.Method == "Run" {
			args, ok := call.Args[0].([]string)
			if ok && len(args) > 0 && args[0] == "rebase" {
				t.Error("should not rebase with --no-rebase flag")
			}
		}
	}

	// Should not have called UpdatePullRequestBase for retargeting
	for _, call := range mockGH.Calls {
		if call.Method == "UpdatePullRequestBase" {
			t.Error("should not retarget PRs with --no-rebase flag")
		}
	}
}

// TestRunMerge_NoRemainingPRs tests merge when there are no remaining PRs (single PR stack)
func TestRunMerge_NoRemainingPRs(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")

	oldNewGit := mergeNewGit
	oldNewGitHub := mergeNewGitHub
	oldNoRebase := mergeNoRebase
	defer func() {
		mergeNewGit = oldNewGit
		mergeNewGitHub = oldNewGitHub
		mergeNoRebase = oldNoRebase
	}()
	mergeNewGit = func() git.GitOperations { return mockGit }
	mergeNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }
	mergeNoRebase = false

	err := runMerge(nil, []string{"1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify PR was merged
	if len(mockGH.MergedPRs) != 1 || mockGH.MergedPRs[0] != 100 {
		t.Errorf("expected PR #100 merged, got %v", mockGH.MergedPRs)
	}

	// Should not have called UpdatePullRequestBase (no remaining PRs to retarget)
	for _, call := range mockGH.Calls {
		if call.Method == "UpdatePullRequestBase" {
			t.Error("should not retarget when no remaining PRs")
		}
	}
}

// TestRunMerge_RejectsStackBranch tests error when running from a stack branch
func TestRunMerge_RejectsStackBranch(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "stack/feature/auth/01-add-user-model"

	oldNewGit := mergeNewGit
	defer func() { mergeNewGit = oldNewGit }()
	mergeNewGit = func() git.GitOperations { return mockGit }

	err := runMerge(nil, []string{"1"})
	if err == nil {
		t.Fatal("expected error when running from stack branch")
	}

	expectedError := "run merge from your feature branch, not a stack branch"
	if err.Error() != expectedError {
		t.Errorf("expected error %q, got %q", expectedError, err.Error())
	}
}

// TestRunMerge_InvalidPRNumber tests error when PR number is out of range
func TestRunMerge_InvalidPRNumber(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	oldNewGit := mergeNewGit
	defer func() { mergeNewGit = oldNewGit }()
	mergeNewGit = func() git.GitOperations { return mockGit }

	err := runMerge(nil, []string{"5"}) // Only 1 commit exists
	if err == nil {
		t.Fatal("expected error for invalid PR number")
	}

	if !containsSubstring(err.Error(), "PR 5 not found") {
		t.Errorf("expected error containing 'PR 5 not found', got %q", err.Error())
	}
}

// TestRunMerge_NonNumericArg tests error when argument is not a number
func TestRunMerge_NonNumericArg(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"

	oldNewGit := mergeNewGit
	defer func() { mergeNewGit = oldNewGit }()
	mergeNewGit = func() git.GitOperations { return mockGit }

	err := runMerge(nil, []string{"abc"})
	if err == nil {
		t.Fatal("expected error for non-numeric argument")
	}

	if !containsSubstring(err.Error(), "invalid PR number") {
		t.Errorf("expected error containing 'invalid PR number', got %q", err.Error())
	}
}

// TestRunMerge_PRNumberZero tests error when PR number is 0
func TestRunMerge_PRNumberZero(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	oldNewGit := mergeNewGit
	defer func() { mergeNewGit = oldNewGit }()
	mergeNewGit = func() git.GitOperations { return mockGit }

	err := runMerge(nil, []string{"0"})
	if err == nil {
		t.Fatal("expected error for PR number 0")
	}

	if !containsSubstring(err.Error(), "PR 0 not found") {
		t.Errorf("expected error about PR 0 not found, got %q", err.Error())
	}
}

// TestRunMerge_NoPRFound tests error when no PR exists for the branch
func TestRunMerge_NoPRFound(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	mockGH := github.NewMockGitHub()
	// No PRs added - branch has no associated PR

	oldNewGit := mergeNewGit
	oldNewGitHub := mergeNewGitHub
	defer func() {
		mergeNewGit = oldNewGit
		mergeNewGitHub = oldNewGitHub
	}()
	mergeNewGit = func() git.GitOperations { return mockGit }
	mergeNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runMerge(nil, []string{"1"})
	if err == nil {
		t.Fatal("expected error when no PR found for branch")
	}

	if !containsSubstring(err.Error(), "no PR found") {
		t.Errorf("expected error about no PR found, got %q", err.Error())
	}
}

// TestRunMerge_MergeFails tests error handling when MergePullRequest fails
func TestRunMerge_MergeFails(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")
	mockGH.Errors["MergePullRequest"] = errors.New("merge conflict")

	oldNewGit := mergeNewGit
	oldNewGitHub := mergeNewGitHub
	defer func() {
		mergeNewGit = oldNewGit
		mergeNewGitHub = oldNewGitHub
	}()
	mergeNewGit = func() git.GitOperations { return mockGit }
	mergeNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runMerge(nil, []string{"1"})
	if err == nil {
		t.Fatal("expected error when MergePullRequest fails")
	}

	if !containsSubstring(err.Error(), "failed to merge PR") {
		t.Errorf("expected error about failed merge, got %q", err.Error())
	}
}

// TestRunMerge_RetargetFails tests error handling when UpdatePullRequestBase fails
func TestRunMerge_RetargetFails(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 101, "Add user API")
	mockGH.Errors["UpdatePullRequestBase"] = errors.New("retarget failed")

	oldNewGit := mergeNewGit
	oldNewGitHub := mergeNewGitHub
	oldNoRebase := mergeNoRebase
	defer func() {
		mergeNewGit = oldNewGit
		mergeNewGitHub = oldNewGitHub
		mergeNoRebase = oldNoRebase
	}()
	mergeNewGit = func() git.GitOperations { return mockGit }
	mergeNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }
	mergeNoRebase = false

	err := runMerge(nil, []string{"1"})
	if err == nil {
		t.Fatal("expected error when retarget fails")
	}

	if !containsSubstring(err.Error(), "failed to retarget") {
		t.Errorf("expected error about retarget failure, got %q", err.Error())
	}
}

// TestRunMerge_RebaseConflict tests error handling when rebase has conflict
func TestRunMerge_RebaseConflict(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
	}
	// Simulate rebase conflict on the second Run call (first is fetch, second is rebase)
	callCount := 0
	mockGit.Errors["Run"] = nil // Will be set dynamically

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 101, "Add user API")

	// Create a custom MockGit that fails on rebase
	customMockGit := &rebaseFailMockGit{
		MockGit:   mockGit,
		failCount: 0,
	}

	oldNewGit := mergeNewGit
	oldNewGitHub := mergeNewGitHub
	oldNoRebase := mergeNoRebase
	defer func() {
		mergeNewGit = oldNewGit
		mergeNewGitHub = oldNewGitHub
		mergeNoRebase = oldNoRebase
	}()
	mergeNewGit = func() git.GitOperations { return customMockGit }
	mergeNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }
	mergeNoRebase = false
	_ = callCount // silence unused variable warning

	err := runMerge(nil, []string{"1"})
	if err == nil {
		t.Fatal("expected error when rebase has conflict")
	}

	// Error message should mention conflict
	if !containsSubstring(err.Error(), "conflict") {
		t.Errorf("expected error mentioning conflict, got %q", err.Error())
	}

	// AbortRebase should have been called
	if !customMockGit.WasCalled("AbortRebase") {
		t.Error("expected AbortRebase to be called on conflict")
	}
}

// rebaseFailMockGit is a custom mock that fails on the rebase Run call
type rebaseFailMockGit struct {
	*git.MockGit
	failCount int
}

func (m *rebaseFailMockGit) Run(args ...string) (string, error) {
	m.MockGit.Calls = append(m.MockGit.Calls, git.MockCall{Method: "Run", Args: []interface{}{args}})
	if len(args) > 0 && args[0] == "rebase" {
		return "", errors.New("conflict rebasing")
	}
	return "", nil
}

// TestRunMerge_FetchFails tests error handling when fetch fails
func TestRunMerge_FetchFails(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
	}
	mockGit.Errors["Run"] = errors.New("fetch failed")

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 101, "Add user API")

	oldNewGit := mergeNewGit
	oldNewGitHub := mergeNewGitHub
	oldNoRebase := mergeNoRebase
	defer func() {
		mergeNewGit = oldNewGit
		mergeNewGitHub = oldNewGitHub
		mergeNoRebase = oldNoRebase
	}()
	mergeNewGit = func() git.GitOperations { return mockGit }
	mergeNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }
	mergeNoRebase = false

	err := runMerge(nil, []string{"1"})
	if err == nil {
		t.Fatal("expected error when fetch fails")
	}

	if !containsSubstring(err.Error(), "failed to fetch") {
		t.Errorf("expected error about fetch failure, got %q", err.Error())
	}
}

// TestRunMerge_ThreePRStack tests merging first PR in a 3-PR stack
func TestRunMerge_ThreePRStack(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
		{Hash: "ghi9012", Subject: "Add user tests"},
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 101, "Add user API")
	mockGH.AddExistingPR("stack/feature/auth/03-add-user-tests", 102, "Add user tests")

	oldNewGit := mergeNewGit
	oldNewGitHub := mergeNewGitHub
	oldNoRebase := mergeNoRebase
	defer func() {
		mergeNewGit = oldNewGit
		mergeNewGitHub = oldNewGitHub
		mergeNoRebase = oldNoRebase
	}()
	mergeNewGit = func() git.GitOperations { return mockGit }
	mergeNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }
	mergeNoRebase = false

	err := runMerge(nil, []string{"1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify only PR #100 was merged
	if len(mockGH.MergedPRs) != 1 || mockGH.MergedPRs[0] != 100 {
		t.Errorf("expected only PR #100 merged, got %v", mockGH.MergedPRs)
	}

	// Verify PR #101 (the second PR, now first in remaining stack) was retargeted to main
	foundRetarget := false
	for _, call := range mockGH.Calls {
		if call.Method == "UpdatePullRequestBase" {
			if len(call.Args) >= 2 {
				number, _ := call.Args[0].(int)
				base, _ := call.Args[1].(string)
				if number == 101 && base == "main" {
					foundRetarget = true
				}
			}
		}
	}
	if !foundRetarget {
		t.Error("expected PR #101 to be retargeted to main")
	}
}

// TestRunMerge_GitHubClientCreationFails tests error when GitHub client creation fails
func TestRunMerge_GitHubClientCreationFails(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	oldNewGit := mergeNewGit
	oldNewGitHub := mergeNewGitHub
	defer func() {
		mergeNewGit = oldNewGit
		mergeNewGitHub = oldNewGitHub
	}()
	mergeNewGit = func() git.GitOperations { return mockGit }
	mergeNewGitHub = func() (github.GitHubOperations, error) {
		return nil, errors.New("authentication failed")
	}

	err := runMerge(nil, []string{"1"})
	if err == nil {
		t.Fatal("expected error when GitHub client creation fails")
	}

	if !containsSubstring(err.Error(), "failed to create GitHub client") {
		t.Errorf("expected error about GitHub client creation, got %q", err.Error())
	}
}

// TestRunMerge_ListPullRequestsFails tests error when ListPullRequests fails
func TestRunMerge_ListPullRequestsFails(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	mockGH := github.NewMockGitHub()
	mockGH.Errors["ListPullRequests"] = errors.New("API rate limit exceeded")

	oldNewGit := mergeNewGit
	oldNewGitHub := mergeNewGitHub
	defer func() {
		mergeNewGit = oldNewGit
		mergeNewGitHub = oldNewGitHub
	}()
	mergeNewGit = func() git.GitOperations { return mockGit }
	mergeNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runMerge(nil, []string{"1"})
	if err == nil {
		t.Fatal("expected error when ListPullRequests fails")
	}

	if !containsSubstring(err.Error(), "failed to find PR") {
		t.Errorf("expected error about finding PR, got %q", err.Error())
	}
}

// TestRunMerge_NoCommits tests error when there are no commits
func TestRunMerge_NoCommits(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{} // No commits

	oldNewGit := mergeNewGit
	defer func() { mergeNewGit = oldNewGit }()
	mergeNewGit = func() git.GitOperations { return mockGit }

	err := runMerge(nil, []string{"1"})
	if err == nil {
		t.Fatal("expected error when there are no commits")
	}

	if !containsSubstring(err.Error(), "PR 1 not found") {
		t.Errorf("expected error about PR not found, got %q", err.Error())
	}
}
