// cmd/amend.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/stack"
	"github.com/spf13/cobra"
)

var amendVerbose bool

// Dependency injection points for testing
var amendNewGit = func() git.GitOperations { return git.New() }

type AmendOutput struct {
	Amended int      `json:"amended"`
	Commit  string   `json:"commit"`
	Rebased []int    `json:"rebased"`
	Pushed  []string `json:"pushed"`
}

var amendCmd = &cobra.Command{
	Use:   "amend <pr-number>",
	Short: "Amend a specific PR with staged changes",
	Long: `Amend a specific PR in the stack with your staged changes.

This command:
  1. Creates a fixup commit targeting the specified PR's commit
  2. Runs an autosquash rebase to fold it in
  3. Force-pushes all affected PR branches

You must stage your changes before running this command.`,
	Example: `  # Fix PR 2 after code review
  git add .
  gh stacked amend 2`,
	Args: cobra.ExactArgs(1),
	RunE: runAmend,
}

func init() {
	rootCmd.AddCommand(amendCmd)
	amendCmd.Flags().BoolVarP(&amendVerbose, "verbose", "v", false, "Show detailed progress output")
}

func runAmend(cmd *cobra.Command, args []string) error {
	prPosition, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid PR number: %s", args[0])
	}

	g := amendNewGit()

	if amendVerbose {
		fmt.Println("[verbose] Getting current branch...")
	}

	// Get current branch first (needed to check for existing fixup)
	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if stack.IsStackBranch(currentBranch) {
		return fmt.Errorf("run amend from your feature branch, not a stack branch")
	}

	if amendVerbose {
		fmt.Printf("[verbose] Current branch: %s\n", currentBranch)
		fmt.Println("[verbose] Getting base branch...")
	}

	// Get base branch
	baseBranch, err := g.GetBaseBranch()
	if err != nil {
		return fmt.Errorf("failed to get base branch: %w", err)
	}

	if amendVerbose {
		fmt.Printf("[verbose] Base branch: %s\n", baseBranch)
		fmt.Println("[verbose] Getting commits since base...")
	}

	// Get commits since base
	commits, err := g.GetCommitsSinceBase(baseBranch)
	if err != nil {
		return fmt.Errorf("failed to get commits: %w", err)
	}

	if amendVerbose {
		fmt.Printf("[verbose] Found %d commits in stack\n", len(commits))
		fmt.Println("[verbose] Fetching remote branches...")
	}

	// Fetch existing remote branches for this stack - SOURCE OF TRUTH for positions
	remoteBranches, err := g.ListRemoteBranches("origin/stack/" + currentBranch + "/*")
	if err != nil {
		remoteBranches = nil
	}
	existingBranches := stack.ParseExistingBranches(remoteBranches, currentBranch)

	if amendVerbose {
		fmt.Printf("[verbose] Found %d existing remote branches\n", len(existingBranches))
	}

	// Match commits to their positions using remote branches
	commitPositions := stack.MatchCommitsToPositions(
		stack.GitCommitsToInfos(commits),
		existingBranches,
		currentBranch,
	)

	// Find the commit that matches the requested position
	var targetIdx int = -1
	var targetCommit git.Commit
	for i, cp := range commitPositions {
		if cp.Position == prPosition {
			targetIdx = i
			targetCommit = commits[i]
			break
		}
	}

	if targetIdx == -1 {
		// List available positions
		var positions []int
		for _, cp := range commitPositions {
			positions = append(positions, cp.Position)
		}
		return fmt.Errorf("PR position %d not found in current stack (available: %v)", prPosition, positions)
	}

	if amendVerbose {
		fmt.Printf("[verbose] Target commit: %s %s (position %d)\n", targetCommit.Hash[:7], targetCommit.Subject, prPosition)
	}

	// Check if there's already a fixup commit at the top for this target
	fixupPrefix := "fixup! " + targetCommit.Subject
	hasExistingFixup := len(commits) > 0 && commits[len(commits)-1].Subject == fixupPrefix

	// Save original HEAD hash BEFORE creating fixup commit (for recovery)
	origHead, err := g.Run("rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("failed to save original state: %w", err)
	}

	if hasExistingFixup {
		if amendVerbose {
			fmt.Printf("[verbose] Found existing fixup commit at top: %s\n", commits[len(commits)-1].Hash[:7])
		}
		if !jsonOutput {
			fmt.Println("Using existing fixup commit from previous attempt...")
		}
	} else {
		// No existing fixup - need staged changes to create one
		if amendVerbose {
			fmt.Println("[verbose] Checking for staged changes...")
		}
		hasStaged, err := g.HasStagedChanges()
		if err != nil {
			return fmt.Errorf("failed to check staged changes: %w", err)
		}
		if !hasStaged {
			return fmt.Errorf("no staged changes. Stage files first with `git add`")
		}

		if amendVerbose {
			fmt.Printf("[verbose] Saved original HEAD: %s\n", origHead[:7])
			fmt.Println("[verbose] Creating fixup commit...")
		}

		// Create fixup commit
		err = g.CreateFixupCommit(targetCommit.Hash)
		if err != nil {
			return fmt.Errorf("failed to create fixup commit: %w", err)
		}
	}

	if amendVerbose {
		fmt.Printf("[verbose] Running autosquash rebase onto %s...\n", baseBranch)
		fmt.Println("[verbose] (This may take a while with many commits)")
	}

	// Run autosquash rebase
	err = g.AutosquashRebase(baseBranch)
	if err != nil {
		// Attempt to abort and restore to state BEFORE fixup commit
		g.AbortRebase()
		g.ResetHard(origHead)

		return fmt.Errorf(`conflict rebasing "%s" onto %s
Restored original branch state (fixup commit removed).

This usually means your staged changes conflict with commits after position %d.
The file may have been modified, renamed, or deleted in later commits.

To amend manually:
  git rebase -i %s
  # move the target commit, edit it, apply your changes
  # then continue the rebase
  gh stacked push`, targetCommit.Subject, baseBranch, prPosition, baseBranch)
	}

	if amendVerbose {
		fmt.Println("[verbose] Rebase complete. Getting new commits...")
	}

	// Get new commits after rebase
	newCommits, err := g.GetCommitsSinceBase(baseBranch)
	if err != nil {
		return fmt.Errorf("failed to get commits after rebase: %w", err)
	}

	// Re-match commits to positions after rebase (hashes changed but subjects same)
	newCommitPositions := stack.MatchCommitsToPositions(
		stack.GitCommitsToInfos(newCommits),
		existingBranches,
		currentBranch,
	)

	// Check if commits before targetIdx also changed (happens when base branch moved)
	// If so, we need to push ALL branches, not just from targetIdx onwards
	startIdx := targetIdx
	for i := 0; i < targetIdx && i < len(newCommits) && i < len(commits); i++ {
		if newCommits[i].Hash != commits[i].Hash {
			// Base moved, all commits changed - need to push from beginning
			startIdx = 0
			if amendVerbose {
				fmt.Println("[verbose] Base branch moved - all commits have new hashes")
			}
			break
		}
	}

	affectedCount := len(newCommits) - startIdx
	if amendVerbose {
		fmt.Printf("[verbose] Found %d commits after rebase\n", len(newCommits))
		fmt.Printf("[verbose] Pushing %d affected branches...\n", affectedCount)
	}

	// Find the new target commit hash
	var newTargetCommit git.Commit
	for i, cp := range newCommitPositions {
		if cp.Position == prPosition {
			newTargetCommit = newCommits[i]
			break
		}
	}

	// Push affected branches
	output := AmendOutput{
		Amended: prPosition,
		Commit:  newTargetCommit.Hash[:7],
		Rebased: []int{},
		Pushed:  []string{},
	}

	// Push all commits from startIdx onwards (may be 0 if base moved)
	for i := startIdx; i < len(newCommits); i++ {
		commit := newCommits[i]
		cp := newCommitPositions[i]

		if amendVerbose {
			fmt.Printf("[verbose] Creating branch %s at %s...\n", cp.Branch, commit.Hash[:7])
		}

		err := g.CreateBranchAtCommit(cp.Branch, commit.Hash)
		if err != nil {
			return fmt.Errorf("failed to update branch %s: %w", cp.Branch, err)
		}

		if amendVerbose {
			fmt.Printf("[verbose] Pushing %s...\n", cp.Branch)
		}

		err = g.PushBranch("origin", cp.Branch, true)
		if err != nil {
			return fmt.Errorf("failed to push %s: %w", cp.Branch, err)
		}

		if cp.Position != prPosition {
			output.Rebased = append(output.Rebased, cp.Position)
		}
		output.Pushed = append(output.Pushed, cp.Branch)

		if !jsonOutput {
			if cp.Position == prPosition {
				fmt.Printf("Amended: %s\n", cp.Branch)
			} else {
				fmt.Printf("Rebased: %s\n", cp.Branch)
			}
		}
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	fmt.Printf("\nAmended PR %d, rebased %d commits\n", prPosition, len(output.Rebased))

	return nil
}
