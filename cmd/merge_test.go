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

	expectedUse := "merge <position> or <start-end>"
	if cmd.Use != expectedUse {
		t.Errorf("expected Use %q, got %s", expectedUse, cmd.Use)
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
	// Remote branches are source of truth for positions
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
		"stack/feature/auth/02-add-user-api",
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
	// Remote branches are source of truth for positions
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
		"stack/feature/auth/02-add-user-api",
	}

	oldNewGit := mergeNewGit
	defer func() { mergeNewGit = oldNewGit }()
	mergeNewGit = func() git.GitOperations { return mockGit }

	err := runMerge(nil, []string{"2"}) // Try to merge PR 2 first
	if err == nil {
		t.Fatal("expected error for out-of-order merge")
	}

	// Error message should mention must start from position 1
	if !containsSubstring(err.Error(), "must start from position 1") {
		t.Errorf("expected error mentioning must start from position 1, got %q", err.Error())
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
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
		"stack/feature/auth/02-add-user-api",
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
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
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
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
	}

	oldNewGit := mergeNewGit
	defer func() { mergeNewGit = oldNewGit }()
	mergeNewGit = func() git.GitOperations { return mockGit }

	err := runMerge(nil, []string{"5"}) // Only 1 commit exists (stack position 1)
	if err == nil {
		t.Fatal("expected error for invalid PR number")
	}

	// Should fail because stack position 5 doesn't exist and must start from position 1
	if !containsSubstring(err.Error(), "must start from position 1") {
		t.Errorf("expected error about must start from position 1, got %q", err.Error())
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

	if !containsSubstring(err.Error(), "invalid position") {
		t.Errorf("expected error containing 'invalid position', got %q", err.Error())
	}
}

// TestRunMerge_PRNumberZero tests error when PR number is 0
func TestRunMerge_PRNumberZero(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"

	oldNewGit := mergeNewGit
	defer func() { mergeNewGit = oldNewGit }()
	mergeNewGit = func() git.GitOperations { return mockGit }

	err := runMerge(nil, []string{"0"})
	if err == nil {
		t.Fatal("expected error for PR number 0")
	}

	// Position 0 is invalid (must be positive)
	if !containsSubstring(err.Error(), "position must be positive") {
		t.Errorf("expected error about position must be positive, got %q", err.Error())
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
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
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

	if !containsSubstring(err.Error(), "no open PR found") {
		t.Errorf("expected error about no open PR found, got %q", err.Error())
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
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
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
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
		"stack/feature/auth/02-add-user-api",
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
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
		"stack/feature/auth/02-add-user-api",
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
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
		"stack/feature/auth/02-add-user-api",
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
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
		"stack/feature/auth/02-add-user-api",
		"stack/feature/auth/03-add-user-tests",
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
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
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
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
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

	if !containsSubstring(err.Error(), "no commits in stack") {
		t.Errorf("expected error about no commits in stack, got %q", err.Error())
	}
}

// TestRunMerge_Position2AfterMerge tests merging position 2 when it's the first in the stack
// (after PR 1 was already merged via GitHub)
func TestRunMerge_Position2AfterMerge(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	// Only 2 commits remain after PR 1 was merged
	mockGit.Commits = []git.Commit{
		{Hash: "def5678", Subject: "Add user API"},
		{Hash: "ghi9012", Subject: "Add user tests"},
	}
	// Position 1 branch no longer exists - positions start at 2
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/02-add-user-api",
		"stack/feature/auth/03-add-user-tests",
	}

	mockGH := github.NewMockGitHub()
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

	// Should be able to merge position 1 (which is now "Add user API" since position 1 was removed from commits)
	err := runMerge(nil, []string{"1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify PR #101 was merged (position 2)
	if len(mockGH.MergedPRs) != 1 || mockGH.MergedPRs[0] != 101 {
		t.Errorf("expected PR #101 merged, got %v", mockGH.MergedPRs)
	}

	// Verify PR #102 (position 3, now first in remaining stack) was retargeted to main
	foundRetarget := false
	for _, call := range mockGH.Calls {
		if call.Method == "UpdatePullRequestBase" {
			if len(call.Args) >= 2 {
				number, _ := call.Args[0].(int)
				base, _ := call.Args[1].(string)
				if number == 102 && base == "main" {
					foundRetarget = true
				}
			}
		}
	}
	if !foundRetarget {
		t.Error("expected PR #102 to be retargeted to main")
	}
}

// TestRunMerge_Position2NotFirstErrors tests that merging position 2 fails (must start from 1)
func TestRunMerge_Position2NotFirstErrors(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "def5678", Subject: "Add user API"},
		{Hash: "ghi9012", Subject: "Add user tests"},
	}
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/02-add-user-api",
		"stack/feature/auth/03-add-user-tests",
	}

	oldNewGit := mergeNewGit
	defer func() { mergeNewGit = oldNewGit }()
	mergeNewGit = func() git.GitOperations { return mockGit }

	// Try to merge stack position 2 (must start from 1)
	err := runMerge(nil, []string{"2"})
	if err == nil {
		t.Fatal("expected error for out-of-order merge")
	}

	// Error should mention must start from position 1
	if !containsSubstring(err.Error(), "must start from position 1") {
		t.Errorf("expected error mentioning must start from position 1, got %q", err.Error())
	}
}

// TestRunMerge_IgnoresOrphanedBranches tests that merge ignores remote branches
// that don't match any local commit (orphaned from previous merges/rebases)
func TestRunMerge_IgnoresOrphanedBranches(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	// Only 2 local commits exist
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user API"},
		{Hash: "def5678", Subject: "Add user tests"},
	}
	// But remote has an orphaned branch at position 2 (no matching local commit)
	// Local commits match positions 3 and 4
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/02-orphaned-commit-was-rebased-away", // Orphaned - no matching local commit
		"stack/feature/auth/03-add-user-api",                     // Matches first local commit
		"stack/feature/auth/04-add-user-tests",                   // Matches second local commit
	}

	mockGH := github.NewMockGitHub()
	// PR exists for the orphaned branch (but we should ignore it)
	mockGH.AddExistingPR("stack/feature/auth/02-orphaned-commit-was-rebased-away", 99, "Orphaned")
	// PRs for the actual commits
	mockGH.AddExistingPR("stack/feature/auth/03-add-user-api", 100, "Add user API")
	mockGH.AddExistingPR("stack/feature/auth/04-add-user-tests", 101, "Add user tests")

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
	mergeNoRebase = true // Skip rebase for simplicity

	// Should be able to merge stack position 1 (which matches branch position 3, ignoring orphaned)
	err := runMerge(nil, []string{"1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify PR #100 was merged (position 3, first matching local commit)
	if len(mockGH.MergedPRs) != 1 || mockGH.MergedPRs[0] != 100 {
		t.Errorf("expected PR #100 merged, got %v", mockGH.MergedPRs)
	}
}

// TestRunMerge_OrphanedBranchDoesNotBlockMerge tests that trying to merge
// position 3 works even when orphaned position 2 exists
func TestRunMerge_OrphanedBranchDoesNotBlockMerge(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	// Single local commit
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}
	// Remote has orphaned branch at position 1 and 2, local commit matches position 3
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-old-commit-merged",    // Orphaned
		"stack/feature/auth/02-another-old-commit",   // Orphaned
		"stack/feature/auth/03-add-user-model",       // Matches local commit
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/03-add-user-model", 100, "Add user model")

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

	// Should be able to merge stack position 1 (which matches branch position 3, orphaned 1 and 2 are ignored)
	err := runMerge(nil, []string{"1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify PR #100 was merged
	if len(mockGH.MergedPRs) != 1 || mockGH.MergedPRs[0] != 100 {
		t.Errorf("expected PR #100 merged, got %v", mockGH.MergedPRs)
	}
}

// TestRunMerge_AlreadyMergedPR tests that trying to merge an already-merged PR gives a helpful error
func TestRunMerge_AlreadyMergedPR(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
	}

	mockGH := github.NewMockGitHub()
	// Add PR as already closed/merged (not open)
	mockGH.AddExistingPRWithState("stack/feature/auth/01-add-user-model", 77, "Add user model", "closed")

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
	if err == nil {
		t.Fatal("expected error when PR is already merged")
	}

	// Error should mention the PR number and that it's already merged
	if !containsSubstring(err.Error(), "#77") {
		t.Errorf("expected error to mention PR #77, got %q", err.Error())
	}
	if !containsSubstring(err.Error(), "already closed/merged") {
		t.Errorf("expected error to say 'already closed/merged', got %q", err.Error())
	}
	if !containsSubstring(err.Error(), "rebase") {
		t.Errorf("expected error to mention rebase as fix, got %q", err.Error())
	}
}

// TestParsePositionRange tests parsing of position range arguments
func TestParsePositionRange(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    []int
		expectError bool
		errorSubstr string
	}{
		{"single position", "5", []int{5}, false, ""},
		{"range 1-3", "1-3", []int{1, 2, 3}, false, ""},
		{"range 4-6", "4-6", []int{4, 5, 6}, false, ""},
		{"single element range", "3-3", []int{3}, false, ""},
		{"invalid format", "1-2-3", nil, true, "invalid range format"},
		{"start > end", "5-3", nil, true, "start 5 > end 3"},
		{"non-numeric start", "a-3", nil, true, "invalid start position"},
		{"non-numeric end", "1-b", nil, true, "invalid end position"},
		{"non-numeric single", "abc", nil, true, "invalid position"},
		{"zero position", "0", nil, true, "position must be positive"},
		{"negative start", "-1-3", nil, true, "invalid range format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parsePositionRange(tt.input)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorSubstr)
				} else if !containsSubstring(err.Error(), tt.errorSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errorSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(result) != len(tt.expected) {
					t.Errorf("expected %v, got %v", tt.expected, result)
				}
				for i := range result {
					if result[i] != tt.expected[i] {
						t.Errorf("position %d: expected %d, got %d", i, tt.expected[i], result[i])
					}
				}
			}
		})
	}
}

// TestRunMerge_RangeMerge tests merging multiple PRs at once
func TestRunMerge_RangeMerge(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
		{Hash: "ghi9012", Subject: "Add user tests"},
		{Hash: "jkl3456", Subject: "Add user docs"},
	}
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-add-user-model",
		"stack/feature/auth/02-add-user-api",
		"stack/feature/auth/03-add-user-tests",
		"stack/feature/auth/04-add-user-docs",
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 101, "Add user API")
	mockGH.AddExistingPR("stack/feature/auth/03-add-user-tests", 102, "Add user tests")
	mockGH.AddExistingPR("stack/feature/auth/04-add-user-docs", 103, "Add user docs")

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

	// Merge positions 1-3
	err := runMerge(nil, []string{"1-3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify PRs 100, 101, 102 were merged
	if len(mockGH.MergedPRs) != 3 {
		t.Errorf("expected 3 PRs merged, got %d: %v", len(mockGH.MergedPRs), mockGH.MergedPRs)
	}
	expectedMerged := []int{100, 101, 102}
	for i, expected := range expectedMerged {
		if i >= len(mockGH.MergedPRs) || mockGH.MergedPRs[i] != expected {
			t.Errorf("expected PR #%d merged at position %d", expected, i)
		}
	}

	// Verify PR #103 was retargeted to main (it's now the first remaining)
	foundRetarget := false
	for _, call := range mockGH.Calls {
		if call.Method == "UpdatePullRequestBase" {
			if len(call.Args) >= 2 {
				number, _ := call.Args[0].(int)
				base, _ := call.Args[1].(string)
				if number == 103 && base == "main" {
					foundRetarget = true
				}
			}
		}
	}
	if !foundRetarget {
		t.Error("expected PR #103 to be retargeted to main")
	}
}

// TestRunMerge_RangeMustStartAtFirst tests that range must start at first position
func TestRunMerge_RangeMustStartAtFirst(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
		{Hash: "ghi9012", Subject: "Add user tests"},
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

	oldNewGit := mergeNewGit
	oldNewGitHub := mergeNewGitHub
	defer func() {
		mergeNewGit = oldNewGit
		mergeNewGitHub = oldNewGitHub
	}()
	mergeNewGit = func() git.GitOperations { return mockGit }
	mergeNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	// Try to merge 2-3 without merging 1 first
	err := runMerge(nil, []string{"2-3"})
	if err == nil {
		t.Fatal("expected error when not starting from first position")
	}

	if !containsSubstring(err.Error(), "must start from position 1") {
		t.Errorf("expected error about starting from position 1, got %q", err.Error())
	}
}
