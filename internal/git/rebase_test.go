package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGitTestCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func setupRemoteStack(t *testing.T) (workDir string, branches []string) {
	t.Helper()
	workDir = filepath.Join(t.TempDir(), "work")
	remoteDir := filepath.Join(t.TempDir(), "remote.git")

	runGitTestCommand(t, "", "init", "--bare", remoteDir)
	runGitTestCommand(t, "", "init", workDir)
	runGitTestCommand(t, workDir, "config", "user.email", "test@example.com")
	runGitTestCommand(t, workDir, "config", "user.name", "Test")
	runGitTestCommand(t, workDir, "commit", "--allow-empty", "-m", "Initial")
	runGitTestCommand(t, workDir, "branch", "-M", "main")
	runGitTestCommand(t, workDir, "remote", "add", "origin", remoteDir)
	runGitTestCommand(t, workDir, "push", "-u", "origin", "main")

	runGitTestCommand(t, workDir, "checkout", "-b", "feature/auth")
	runGitTestCommand(t, workDir, "commit", "--allow-empty", "-m", "Add model")
	branches = append(branches, "stack/feature/auth/01-add-model")
	runGitTestCommand(t, workDir, "branch", branches[0])
	runGitTestCommand(t, workDir, "commit", "--allow-empty", "-m", "Add API")
	branches = append(branches, "stack/feature/auth/02-add-api")
	runGitTestCommand(t, workDir, "branch", branches[1])
	runGitTestCommand(t, workDir, "push", "origin", branches[0], branches[1])

	runGitTestCommand(t, workDir, "checkout", "main")
	runGitTestCommand(t, workDir, "commit", "--allow-empty", "-m", "Main moved")
	runGitTestCommand(t, workDir, "push", "origin", "main")
	runGitTestCommand(t, workDir, "fetch", "origin")

	return workDir, branches
}

func TestPrepareStackRebaseUsesTemporaryWorktree(t *testing.T) {
	workDir, branches := setupRemoteStack(t)
	oldWD, _ := os.Getwd()
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)

	originalHead := runGitTestCommand(t, workDir, "rev-parse", "HEAD")
	g := New()
	result, err := g.PrepareStackRebase("origin/main", branches)
	if err != nil {
		t.Fatalf("PrepareStackRebase failed: %v", err)
	}

	if len(result.Commits) != 2 || result.Commits[0].Subject != "Add model" || result.Commits[1].Subject != "Add API" {
		t.Fatalf("unexpected rebased commits: %#v", result.Commits)
	}
	if len(result.OldOIDs) != 2 || result.OldOIDs[branches[0]] == "" || result.BaseOID == "" {
		t.Fatalf("missing old ref snapshots: %#v", result.OldOIDs)
	}
	if current := runGitTestCommand(t, workDir, "rev-parse", "HEAD"); current != originalHead {
		t.Fatalf("current worktree moved from %s to %s", originalHead, current)
	}
	if branch := runGitTestCommand(t, workDir, "branch", "--show-current"); branch != "main" {
		t.Fatalf("current branch changed to %q", branch)
	}
	if worktrees := runGitTestCommand(t, workDir, "worktree", "list", "--porcelain"); strings.Contains(worktrees, "gh-stacked-rebase-") {
		t.Fatalf("temporary worktree registration leaked:\n%s", worktrees)
	}
}

func TestPrepareStackRebaseRejectsRootWithMultipleCommits(t *testing.T) {
	workDir, branches := setupRemoteStack(t)
	oldWD, _ := os.Getwd()
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)

	tipOID := runGitTestCommand(t, workDir, "rev-parse", "origin/"+branches[1])
	runGitTestCommand(t, workDir, "branch", "-f", branches[0], tipOID)
	runGitTestCommand(t, workDir, "update-ref", "refs/remotes/origin/"+branches[0], tipOID)

	_, err := New().PrepareStackRebase("origin/main", branches)
	if err == nil || !strings.Contains(err.Error(), "expected exactly 1") {
		t.Fatalf("expected multi-commit root rejection, got %v", err)
	}
}

func TestPrepareStackRebaseRejectsBrokenDirectParentChain(t *testing.T) {
	workDir, branches := setupRemoteStack(t)
	oldWD, _ := os.Getwd()
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)

	mainOID := runGitTestCommand(t, workDir, "rev-parse", "origin/main")
	runGitTestCommand(t, workDir, "update-ref", "refs/remotes/origin/"+branches[1], mainOID)

	_, err := New().PrepareStackRebase("origin/main", branches)
	if err == nil || !strings.Contains(err.Error(), "directly based") {
		t.Fatalf("expected direct-parent rejection, got %v", err)
	}
}

func TestAtomicPushStackRefsIsAllOrNothingOnLeaseFailure(t *testing.T) {
	workDir, branches := setupRemoteStack(t)
	oldWD, _ := os.Getwd()
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)

	g := New()
	result, err := g.PrepareStackRebase("origin/main", branches)
	if err != nil {
		t.Fatalf("PrepareStackRebase failed: %v", err)
	}

	updates := []RefUpdate{
		{Branch: branches[0], OldOID: result.OldOIDs[branches[0]], NewOID: result.Commits[0].Hash},
		{Branch: branches[1], OldOID: strings.Repeat("0", 40), NewOID: result.Commits[1].Hash},
	}
	if err := g.AtomicPushStackRefs("origin", updates); err == nil {
		t.Fatal("expected stale lease to reject atomic push")
	}

	for _, branch := range branches {
		remoteOID := runGitTestCommand(t, workDir, "ls-remote", "origin", "refs/heads/"+branch)
		if !strings.HasPrefix(remoteOID, result.OldOIDs[branch]) {
			t.Fatalf("remote branch %s changed despite atomic push failure: %s", branch, remoteOID)
		}
	}
}

func TestAtomicPushStackRefsUpdatesWholeStack(t *testing.T) {
	workDir, branches := setupRemoteStack(t)
	oldWD, _ := os.Getwd()
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)

	g := New()
	result, err := g.PrepareStackRebase("origin/main", branches)
	if err != nil {
		t.Fatalf("PrepareStackRebase failed: %v", err)
	}
	updates := []RefUpdate{
		{Branch: branches[0], OldOID: result.OldOIDs[branches[0]], NewOID: result.Commits[0].Hash},
		{Branch: branches[1], OldOID: result.OldOIDs[branches[1]], NewOID: result.Commits[1].Hash},
	}
	if err := g.AtomicPushStackRefs("origin", updates); err != nil {
		t.Fatalf("AtomicPushStackRefs failed: %v", err)
	}

	for i, branch := range branches {
		remoteOID := runGitTestCommand(t, workDir, "ls-remote", "origin", "refs/heads/"+branch)
		if !strings.HasPrefix(remoteOID, result.Commits[i].Hash) {
			t.Fatalf("remote branch %s was not updated to %s: %s", branch, result.Commits[i].Hash, remoteOID)
		}
	}
}

func TestRemoteRefOIDObservesBaseMovementAfterPreflight(t *testing.T) {
	workDir, branches := setupRemoteStack(t)
	oldWD, _ := os.Getwd()
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)

	g := New()
	result, err := g.PrepareStackRebase("origin/main", branches)
	if err != nil {
		t.Fatalf("PrepareStackRebase failed: %v", err)
	}
	runGitTestCommand(t, workDir, "commit", "--allow-empty", "-m", "Main raced")
	runGitTestCommand(t, workDir, "push", "origin", "main")

	remoteBaseOID, err := g.RemoteRefOID("origin", "main")
	if err != nil {
		t.Fatalf("RemoteRefOID failed: %v", err)
	}
	if remoteBaseOID == result.BaseOID {
		t.Fatalf("expected remote base to move from %s", result.BaseOID)
	}
}

func TestAtomicPushStackRefsValidatesAllUpdates(t *testing.T) {
	g := New()
	if err := g.AtomicPushStackRefs("origin", nil); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty update rejection, got %v", err)
	}
	update := []RefUpdate{{Branch: "", OldOID: "old", NewOID: "new"}}
	if err := g.AtomicPushStackRefs("origin", update); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected invalid update rejection, got %v", err)
	}
}

func TestRemoteRefOIDValidatesTarget(t *testing.T) {
	if _, err := New().RemoteRefOID("", "main"); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected missing remote rejection, got %v", err)
	}
}

func TestPrepareStackRebaseReturnsRebasedPrefixAtFirstConflict(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "work")
	remoteDir := filepath.Join(t.TempDir(), "remote.git")
	runGitTestCommand(t, "", "init", "--bare", remoteDir)
	runGitTestCommand(t, "", "init", workDir)
	runGitTestCommand(t, workDir, "config", "user.email", "test@example.com")
	runGitTestCommand(t, workDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(workDir, "value.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, workDir, "add", "value.txt")
	runGitTestCommand(t, workDir, "commit", "-m", "Initial")
	runGitTestCommand(t, workDir, "branch", "-M", "main")
	runGitTestCommand(t, workDir, "remote", "add", "origin", remoteDir)
	runGitTestCommand(t, workDir, "push", "-u", "origin", "main")

	runGitTestCommand(t, workDir, "checkout", "-b", "feature/conflict")
	if err := os.WriteFile(filepath.Join(workDir, "safe.txt"), []byte("safe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, workDir, "add", "safe.txt")
	runGitTestCommand(t, workDir, "commit", "-m", "Safe prefix")
	safeBranch := "stack/feature/conflict/01-safe-prefix"
	runGitTestCommand(t, workDir, "branch", safeBranch)

	if err := os.WriteFile(filepath.Join(workDir, "value.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, workDir, "commit", "-am", "Feature change")
	blockedBranch := "stack/feature/conflict/02-feature-change"
	runGitTestCommand(t, workDir, "branch", blockedBranch)
	runGitTestCommand(t, workDir, "push", "origin", safeBranch, blockedBranch)

	runGitTestCommand(t, workDir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(workDir, "value.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, workDir, "commit", "-am", "Main change")
	runGitTestCommand(t, workDir, "push", "origin", "main")
	runGitTestCommand(t, workDir, "fetch", "origin")
	originalHead := runGitTestCommand(t, workDir, "rev-parse", "HEAD")

	oldWD, _ := os.Getwd()
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)

	result, err := New().PrepareStackRebase("origin/main", []string{safeBranch, blockedBranch})
	if err != nil {
		t.Fatalf("expected a partial result, got error %v", err)
	}
	if len(result.Commits) != 1 || result.Commits[0].Subject != "Safe prefix" {
		t.Fatalf("expected one safely rebased commit, got %#v", result.Commits)
	}
	if result.BlockedBranch != blockedBranch || !strings.Contains(result.BlockedReason, "conflicts") {
		t.Fatalf("expected conflict at %s, got branch %q reason %q", blockedBranch, result.BlockedBranch, result.BlockedReason)
	}
	if current := runGitTestCommand(t, workDir, "rev-parse", "HEAD"); current != originalHead {
		t.Fatalf("caller HEAD changed after conflict: %s -> %s", originalHead, current)
	}
	if branchName := runGitTestCommand(t, workDir, "branch", "--show-current"); branchName != "main" {
		t.Fatalf("caller branch changed after conflict: %s", branchName)
	}
}

func TestPrepareStackRebaseDoesNotClassifyOperationalFailureAsConflict(t *testing.T) {
	workDir, branches := setupRemoteStack(t)
	oldWD, _ := os.Getwd()
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)

	// Force commit creation to fail without producing unmerged index entries.
	// Operational failures must abort the whole preparation, not publish a
	// misleading "safe prefix."
	runGitTestCommand(t, workDir, "config", "commit.gpgsign", "true")
	runGitTestCommand(t, workDir, "config", "user.signingkey", "definitely-not-a-real-test-key")

	result, err := New().PrepareStackRebase("origin/main", branches)
	if err == nil || !strings.Contains(err.Error(), "replay stack commit") {
		t.Fatalf("expected operational cherry-pick failure, got result %#v error %v", result, err)
	}
}
