// cmd/amend.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/github"
	"github.com/calvin/gh-stacked/internal/stack"
	"github.com/spf13/cobra"
)

var amendVerbose bool
var amendPRNumber int

// Dependency injection points for testing
var (
	amendNewGit    = func() git.GitOperations { return git.New() }
	amendNewGitHub = func() (github.GitHubOperations, error) { return github.NewClient() }
)

type AmendOutput struct {
	Amended int      `json:"amended"`
	Commit  string   `json:"commit"`
	Rebased []int    `json:"rebased"`
	Pushed  []string `json:"pushed"`
}

var amendCmd = &cobra.Command{
	Use:   "amend [position]",
	Short: "Amend a specific PR with staged changes",
	Long: `Amend a specific PR in the stack with your staged changes.

You can specify which commit to amend by either:
  - Stack position (1, 2, 3, ...) as shown by 'gh stacked status'
  - PR number using the --pr flag

This command:
  1. Creates a fixup commit targeting the specified commit
  2. Runs an autosquash rebase to fold it in
  3. Force-pushes all affected PR branches

You must stage your changes before running this command.`,
	Example: `  # Fix position 2 after code review
  git add .
  gh stacked amend 2

  # Or use the PR number directly
  git add .
  gh stacked amend --pr 123

  # Check positions first
  gh stacked status`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAmend,
}

func init() {
	rootCmd.AddCommand(amendCmd)
	amendCmd.Flags().BoolVarP(&amendVerbose, "verbose", "v", false, "Show detailed progress output")
	amendCmd.Flags().IntVar(&amendPRNumber, "pr", 0, "PR number to amend (alternative to position)")
}

func runAmend(cmd *cobra.Command, args []string) error {
	// Validate arguments: need exactly one of position arg or --pr flag
	hasPositionArg := len(args) > 0
	hasPRFlag := amendPRNumber > 0

	if hasPositionArg && hasPRFlag {
		return fmt.Errorf("cannot specify both position argument and --pr flag")
	}
	if !hasPositionArg && !hasPRFlag {
		return fmt.Errorf("must specify either a stack position or --pr flag")
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

	if len(commits) == 0 {
		return fmt.Errorf("no commits in stack")
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

	// Determine stack index from position arg or --pr flag
	var stackIndex int
	if hasPositionArg {
		stackIndex, err = strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid stack position: %s", args[0])
		}
		if stackIndex <= 0 {
			return fmt.Errorf("stack position must be positive (1, 2, 3, ...)")
		}
		if stackIndex > len(commits) {
			return fmt.Errorf("stack position %d is out of range (stack has %d commits)", stackIndex, len(commits))
		}
	} else {
		// Look up PR number to find stack position
		if amendVerbose {
			fmt.Printf("[verbose] Looking up PR #%d...\n", amendPRNumber)
		}

		gh, err := amendNewGitHub()
		if err != nil {
			return fmt.Errorf("failed to create GitHub client: %w", err)
		}

		// Build stack entries with PR info
		getPRForBranch := func(branch string) (*stack.PRInfo, error) {
			prs, err := gh.ListAllPullRequests(branch)
			if err != nil {
				return nil, err
			}
			if len(prs) == 0 {
				return nil, nil
			}
			pr := prs[0]
			return &stack.PRInfo{
				Number: pr.Number,
				URL:    pr.URL,
				State:  pr.State,
				Base:   pr.Base.Ref,
			}, nil
		}

		entries := stack.AnalyzeStack(commits, remoteBranches, currentBranch, getPRForBranch)

		// Find the entry with the matching PR number
		found := false
		for _, e := range entries {
			if e.PRNumber == amendPRNumber {
				stackIndex = e.Position
				found = true
				if amendVerbose {
					fmt.Printf("[verbose] PR #%d is at stack position %d\n", amendPRNumber, stackIndex)
				}
				break
			}
		}

		if !found {
			return fmt.Errorf("PR #%d not found in current stack", amendPRNumber)
		}
	}

	// Target index is 0-based
	targetIdx := stackIndex - 1
	targetCommit := commits[targetIdx]

	if amendVerbose {
		fmt.Printf("[verbose] Found %d existing remote branches\n", len(existingBranches))
	}

	// Match commits to their positions using remote branches
	commitPositions := stack.MatchCommitsToPositions(
		stack.GitCommitsToInfos(commits),
		existingBranches,
		currentBranch,
	)

	// Get the branch position for the target commit
	branchPosition := commitPositions[targetIdx].Position

	if amendVerbose {
		fmt.Printf("[verbose] Target commit: %s %s (stack #%d, branch %d)\n", targetCommit.Hash[:7], targetCommit.Subject, stackIndex, branchPosition)
	}

	// Check if there's already a fixup commit at the top for this target
	fixupPrefix := "fixup! " + targetCommit.Subject
	hasExistingFixup := len(commits) > 0 && commits[len(commits)-1].Subject == fixupPrefix

	// Save original HEAD hash BEFORE creating fixup commit (for recovery)
	origHead, err := g.Run("rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("failed to save original state: %w", err)
	}

	// Track the fixup commit hash so we can restore staged changes on failure
	var fixupCommitHash string

	if hasExistingFixup {
		if amendVerbose {
			fmt.Printf("[verbose] Found existing fixup commit at top: %s\n", commits[len(commits)-1].Hash[:7])
		}
		if !jsonOutput {
			fmt.Println("Using existing fixup commit from previous attempt...")
		}
		fixupCommitHash = commits[len(commits)-1].Hash
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

		// Save the fixup commit hash for potential recovery
		fixupCommitHash, err = g.Run("rev-parse", "HEAD")
		if err != nil {
			return fmt.Errorf("failed to get fixup commit hash: %w", err)
		}
	}

	if amendVerbose {
		fmt.Printf("[verbose] Running autosquash rebase onto %s...\n", baseBranch)
		fmt.Println("[verbose] (This may take a while with many commits)")
	}

	// Run autosquash rebase
	err = g.AutosquashRebase(baseBranch)
	if err != nil {
		// Abort the failed rebase
		g.AbortRebase()

		// Extract the diff from the fixup commit before resetting
		fixupDiff, diffErr := g.Run("diff-tree", "-p", fixupCommitHash)

		// Reset to state before fixup commit
		g.ResetHard(origHead)

		// Try to restore the staged changes
		if diffErr == nil && fixupDiff != "" {
			// Apply the diff as staged changes
			applyErr := g.ApplyPatch(fixupDiff, true) // true = apply to index (staged)
			if applyErr == nil {
				return fmt.Errorf(`conflict rebasing "%s" onto %s
Restored your staged changes (not committed).

This usually means your staged changes conflict with commits after position %d.
The file may have been modified, renamed, or deleted in later commits.

To amend manually:
  git stash
  git rebase -i %s
  # move the target commit, edit it, apply your changes
  # then continue the rebase
  git stash pop
  gh stacked push`, targetCommit.Subject, baseBranch, stackIndex, baseBranch)
			}
		}

		// Fallback if we couldn't restore staged changes
		return fmt.Errorf(`conflict rebasing "%s" onto %s
Could not restore staged changes automatically.

This usually means your staged changes conflict with commits after position %d.
The file may have been modified, renamed, or deleted in later commits.

To amend manually:
  git rebase -i %s
  # move the target commit, edit it, apply your changes
  # then continue the rebase
  gh stacked push`, targetCommit.Subject, baseBranch, stackIndex, baseBranch)
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

	// The target commit is at the same index after rebase (order preserved, hashes changed)
	newTargetCommit := newCommits[targetIdx]

	// Push affected branches
	output := AmendOutput{
		Amended: stackIndex,
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

		if cp.Position != branchPosition {
			output.Rebased = append(output.Rebased, i+1) // Use stack index in output
		}
		output.Pushed = append(output.Pushed, cp.Branch)

		if !jsonOutput {
			if cp.Position == branchPosition {
				fmt.Printf("Amended: %s\n", cp.Branch)
			} else {
				fmt.Printf("Rebased: %s\n", cp.Branch)
			}
		}
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	fmt.Printf("\nAmended stack position %d, rebased %d commits\n", stackIndex, len(output.Rebased))

	return nil
}
