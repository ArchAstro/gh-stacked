package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArchAstro/gh-stacked/internal/git"
	"github.com/ArchAstro/gh-stacked/internal/github"
)

func runRebaseIntegrationGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeRebaseIntegrationFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowGuardDetectsAnEarlierChangeRevertedAtStackTip(t *testing.T) {
	workDir := t.TempDir()
	runRebaseIntegrationGit(t, "", "init", workDir)
	runRebaseIntegrationGit(t, workDir, "config", "user.name", "Stack Author")
	runRebaseIntegrationGit(t, workDir, "config", "user.email", "stack@example.com")
	writeRebaseIntegrationFile(t, filepath.Join(workDir, "README.md"), "base\n")
	runRebaseIntegrationGit(t, workDir, "add", "README.md")
	runRebaseIntegrationGit(t, workDir, "commit", "-m", "Initial base")

	workflowDir := filepath.Join(workDir, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(workflowDir, "deploy.yml")
	writeRebaseIntegrationFile(t, workflowPath, "on: push\n")
	runRebaseIntegrationGit(t, workDir, "add", ".github/workflows/deploy.yml")
	runRebaseIntegrationGit(t, workDir, "commit", "-m", "Add workflow")
	addOID := runRebaseIntegrationGit(t, workDir, "rev-parse", "HEAD")

	if err := os.Remove(workflowPath); err != nil {
		t.Fatal(err)
	}
	runRebaseIntegrationGit(t, workDir, "add", ".github/workflows/deploy.yml")
	runRebaseIntegrationGit(t, workDir, "commit", "-m", "Revert workflow")
	revertOID := runRebaseIntegrationGit(t, workDir, "rev-parse", "HEAD")

	// The cumulative tip matches the base under .github/workflows, but the
	// first PR ref would still publish a workflow change. Inspecting each real
	// commit must catch that intermediate ref before any publication.
	oldWorkingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWorkingDir) })
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	changed, err := rebasedStackChangesWorkflows(git.New(), []git.Commit{
		{Hash: addOID},
		{Hash: revertOID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("workflow guard missed an earlier change reverted at the stack tip")
	}
}

func TestCommentRequestedRebasePublishesTheCompleteStackOnLatestBase(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "work")
	remoteDir := filepath.Join(t.TempDir(), "origin.git")

	// Establish a real repository boundary: the command works in one checkout
	// while all externally observable assertions read from a separate bare remote.
	runRebaseIntegrationGit(t, "", "init", "--bare", remoteDir)
	runRebaseIntegrationGit(t, "", "init", workDir)
	runRebaseIntegrationGit(t, workDir, "config", "user.name", "Stack Author")
	runRebaseIntegrationGit(t, workDir, "config", "user.email", "stack@example.com")
	writeRebaseIntegrationFile(t, filepath.Join(workDir, "base.txt"), "initial\n")
	runRebaseIntegrationGit(t, workDir, "add", "base.txt")
	runRebaseIntegrationGit(t, workDir, "commit", "-m", "Initial base")
	runRebaseIntegrationGit(t, workDir, "branch", "-M", "main")
	runRebaseIntegrationGit(t, workDir, "remote", "add", "origin", remoteDir)
	runRebaseIntegrationGit(t, workDir, "push", "-u", "origin", "main")

	// Build the same cumulative one-commit-per-PR refs produced by gh stacked.
	runRebaseIntegrationGit(t, workDir, "checkout", "-b", "feature/comment-rebase")
	writeRebaseIntegrationFile(t, filepath.Join(workDir, "model.txt"), "model\n")
	runRebaseIntegrationGit(t, workDir, "add", "model.txt")
	runRebaseIntegrationGit(t, workDir, "commit", "-m", "Add model", "-m", "Body with ---COMMIT_RECORD--- text")
	rootBranch := "stack/feature/comment-rebase/01-add-model"
	runRebaseIntegrationGit(t, workDir, "branch", rootBranch)

	writeRebaseIntegrationFile(t, filepath.Join(workDir, "api.txt"), "api\n")
	runRebaseIntegrationGit(t, workDir, "add", "api.txt")
	runRebaseIntegrationGit(t, workDir, "commit", "-m", "Add API", "-m", "Preserve this body too")
	tipBranch := "stack/feature/comment-rebase/02-add-api"
	runRebaseIntegrationGit(t, workDir, "branch", tipBranch)
	runRebaseIntegrationGit(t, workDir, "push", "origin", rootBranch, tipBranch)

	// Move main independently so a successful command must rewrite both PR refs.
	runRebaseIntegrationGit(t, workDir, "checkout", "main")
	writeRebaseIntegrationFile(t, filepath.Join(workDir, "base.txt"), "latest\n")
	runRebaseIntegrationGit(t, workDir, "commit", "-am", "Advance base")
	runRebaseIntegrationGit(t, workDir, "push", "origin", "main")
	runRebaseIntegrationGit(t, workDir, "fetch", "origin", "--prune")
	originalHead := runRebaseIntegrationGit(t, workDir, "rev-parse", "HEAD")
	originalStatus := runRebaseIntegrationGit(t, workDir, "status", "--porcelain")

	// Cross the command boundary with real Git subprocesses and remote I/O.
	mockGitHub := github.NewMockGitHub()
	mockGitHub.AddExistingPRWithBase(rootBranch, 101, "Add model", "open", false, "main")
	mockGitHub.AddExistingPRWithBase(tipBranch, 102, "Add API", "open", false, rootBranch)

	oldGit := rebaseNewGit
	oldGitHub := rebaseNewGitHub
	oldPR := rebasePR
	oldActor := rebaseActor
	oldDryRun := rebaseDryRun
	oldRejectWorkflowChanges := rebaseRejectWorkflowChanges
	oldJSON := jsonOutput
	oldWorkingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rebaseNewGit = oldGit
		rebaseNewGitHub = oldGitHub
		rebasePR = oldPR
		rebaseActor = oldActor
		rebaseDryRun = oldDryRun
		rebaseRejectWorkflowChanges = oldRejectWorkflowChanges
		jsonOutput = oldJSON
		_ = os.Chdir(oldWorkingDir)
	})
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	rebaseNewGit = func() git.GitOperations { return git.New() }
	rebaseNewGitHub = func() (github.GitHubOperations, error) { return mockGitHub, nil }
	rebasePR = 102
	rebaseActor = ""
	rebaseDryRun = false
	rebaseRejectWorkflowChanges = false
	jsonOutput = false

	if err := runRebase(nil, nil); err != nil {
		t.Fatalf("runRebase failed: %v", err)
	}

	// Observe the published story from the remote: both refs moved together,
	// preserve their messages, and form a direct chain from the latest main.
	runRebaseIntegrationGit(t, workDir, "fetch", "origin", "--prune")
	mainOID := runRebaseIntegrationGit(t, workDir, "rev-parse", "origin/main")
	rootOID := runRebaseIntegrationGit(t, workDir, "rev-parse", "origin/"+rootBranch)
	tipOID := runRebaseIntegrationGit(t, workDir, "rev-parse", "origin/"+tipBranch)
	if rootParent := runRebaseIntegrationGit(t, workDir, "rev-parse", rootOID+"^"); rootParent != mainOID {
		t.Fatalf("root PR is based on %s, want latest main %s", rootParent, mainOID)
	}
	if tipParent := runRebaseIntegrationGit(t, workDir, "rev-parse", tipOID+"^"); tipParent != rootOID {
		t.Fatalf("tip PR is based on %s, want rewritten root %s", tipParent, rootOID)
	}
	if subject := runRebaseIntegrationGit(t, workDir, "show", "-s", "--format=%s", rootOID); subject != "Add model" {
		t.Fatalf("root subject changed: %q", subject)
	}
	if body := runRebaseIntegrationGit(t, workDir, "show", "-s", "--format=%b", rootOID); body != "Body with ---COMMIT_RECORD--- text" {
		t.Fatalf("root body changed: %q", body)
	}
	if subject := runRebaseIntegrationGit(t, workDir, "show", "-s", "--format=%s", tipOID); subject != "Add API" {
		t.Fatalf("tip subject changed: %q", subject)
	}

	// The automation must never disturb a developer checkout while doing this.
	if currentHead := runRebaseIntegrationGit(t, workDir, "rev-parse", "HEAD"); currentHead != originalHead {
		t.Fatalf("caller HEAD moved from %s to %s", originalHead, currentHead)
	}
	if branch := runRebaseIntegrationGit(t, workDir, "branch", "--show-current"); branch != "main" {
		t.Fatalf("caller branch changed to %q", branch)
	}
	if status := runRebaseIntegrationGit(t, workDir, "status", "--porcelain"); status != originalStatus {
		t.Fatalf("caller worktree changed:\n%s", status)
	}
}
