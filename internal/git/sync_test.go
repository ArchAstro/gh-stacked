// internal/git/sync_test.go
package git

import (
	"strings"
	"testing"
)

func TestSyncChecker_CheckStatus_InSync(t *testing.T) {
	mockGit := NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "origin/main"
	mockGit.Commits = []Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}
	mockGit.OnRun = func(args []string) (string, error) {
		if len(args) >= 2 && args[0] == "rev-list" && args[1] == "--count" {
			return "0", nil // Not behind
		}
		if len(args) >= 2 && args[0] == "log" && args[1] == "--oneline" {
			return "", nil // No merged commits found
		}
		if len(args) >= 2 && args[0] == "fetch" {
			return "", nil
		}
		return "", nil
	}

	checker := NewSyncChecker(mockGit)
	status, err := checker.CheckStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.NeedsRebase {
		t.Error("expected NeedsRebase=false when in sync")
	}
	if status.BehindBase {
		t.Error("expected BehindBase=false")
	}
	if status.HasMergedCommits {
		t.Error("expected HasMergedCommits=false")
	}
}

func TestSyncChecker_CheckStatus_BehindBase(t *testing.T) {
	mockGit := NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "origin/main"
	mockGit.Commits = []Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}
	mockGit.OnRun = func(args []string) (string, error) {
		if len(args) >= 2 && args[0] == "rev-list" && args[1] == "--count" {
			return "3", nil // 3 commits behind
		}
		if len(args) >= 2 && args[0] == "log" && args[1] == "--oneline" {
			return "", nil // No merged commits
		}
		return "", nil
	}

	checker := NewSyncChecker(mockGit)
	status, err := checker.CheckStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !status.NeedsRebase {
		t.Error("expected NeedsRebase=true when behind base")
	}
	if !status.BehindBase {
		t.Error("expected BehindBase=true")
	}
	if status.BehindCount != 3 {
		t.Errorf("expected BehindCount=3, got %d", status.BehindCount)
	}
}

func TestSyncChecker_CheckStatus_HasMergedCommits(t *testing.T) {
	mockGit := NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "origin/main"
	mockGit.Commits = []Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
	}
	mockGit.OnRun = func(args []string) (string, error) {
		if len(args) >= 2 && args[0] == "rev-list" && args[1] == "--count" {
			return "0", nil // Not behind
		}
		if len(args) >= 2 && args[0] == "log" && args[1] == "--oneline" {
			// Check which commit we're searching for
			for _, arg := range args {
				if strings.HasPrefix(arg, "--grep=") {
					subject := strings.TrimPrefix(arg, "--grep=")
					if subject == "Add user model" {
						// This commit is in main (merged)
						return "ca4a39d Add user model (#77)", nil
					}
				}
			}
			return "", nil // Not merged
		}
		return "", nil
	}

	checker := NewSyncChecker(mockGit)
	status, err := checker.CheckStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !status.NeedsRebase {
		t.Error("expected NeedsRebase=true when has merged commits")
	}
	if !status.HasMergedCommits {
		t.Error("expected HasMergedCommits=true")
	}
	if len(status.MergedCommits) != 1 {
		t.Errorf("expected 1 merged commit, got %d", len(status.MergedCommits))
	}
	if len(status.MergedCommits) > 0 && status.MergedCommits[0].Subject != "Add user model" {
		t.Errorf("expected merged commit 'Add user model', got %q", status.MergedCommits[0].Subject)
	}
}

func TestSyncChecker_EnsureSynced_AutoRebase(t *testing.T) {
	mockGit := NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "origin/main"
	mockGit.Commits = []Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	fetchCalled := false
	rebaseCalled := false

	mockGit.OnRun = func(args []string) (string, error) {
		if len(args) >= 1 && args[0] == "fetch" {
			fetchCalled = true
			return "", nil
		}
		if len(args) >= 1 && args[0] == "rebase" {
			rebaseCalled = true
			// After rebase, simulate that we're now in sync
			mockGit.Commits = []Commit{} // No more commits (merged one dropped)
			return "", nil
		}
		if len(args) >= 2 && args[0] == "rev-list" && args[1] == "--count" {
			// After rebase, no longer behind
			if rebaseCalled {
				return "0", nil
			}
			return "1", nil // Behind by 1 initially
		}
		if len(args) >= 2 && args[0] == "log" && args[1] == "--oneline" {
			return "", nil
		}
		return "", nil
	}

	checker := NewSyncChecker(mockGit)
	status, err := checker.EnsureSynced(true) // auto-rebase enabled
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !fetchCalled {
		t.Error("expected fetch to be called")
	}
	if !rebaseCalled {
		t.Error("expected rebase to be called when behind")
	}
	if status.NeedsRebase {
		t.Error("expected NeedsRebase=false after rebase")
	}
}

func TestSyncChecker_EnsureSynced_NoAutoRebase(t *testing.T) {
	mockGit := NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "origin/main"
	mockGit.Commits = []Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	rebaseCalled := false

	mockGit.OnRun = func(args []string) (string, error) {
		if len(args) >= 1 && args[0] == "fetch" {
			return "", nil
		}
		if len(args) >= 1 && args[0] == "rebase" {
			rebaseCalled = true
			return "", nil
		}
		if len(args) >= 2 && args[0] == "rev-list" && args[1] == "--count" {
			return "1", nil // Behind by 1
		}
		if len(args) >= 2 && args[0] == "log" && args[1] == "--oneline" {
			return "", nil
		}
		return "", nil
	}

	checker := NewSyncChecker(mockGit)
	status, err := checker.EnsureSynced(false) // auto-rebase disabled
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rebaseCalled {
		t.Error("expected rebase NOT to be called when auto-rebase is false")
	}
	if !status.NeedsRebase {
		t.Error("expected NeedsRebase=true when auto-rebase is disabled")
	}
}

func TestSyncStatus_FormatSyncMessage(t *testing.T) {
	status := &SyncStatus{
		NeedsRebase:      true,
		BehindBase:       true,
		BehindCount:      3,
		HasMergedCommits: true,
		MergedCommits: []Commit{
			{Hash: "abc1234", Subject: "Add user model"},
		},
		BaseBranch: "origin/main",
	}

	msg := status.FormatSyncMessage()

	if !strings.Contains(msg, "1 commit(s) already merged") {
		t.Error("expected message to mention merged commits")
	}
	if !strings.Contains(msg, "Add user model") {
		t.Error("expected message to list merged commit subject")
	}
	if !strings.Contains(msg, "3 commit(s) behind") {
		t.Error("expected message to mention being behind")
	}
	if !strings.Contains(msg, "git rebase origin/main") {
		t.Error("expected message to show rebase command")
	}
}

func TestSyncStatus_FormatSyncMessage_InSync(t *testing.T) {
	status := &SyncStatus{
		NeedsRebase: false,
	}

	msg := status.FormatSyncMessage()

	if msg != "" {
		t.Errorf("expected empty message when in sync, got %q", msg)
	}
}
