// internal/git/git_test.go
package git

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "Initial commit"},
		{"git", "branch", "-M", "main"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Fatalf("setup command %v failed: %v", args, err)
		}
	}

	return dir
}

func TestGetBaseBranch(t *testing.T) {
	dir := setupTestRepo(t)
	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	// Create feature branch
	exec.Command("git", "checkout", "-b", "feature/test").Run()
	exec.Command("git", "commit", "--allow-empty", "-m", "Feature commit").Run()

	g := New()
	base, err := g.GetBaseBranch()
	if err != nil {
		t.Fatalf("GetBaseBranch failed: %v", err)
	}
	if base != "main" {
		t.Errorf("expected base 'main', got '%s'", base)
	}
}

func TestGetCurrentBranch(t *testing.T) {
	dir := setupTestRepo(t)
	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	exec.Command("git", "checkout", "-b", "feature/my-feature").Run()

	g := New()
	branch, err := g.GetCurrentBranch()
	if err != nil {
		t.Fatalf("GetCurrentBranch failed: %v", err)
	}
	if branch != "feature/my-feature" {
		t.Errorf("expected 'feature/my-feature', got '%s'", branch)
	}
}

func TestGetCommitsSinceBase(t *testing.T) {
	dir := setupTestRepo(t)
	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	// Create feature branch with 2 commits
	exec.Command("git", "checkout", "-b", "feature/test").Run()
	exec.Command("git", "commit", "--allow-empty", "-m", "First feature commit").Run()
	exec.Command("git", "commit", "--allow-empty", "-m", "Second feature commit").Run()

	g := New()
	commits, err := g.GetCommitsSinceBase("main")
	if err != nil {
		t.Fatalf("GetCommitsSinceBase failed: %v", err)
	}
	if len(commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].Subject != "First feature commit" {
		t.Errorf("expected 'First feature commit', got '%s'", commits[0].Subject)
	}
	if commits[1].Subject != "Second feature commit" {
		t.Errorf("expected 'Second feature commit', got '%s'", commits[1].Subject)
	}
}

func TestCreateBranchAtCommit(t *testing.T) {
	dir := setupTestRepo(t)
	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	// Get the commit hash
	out, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	hash := strings.TrimSpace(string(out))

	g := New()
	err := g.CreateBranchAtCommit("test-branch", hash)
	if err != nil {
		t.Fatalf("CreateBranchAtCommit failed: %v", err)
	}

	// Verify branch exists
	_, err = exec.Command("git", "rev-parse", "--verify", "test-branch").Output()
	if err != nil {
		t.Error("branch was not created")
	}
}

func TestCreateFixupCommit(t *testing.T) {
	dir := setupTestRepo(t)
	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	// Create a commit to fixup
	exec.Command("git", "commit", "--allow-empty", "-m", "Original commit").Run()
	out, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	targetHash := strings.TrimSpace(string(out))

	// Create a file to stage
	os.WriteFile("test.txt", []byte("content"), 0644)
	exec.Command("git", "add", "test.txt").Run()

	g := New()
	err := g.CreateFixupCommit(targetHash)
	if err != nil {
		t.Fatalf("CreateFixupCommit failed: %v", err)
	}

	// Verify fixup commit was created
	out, _ = exec.Command("git", "log", "-1", "--format=%s").Output()
	subject := strings.TrimSpace(string(out))
	if !strings.HasPrefix(subject, "fixup! ") {
		t.Errorf("expected fixup commit, got '%s'", subject)
	}
}
