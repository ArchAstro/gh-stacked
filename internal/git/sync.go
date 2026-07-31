// internal/git/sync.go
package git

import (
	"fmt"
	"strings"
)

// SyncStatus represents the sync state of a branch relative to its base
type SyncStatus struct {
	// NeedsFetch is true if we haven't fetched recently (always fetch for safety)
	NeedsFetch bool

	// BehindBase is true if base branch has commits not in local branch
	BehindBase  bool
	BehindCount int

	// HasMergedCommits is true if local commits are already in base (via squash merge)
	HasMergedCommits bool
	MergedCommits    []Commit

	// NeedsRebase is true if the branch should be rebased onto base
	NeedsRebase bool

	// BaseBranch is the target base branch (e.g., "origin/main")
	BaseBranch string

	// CurrentBranch is the current working branch
	CurrentBranch string
}

// SyncChecker provides methods to check and fix branch sync status
type SyncChecker struct {
	git GitOperations
}

// NewSyncChecker creates a new SyncChecker
func NewSyncChecker(g GitOperations) *SyncChecker {
	return &SyncChecker{git: g}
}

// CheckStatus checks the sync status of the current branch
func (s *SyncChecker) CheckStatus() (*SyncStatus, error) {
	status := &SyncStatus{
		NeedsFetch: true, // Always recommend fetching
	}

	// Get current branch
	currentBranch, err := s.git.GetCurrentBranch()
	if err != nil {
		return nil, fmt.Errorf("failed to get current branch: %w", err)
	}
	status.CurrentBranch = currentBranch

	// Get base branch
	baseBranch, err := s.git.GetBaseBranch()
	if err != nil {
		return nil, fmt.Errorf("failed to get base branch: %w", err)
	}
	status.BaseBranch = baseBranch

	// Get local commits since base
	commits, err := s.git.GetCommitsSinceBase(baseBranch)
	if err != nil {
		return nil, fmt.Errorf("failed to get commits: %w", err)
	}

	// Check how many commits we're behind base
	behindCount, err := s.countBehind(baseBranch)
	if err != nil {
		// Non-fatal - might not have fetched yet
		behindCount = 0
	}
	status.BehindBase = behindCount > 0
	status.BehindCount = behindCount

	// Check if any local commits are already in base (merged via squash)
	mergedCommits, err := s.findMergedCommits(commits, baseBranch)
	if err != nil {
		// Non-fatal
		mergedCommits = nil
	}
	status.HasMergedCommits = len(mergedCommits) > 0
	status.MergedCommits = mergedCommits

	// Need rebase if behind base OR have merged commits
	status.NeedsRebase = status.BehindBase || status.HasMergedCommits

	return status, nil
}

// Fetch fetches from origin
func (s *SyncChecker) Fetch() error {
	_, err := s.git.Run("fetch", "origin")
	return err
}

// Rebase rebases current branch onto base
func (s *SyncChecker) Rebase(baseBranch string) error {
	_, err := s.git.Run("rebase", baseBranch)
	return err
}

// countBehind counts how many commits the current branch is behind base
func (s *SyncChecker) countBehind(baseBranch string) (int, error) {
	// Count commits in base that aren't in HEAD
	out, err := s.git.Run("rev-list", "--count", "HEAD.."+baseBranch)
	if err != nil {
		return 0, err
	}
	var count int
	fmt.Sscanf(out, "%d", &count)
	return count, nil
}

// findMergedCommits finds local commits that are already in base (via squash merge)
// This works by checking if a commit's patch-id matches any commit in base
func (s *SyncChecker) findMergedCommits(commits []Commit, baseBranch string) ([]Commit, error) {
	var merged []Commit

	for _, commit := range commits {
		// Check if this commit's changes are already in base
		// We do this by checking if the commit message appears in base's log
		// (GitHub squash merges preserve the commit message)
		isMerged, err := s.isCommitMerged(commit, baseBranch)
		if err != nil {
			continue // Skip on error
		}
		if isMerged {
			merged = append(merged, commit)
		}
	}

	return merged, nil
}

// isCommitMerged checks if a commit's changes are already in the base branch
// by looking for commits with matching subjects (how GitHub squash merge works)
func (s *SyncChecker) isCommitMerged(commit Commit, baseBranch string) (bool, error) {
	// Search for commits in base with matching subject
	// GitHub squash merges typically preserve the subject with " (#PR)" appended
	subject := commit.Subject

	// Search in base branch for commits with this subject
	out, err := s.git.Run("log", "--oneline", "--grep="+subject, baseBranch, "-1")
	if err != nil {
		return false, err
	}

	// If we found a match in base, the commit is merged
	return strings.TrimSpace(out) != "", nil
}

// EnsureSynced fetches and rebases if necessary, returning the final status
func (s *SyncChecker) EnsureSynced(autoRebase bool) (*SyncStatus, error) {
	// Always fetch first
	if err := s.Fetch(); err != nil {
		return nil, fmt.Errorf("failed to fetch: %w", err)
	}

	// Check status after fetch
	status, err := s.CheckStatus()
	if err != nil {
		return nil, err
	}

	// If we need to rebase and auto-rebase is enabled
	if status.NeedsRebase && autoRebase {
		if err := s.Rebase(status.BaseBranch); err != nil {
			return status, fmt.Errorf("rebase failed: %w", err)
		}

		// Re-check status after rebase
		status, err = s.CheckStatus()
		if err != nil {
			return nil, err
		}
	}

	return status, nil
}

// FormatSyncMessage returns a user-friendly message about the sync status
func (s *SyncStatus) FormatSyncMessage() string {
	if !s.NeedsRebase {
		return ""
	}

	var msg strings.Builder

	if s.HasMergedCommits {
		msg.WriteString(fmt.Sprintf("Found %d commit(s) already merged into %s:\n",
			len(s.MergedCommits), s.BaseBranch))
		for _, c := range s.MergedCommits {
			msg.WriteString(fmt.Sprintf("  - %s\n", c.Subject))
		}
	}

	if s.BehindBase {
		msg.WriteString(fmt.Sprintf("Branch is %d commit(s) behind %s\n",
			s.BehindCount, s.BaseBranch))
	}

	msg.WriteString(fmt.Sprintf("\nTo sync: git fetch origin && git rebase %s\n", s.BaseBranch))

	return msg.String()
}
