package cmd

import (
	"errors"
	"testing"

	"github.com/ArchAstro/gh-stacked/internal/git"
	"github.com/ArchAstro/gh-stacked/internal/github"
)

func TestMergeAllCommand(t *testing.T) {
	cmd := mergeAllCmd

	if cmd.Use != "merge-all" {
		t.Errorf("expected Use %q, got %s", "merge-all", cmd.Use)
	}
}

func TestRunMergeAll_Success(t *testing.T) {
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

	oldNewGit := mergeAllNewGit
	oldNewGitHub := mergeAllNewGitHub
	defer func() {
		mergeAllNewGit = oldNewGit
		mergeAllNewGitHub = oldNewGitHub
	}()
	mergeAllNewGit = func() git.GitOperations { return mockGit }
	mergeAllNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runMergeAll(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the LAST PR (#102) was merged
	if len(mockGH.MergedPRs) != 1 || mockGH.MergedPRs[0] != 102 {
		t.Errorf("expected PR #102 merged, got %v", mockGH.MergedPRs)
	}

	// Verify other PRs (#100, #101) were closed
	if len(mockGH.ClosedPRs) != 2 {
		t.Errorf("expected 2 PRs closed, got %d: %v", len(mockGH.ClosedPRs), mockGH.ClosedPRs)
	}
	closedSet := make(map[int]bool)
	for _, n := range mockGH.ClosedPRs {
		closedSet[n] = true
	}
	if !closedSet[100] || !closedSet[101] {
		t.Errorf("expected PRs #100 and #101 closed, got %v", mockGH.ClosedPRs)
	}

	// Verify PR #102 was retargeted to main
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

	// Verify squash commit was created (reset --soft + commit)
	foundReset := false
	foundCommit := false
	for _, call := range mockGit.Calls {
		if call.Method == "Run" {
			args, ok := call.Args[0].([]string)
			if ok && len(args) >= 2 {
				if args[0] == "reset" && args[1] == "--soft" {
					foundReset = true
				}
				if args[0] == "commit" {
					foundCommit = true
				}
			}
		}
	}
	if !foundReset {
		t.Error("expected reset --soft for squash")
	}
	if !foundCommit {
		t.Error("expected commit for squash")
	}
}

func TestRunMergeAll_SinglePR(t *testing.T) {
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

	oldNewGit := mergeAllNewGit
	oldNewGitHub := mergeAllNewGitHub
	defer func() {
		mergeAllNewGit = oldNewGit
		mergeAllNewGitHub = oldNewGitHub
	}()
	mergeAllNewGit = func() git.GitOperations { return mockGit }
	mergeAllNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runMergeAll(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the only PR was merged
	if len(mockGH.MergedPRs) != 1 || mockGH.MergedPRs[0] != 100 {
		t.Errorf("expected PR #100 merged, got %v", mockGH.MergedPRs)
	}

	// No other PRs to close
	if len(mockGH.ClosedPRs) != 0 {
		t.Errorf("expected no PRs closed, got %v", mockGH.ClosedPRs)
	}
}

func TestRunMergeAll_RejectsStackBranch(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "stack/feature/auth/01-add-user-model"

	oldNewGit := mergeAllNewGit
	defer func() { mergeAllNewGit = oldNewGit }()
	mergeAllNewGit = func() git.GitOperations { return mockGit }

	err := runMergeAll(nil, []string{})
	if err == nil {
		t.Fatal("expected error when running from stack branch")
	}

	if err.Error() != "run merge-all from your feature branch, not a stack branch" {
		t.Errorf("unexpected error: %q", err.Error())
	}
}

func TestRunMergeAll_NoPRsFound(t *testing.T) {
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

	oldNewGit := mergeAllNewGit
	oldNewGitHub := mergeAllNewGitHub
	defer func() {
		mergeAllNewGit = oldNewGit
		mergeAllNewGitHub = oldNewGitHub
	}()
	mergeAllNewGit = func() git.GitOperations { return mockGit }
	mergeAllNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runMergeAll(nil, []string{})
	if err == nil {
		t.Fatal("expected error when no PRs found")
	}

	if !containsSubstring(err.Error(), "no open PRs found") {
		t.Errorf("expected error about no open PRs, got %q", err.Error())
	}
}

func TestRunMergeAll_NoCommits(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{}

	oldNewGit := mergeAllNewGit
	defer func() { mergeAllNewGit = oldNewGit }()
	mergeAllNewGit = func() git.GitOperations { return mockGit }

	err := runMergeAll(nil, []string{})
	if err == nil {
		t.Fatal("expected error when no commits")
	}

	if !containsSubstring(err.Error(), "no commits in stack") {
		t.Errorf("expected error about no commits, got %q", err.Error())
	}
}

func TestRunMergeAll_MergeFails(t *testing.T) {
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
	mockGH.Errors["MergePullRequest"] = errors.New("merge blocked")

	oldNewGit := mergeAllNewGit
	oldNewGitHub := mergeAllNewGitHub
	defer func() {
		mergeAllNewGit = oldNewGit
		mergeAllNewGitHub = oldNewGitHub
	}()
	mergeAllNewGit = func() git.GitOperations { return mockGit }
	mergeAllNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runMergeAll(nil, []string{})
	if err == nil {
		t.Fatal("expected error when merge fails")
	}

	if !containsSubstring(err.Error(), "failed to merge PR") {
		t.Errorf("expected error about failed merge, got %q", err.Error())
	}
}

func TestRunMergeAll_ClosePRFailsNonFatal(t *testing.T) {
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
	mockGH.Errors["ClosePullRequest"] = errors.New("close failed")

	oldNewGit := mergeAllNewGit
	oldNewGitHub := mergeAllNewGitHub
	defer func() {
		mergeAllNewGit = oldNewGit
		mergeAllNewGitHub = oldNewGitHub
	}()
	mergeAllNewGit = func() git.GitOperations { return mockGit }
	mergeAllNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	// Should succeed even if close fails (non-fatal)
	err := runMergeAll(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Merge should still have happened
	if len(mockGH.MergedPRs) != 1 || mockGH.MergedPRs[0] != 101 {
		t.Errorf("expected PR #101 merged, got %v", mockGH.MergedPRs)
	}
}

func TestRunMergeAll_GitHubClientFails(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	oldNewGit := mergeAllNewGit
	oldNewGitHub := mergeAllNewGitHub
	defer func() {
		mergeAllNewGit = oldNewGit
		mergeAllNewGitHub = oldNewGitHub
	}()
	mergeAllNewGit = func() git.GitOperations { return mockGit }
	mergeAllNewGitHub = func() (github.GitHubOperations, error) {
		return nil, errors.New("auth failed")
	}

	err := runMergeAll(nil, []string{})
	if err == nil {
		t.Fatal("expected error when GitHub client fails")
	}

	if !containsSubstring(err.Error(), "failed to create GitHub client") {
		t.Errorf("expected error about GitHub client, got %q", err.Error())
	}
}

func TestRunMergeAll_RetargetFails(t *testing.T) {
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
	mockGH.Errors["UpdatePullRequestBase"] = errors.New("retarget failed")

	oldNewGit := mergeAllNewGit
	oldNewGitHub := mergeAllNewGitHub
	defer func() {
		mergeAllNewGit = oldNewGit
		mergeAllNewGitHub = oldNewGitHub
	}()
	mergeAllNewGit = func() git.GitOperations { return mockGit }
	mergeAllNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runMergeAll(nil, []string{})
	if err == nil {
		t.Fatal("expected error when retarget fails")
	}

	if !containsSubstring(err.Error(), "failed to retarget") {
		t.Errorf("expected error about retarget failure, got %q", err.Error())
	}
}

func TestRunMergeAll_OperationOrder(t *testing.T) {
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

	oldNewGit := mergeAllNewGit
	oldNewGitHub := mergeAllNewGitHub
	defer func() {
		mergeAllNewGit = oldNewGit
		mergeAllNewGitHub = oldNewGitHub
	}()
	mergeAllNewGit = func() git.GitOperations { return mockGit }
	mergeAllNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runMergeAll(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify order: retarget -> merge -> close
	retargetIdx := -1
	mergeIdx := -1
	closeIdx := -1

	for i, call := range mockGH.Calls {
		switch call.Method {
		case "UpdatePullRequestBase":
			if retargetIdx == -1 {
				retargetIdx = i
			}
		case "MergePullRequest":
			if mergeIdx == -1 {
				mergeIdx = i
			}
		case "ClosePullRequest":
			if closeIdx == -1 {
				closeIdx = i
			}
		}
	}

	if retargetIdx == -1 {
		t.Fatal("retarget not called")
	}
	if mergeIdx == -1 {
		t.Fatal("merge not called")
	}
	if closeIdx == -1 {
		t.Fatal("close not called")
	}

	if retargetIdx > mergeIdx {
		t.Errorf("retarget (%d) should happen before merge (%d)", retargetIdx, mergeIdx)
	}
	if mergeIdx > closeIdx {
		t.Errorf("merge (%d) should happen before close (%d)", mergeIdx, closeIdx)
	}
}

func TestRunMergeAll_FivePRStack(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.Commits = []git.Commit{
		{Hash: "a", Subject: "Commit 1"},
		{Hash: "b", Subject: "Commit 2"},
		{Hash: "c", Subject: "Commit 3"},
		{Hash: "d", Subject: "Commit 4"},
		{Hash: "e", Subject: "Commit 5"},
	}
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/01-commit-1",
		"stack/feature/auth/02-commit-2",
		"stack/feature/auth/03-commit-3",
		"stack/feature/auth/04-commit-4",
		"stack/feature/auth/05-commit-5",
	}

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPR("stack/feature/auth/01-commit-1", 100, "Commit 1")
	mockGH.AddExistingPR("stack/feature/auth/02-commit-2", 101, "Commit 2")
	mockGH.AddExistingPR("stack/feature/auth/03-commit-3", 102, "Commit 3")
	mockGH.AddExistingPR("stack/feature/auth/04-commit-4", 103, "Commit 4")
	mockGH.AddExistingPR("stack/feature/auth/05-commit-5", 104, "Commit 5")

	oldNewGit := mergeAllNewGit
	oldNewGitHub := mergeAllNewGitHub
	defer func() {
		mergeAllNewGit = oldNewGit
		mergeAllNewGitHub = oldNewGitHub
	}()
	mergeAllNewGit = func() git.GitOperations { return mockGit }
	mergeAllNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }

	err := runMergeAll(nil, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the last PR (#104) should be merged
	if len(mockGH.MergedPRs) != 1 || mockGH.MergedPRs[0] != 104 {
		t.Errorf("expected only PR #104 merged, got %v", mockGH.MergedPRs)
	}

	// PRs 100-103 should be closed
	if len(mockGH.ClosedPRs) != 4 {
		t.Errorf("expected 4 PRs closed, got %d: %v", len(mockGH.ClosedPRs), mockGH.ClosedPRs)
	}

	closedSet := make(map[int]bool)
	for _, n := range mockGH.ClosedPRs {
		closedSet[n] = true
	}
	for i := 100; i <= 103; i++ {
		if !closedSet[i] {
			t.Errorf("expected PR #%d to be closed", i)
		}
	}
}
