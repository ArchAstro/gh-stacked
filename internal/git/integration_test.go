// internal/git/integration_test.go
package git

import (
	"os"
	"os/exec"
	"testing"
)

func TestFullWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	// Initialize repo with main branch
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "Initial commit"},
		{"git", "branch", "-M", "main"},
		{"git", "checkout", "-b", "feature/test"},
		{"git", "commit", "--allow-empty", "-m", "First feature"},
		{"git", "commit", "--allow-empty", "-m", "Second feature"},
		{"git", "commit", "--allow-empty", "-m", "Third feature"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %v failed: %v\n%s", args, err, out)
		}
	}

	g := New()

	// Test GetBaseBranch
	base, err := g.GetBaseBranch()
	if err != nil {
		t.Fatalf("GetBaseBranch: %v", err)
	}
	if base != "main" {
		t.Errorf("expected base 'main', got '%s'", base)
	}

	// Test GetCurrentBranch
	branch, err := g.GetCurrentBranch()
	if err != nil {
		t.Fatalf("GetCurrentBranch: %v", err)
	}
	if branch != "feature/test" {
		t.Errorf("expected 'feature/test', got '%s'", branch)
	}

	// Test GetCommitsSinceBase
	commits, err := g.GetCommitsSinceBase("main")
	if err != nil {
		t.Fatalf("GetCommitsSinceBase: %v", err)
	}
	if len(commits) != 3 {
		t.Errorf("expected 3 commits, got %d", len(commits))
	}

	// Verify commit order (oldest first)
	expected := []string{"First feature", "Second feature", "Third feature"}
	for i, c := range commits {
		if c.Subject != expected[i] {
			t.Errorf("commit %d: expected '%s', got '%s'", i, expected[i], c.Subject)
		}
	}
}
