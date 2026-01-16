package cmd

import (
	"errors"
	"testing"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/github"
)

func TestMergeCommand(t *testing.T) {
	cmd := mergeCmd

	if cmd.Use != "merge" {
		t.Errorf("expected Use %q, got %s", "merge", cmd.Use)
	}
}

func TestRunMerge_Success(t *testing.T) {
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

	err := runMerge(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify PR #100 was merged
	if len(mockGH.MergedPRs) != 1 || mockGH.MergedPRs[0] != 100 {
		t.Errorf("expected PR #100 merged, got %v", mockGH.MergedPRs)
	}

	// Verify PR #101 was retargeted to main
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

	// Verify rebase was performed
	foundRebase := false
	for _, call := range mockGit.Calls {
		if call.Method == "Run" {
			args, ok := call.Args[0].([]string)
			if ok && len(args) >= 1 && args[0] == "rebase" {
				foundRebase = true
			}
		}
	}
	if !foundRebase {
		t.Error("expected rebase to be called")
	}
}

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

	err := runMerge(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify PR was merged
	if len(mockGH.MergedPRs) != 1 {
		t.Errorf("expected 1 PR merged, got %d", len(mockGH.MergedPRs))
	}

	// Should not have rebased
	for _, call := range mockGit.Calls {
		if call.Method == "Run" {
			args, ok := call.Args[0].([]string)
			if ok && len(args) > 0 && args[0] == "rebase" {
				t.Error("should not rebase with --no-rebase flag")
			}
		}
	}

	// Should not have retargeted
	for _, call := range mockGH.Calls {
		if call.Method == "UpdatePullRequestBase" {
			t.Error("should not retarget PRs with --no-rebase flag")
		}
	}
}

func TestRunMerge_SinglePRStack(t *testing.T) {
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

	err := runMerge(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify PR was merged
	if len(mockGH.MergedPRs) != 1 || mockGH.MergedPRs[0] != 100 {
		t.Errorf("expected PR #100 merged, got %v", mockGH.MergedPRs)
	}

	// Should not have retargeted (no remaining PRs)
	for _, call := range mockGH.Calls {
		if call.Method == "UpdatePullRequestBase" {
			t.Error("should not retarget when no remaining PRs")
		}
	}

	// Should still rebase even with single PR
	foundRebase := false
	for _, call := range mockGit.Calls {
		if call.Method == "Run" {
			args, ok := call.Args[0].([]string)
			if ok && len(args) >= 1 && args[0] == "rebase" {
				foundRebase = true
			}
		}
	}
	if !foundRebase {
		t.Error("expected rebase to be called even for single PR stack")
	}
}

func TestRunMerge_RejectsStackBranch(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "stack/feature/auth/01-add-user-model"

	oldNewGit := mergeNewGit
	defer func() { mergeNewGit = oldNewGit }()
	mergeNewGit = func() git.GitOperations { return mockGit }

	err := runMerge(nil, []string{})
	if err == nil {
		t.Fatal("expected error when running from stack branch")
	}

	if err.Error() != "run merge from your feature branch, not a stack branch" {
		t.Errorf("unexpected error: %q", err.Error())
	}
}

func TestRunMerge_NoPRsFound(t *testing.T) {
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
	// No PRs added

	oldNewGit := mergeNewGit
	oldNewGitHub := mergeNewGitHub
	defer func() {
		mergeNewGit = oldNewGit
		mergeNewGitHub = oldNewGitHub
	}()
	mergeNewGit = func() git.GitOperations { return mockGit }
	mergeNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runMerge(nil, []string{})
	if err == nil {
		t.Fatal("expected error when no PRs found")
	}

	if !containsSubstring(err.Error(), "no open PRs found") {
		t.Errorf("expected error about no open PRs, got %q", err.Error())
	}
}

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

	err := runMerge(nil, []string{})
	if err == nil {
		t.Fatal("expected error when merge fails")
	}

	if !containsSubstring(err.Error(), "failed to merge PR") {
		t.Errorf("expected error about failed merge, got %q", err.Error())
	}
}

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

	err := runMerge(nil, []string{})
	if err == nil {
		t.Fatal("expected error when retarget fails")
	}

	if !containsSubstring(err.Error(), "failed to retarget") {
		t.Errorf("expected error about retarget failure, got %q", err.Error())
	}
}

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

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-add-user-model", 100, "Add user model")
	mockGH.AddExistingPR("stack/feature/auth/02-add-user-api", 101, "Add user API")

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

	err := runMerge(nil, []string{})
	if err == nil {
		t.Fatal("expected error when rebase has conflict")
	}

	if !containsSubstring(err.Error(), "conflict") {
		t.Errorf("expected error mentioning conflict, got %q", err.Error())
	}

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

func TestRunMerge_NoCommits(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{}

	oldNewGit := mergeNewGit
	defer func() { mergeNewGit = oldNewGit }()
	mergeNewGit = func() git.GitOperations { return mockGit }

	err := runMerge(nil, []string{})
	if err == nil {
		t.Fatal("expected error when no commits")
	}

	if !containsSubstring(err.Error(), "no commits in stack") {
		t.Errorf("expected error about no commits, got %q", err.Error())
	}
}

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

	err := runMerge(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify only PR #100 was merged
	if len(mockGH.MergedPRs) != 1 || mockGH.MergedPRs[0] != 100 {
		t.Errorf("expected only PR #100 merged, got %v", mockGH.MergedPRs)
	}

	// Verify PR #101 was retargeted to main
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

	err := runMerge(nil, []string{})
	if err == nil {
		t.Fatal("expected error when GitHub client creation fails")
	}

	if !containsSubstring(err.Error(), "failed to create GitHub client") {
		t.Errorf("expected error about GitHub client creation, got %q", err.Error())
	}
}

func TestRunMerge_IgnoresOrphanedBranches(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user API"},
		{Hash: "def5678", Subject: "Add user tests"},
	}
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/02-orphaned-commit",
		"stack/feature/auth/03-add-user-api",
		"stack/feature/auth/04-add-user-tests",
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/02-orphaned-commit", 99, "Orphaned")
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
	mergeNoRebase = true

	err := runMerge(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify PR #100 was merged (matches first local commit)
	if len(mockGH.MergedPRs) != 1 || mockGH.MergedPRs[0] != 100 {
		t.Errorf("expected PR #100 merged, got %v", mockGH.MergedPRs)
	}
}

func TestRunMerge_ClosedPRsIgnored(t *testing.T) {
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
	mockGH.AddExistingPRWithState("stack/feature/auth/01-add-user-model", 77, "Add user model", "closed")

	oldNewGit := mergeNewGit
	oldNewGitHub := mergeNewGitHub
	defer func() {
		mergeNewGit = oldNewGit
		mergeNewGitHub = oldNewGitHub
	}()
	mergeNewGit = func() git.GitOperations { return mockGit }
	mergeNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runMerge(nil, []string{})
	if err == nil {
		t.Fatal("expected error when PR is already closed")
	}

	if !containsSubstring(err.Error(), "no open PRs found") {
		t.Errorf("expected error about no open PRs, got %q", err.Error())
	}
}

func TestRunMerge_RetargetBeforeMerge(t *testing.T) {
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

	err := runMerge(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify retarget happens before merge
	retargetIdx := -1
	mergeIdx := -1
	for i, call := range mockGH.Calls {
		if call.Method == "UpdatePullRequestBase" && retargetIdx == -1 {
			retargetIdx = i
		}
		if call.Method == "MergePullRequest" && mergeIdx == -1 {
			mergeIdx = i
		}
	}

	if retargetIdx == -1 {
		t.Fatal("no retarget call found")
	}
	if mergeIdx == -1 {
		t.Fatal("no merge call found")
	}
	if retargetIdx > mergeIdx {
		t.Errorf("retarget (index %d) should happen before merge (index %d)", retargetIdx, mergeIdx)
	}
}
