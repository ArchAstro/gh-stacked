// cmd/merge.go - replace entire file
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/github"
	"github.com/calvin/gh-stacked/internal/stack"
	"github.com/spf13/cobra"
)

var mergeNoRebase bool

// Dependency injection points for testing
var (
	mergeNewGit    = func() git.GitOperations { return git.New() }
	mergeNewGitHub = func() (github.GitHubOperations, error) { return github.NewClient() }
)

type MergeOutput struct {
	Merged          []int    `json:"merged"`
	MergeCommit     string   `json:"merge_commit,omitempty"`
	Retargeted      []int    `json:"retargeted"`
	Rebased         bool     `json:"rebased"`
	RemainingPRs    []int    `json:"remaining_prs"`
	DeletedBranches []string `json:"deleted_branches"`
}

// parsePositionRange parses "4" or "4-6" into a slice of positions
func parsePositionRange(arg string) ([]int, error) {
	if strings.Contains(arg, "-") {
		parts := strings.Split(arg, "-")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid range format: %s (use N or N-M)", arg)
		}
		start, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid start position: %s", parts[0])
		}
		end, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid end position: %s", parts[1])
		}
		if start > end {
			return nil, fmt.Errorf("invalid range: start %d > end %d", start, end)
		}
		if start <= 0 {
			return nil, fmt.Errorf("positions must be positive")
		}
		positions := make([]int, 0, end-start+1)
		for i := start; i <= end; i++ {
			positions = append(positions, i)
		}
		return positions, nil
	}

	pos, err := strconv.Atoi(arg)
	if err != nil {
		return nil, fmt.Errorf("invalid position: %s", arg)
	}
	if pos <= 0 {
		return nil, fmt.Errorf("position must be positive")
	}
	return []int{pos}, nil
}

var mergeCmd = &cobra.Command{
	Use:   "merge <position> or <start-end>",
	Short: "Merge PRs and update the remaining stack",
	Long: `Merge one or more PRs by stack position and update the remaining stack.

Use 'gh stacked status' to see the current stack positions.

This command:
  1. Merges the specified PR(s) via GitHub API
  2. Retargets the next PR to the base branch
  3. Rebases remaining local commits onto the base branch
  4. Force-pushes all remaining PR branches

PRs must be merged in order - you can only merge starting from the first
position in the current stack.`,
	Example: `  # Merge the first position in the stack
  gh stacked merge 2

  # Merge positions 2, 3, and 4 at once
  gh stacked merge 2-4

  # Merge without rebasing the rest (manual sync later)
  gh stacked merge 2 --no-rebase

  # Check current positions first
  gh stacked status`,
	Args: cobra.ExactArgs(1),
	RunE: runMerge,
}

func init() {
	rootCmd.AddCommand(mergeCmd)

	mergeCmd.Flags().BoolVar(&mergeNoRebase, "no-rebase", false, "Just merge, don't rebase remaining stack")
}

func runMerge(cmd *cobra.Command, args []string) error {
	positions, err := parsePositionRange(args[0])
	if err != nil {
		return err
	}

	g := mergeNewGit()

	// Get current branch
	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if stack.IsStackBranch(currentBranch) {
		return fmt.Errorf("run merge from your feature branch, not a stack branch")
	}

	// Check sync status with remote - fetch and detect merged commits
	syncChecker := git.NewSyncChecker(g)
	if !jsonOutput {
		fmt.Println("Checking sync status...")
	}
	syncStatus, err := syncChecker.EnsureSynced(false) // Don't auto-rebase, just check
	if err != nil {
		// Non-fatal - continue if fetch fails (we'll fetch later anyway)
		if !jsonOutput {
			fmt.Printf("Warning: could not check sync status: %v\n", err)
		}
	} else if syncStatus.NeedsRebase {
		return fmt.Errorf("branch is out of sync with %s\n\n%s\nAfter syncing, run 'gh stacked merge %s' again",
			syncStatus.BaseBranch, syncStatus.FormatSyncMessage(), args[0])
	}

	// Get base branch
	baseBranch, err := g.GetBaseBranch()
	if err != nil {
		return fmt.Errorf("failed to get base branch: %w", err)
	}

	// Get commits since base
	commits, err := g.GetCommitsSinceBase(baseBranch)
	if err != nil {
		return fmt.Errorf("failed to get commits: %w", err)
	}

	if len(commits) == 0 {
		return fmt.Errorf("no commits in stack")
	}

	// Fetch existing remote branches for this stack - SOURCE OF TRUTH for positions
	remoteBranches, err := g.ListRemoteBranches("origin/stack/" + currentBranch + "/*")
	if err != nil {
		remoteBranches = nil
	}
	allExistingBranches := stack.ParseExistingBranches(remoteBranches, currentBranch)

	// Filter to only branches that match current local commits
	// (excludes orphaned branches from merged/rebased commits)
	claimedBranches := make(map[int]bool)
	var existingBranches []stack.ExistingBranchInfo
	for _, commit := range commits {
		existing := stack.MatchCommitToExistingBranchExcluding(commit.Subject, allExistingBranches, claimedBranches)
		if existing != nil {
			claimedBranches[existing.Position] = true
			existingBranches = append(existingBranches, *existing)
		}
	}

	if len(existingBranches) == 0 {
		return fmt.Errorf("no stack branches found - run 'gh stacked push' first")
	}

	// positions are now interpreted as 1-based stack indices (order in stack, not branch position numbers)
	// Validate: indices must start from 1 and be contiguous
	for i, idx := range positions {
		expectedIdx := 1 + i
		if idx != expectedIdx {
			if i == 0 {
				// First index doesn't start at 1
				return fmt.Errorf(`cannot merge stack position %d - must start from position 1

Stack order:
  #1 [OPEN] %s <- merge this first
  #%d [OPEN] ... <- you tried to merge this

Use 'gh stacked status' to see the current stack.`,
					idx, existingBranches[0].Slug, idx)
			}
			// Gap in range
			return fmt.Errorf("positions must be contiguous: expected %d but got %d", expectedIdx, idx)
		}
		// Verify index is within range
		if idx > len(existingBranches) {
			return fmt.Errorf("position %d not found in stack (stack has %d PRs)", idx, len(existingBranches))
		}
	}

	// Get GitHub client
	gh, err := mergeNewGitHub()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Collect PRs to merge (validate they all exist and are open)
	// positions are 1-based stack indices, so existingBranches[idx-1] gives the branch
	type prToMerge struct {
		StackIndex int // 1-based index in stack
		Branch     *stack.ExistingBranchInfo
		PR         github.PullRequest
	}
	var prsToMerge []prToMerge

	for _, idx := range positions {
		branch := &existingBranches[idx-1] // Convert 1-based index to 0-based

		prs, err := gh.ListPullRequests(branch.Branch)
		if err != nil {
			return fmt.Errorf("failed to find PR for stack position %d: %w", idx, err)
		}
		if len(prs) == 0 {
			// Check if there's a merged/closed PR for this branch
			allPRs, err := gh.ListAllPullRequests(branch.Branch)
			if err == nil && len(allPRs) > 0 {
				pr := allPRs[0]
				if pr.State == "closed" {
					return fmt.Errorf(`PR #%d for stack position %d is already closed/merged

The stack is out of sync. To fix:
  git fetch origin
  git rebase origin/%s
  gh stacked push

This will sync your local branch with the merged changes.`, pr.Number, idx, strings.TrimPrefix(baseBranch, "origin/"))
				}
			}
			return fmt.Errorf("no open PR found for branch %s - run 'gh stacked push' first", branch.Branch)
		}

		prsToMerge = append(prsToMerge, prToMerge{
			StackIndex: idx,
			Branch:     branch,
			PR:         prs[0],
		})
	}

	// Find how many will remain after merge
	// positions contains 1-based stack indices, so maxMergeIndex is the highest being merged
	maxMergeIndex := positions[len(positions)-1]
	remainingCommitCount := len(commits) - len(positions)

	// Pre-flight check: If we'll need to rebase, verify it won't conflict
	// BEFORE making any irreversible changes (merge, retarget)
	if remainingCommitCount > 0 && !mergeNoRebase {
		if !jsonOutput {
			fmt.Println("Checking if rebase will succeed...")
		}

		// Fetch latest base
		_, err = g.Run("fetch", "origin")
		if err != nil {
			return fmt.Errorf("failed to fetch origin: %w", err)
		}

		// Save current HEAD
		originalHead, err := g.Run("rev-parse", "HEAD")
		if err != nil {
			return fmt.Errorf("failed to get current HEAD: %w", err)
		}

		// Try a test rebase (will be aborted either way)
		rebaseTarget := "origin/" + strings.TrimPrefix(baseBranch, "origin/")
		_, testErr := g.Run("rebase", rebaseTarget)
		if testErr != nil {
			g.AbortRebase()
			g.Run("reset", "--hard", originalHead)
			return fmt.Errorf(`rebase onto %s will conflict - cannot merge safely

Resolve the conflicts first:
  git fetch origin
  git rebase %s
  # resolve conflicts
  gh stacked push

Then retry: gh stacked merge %s`, rebaseTarget, rebaseTarget, args[0])
		}

		g.AbortRebase()
		g.Run("reset", "--hard", originalHead)

		if !jsonOutput {
			fmt.Println("Pre-flight check passed.")
		}
	}

	// Now safe to merge
	output := MergeOutput{
		Merged:          []int{},
		Retargeted:      []int{},
		Rebased:         false,
		RemainingPRs:    []int{},
		DeletedBranches: []string{},
	}

	// IMPORTANT: When merging multiple PRs (1-N), we must retarget PRs 2-N to main FIRST
	// Otherwise PR #2 gets merged into branch #1 (not main), PR #3 into branch #2, etc.
	// and the changes get lost when those branches are deleted.
	targetBase := strings.TrimPrefix(baseBranch, "origin/")
	if len(prsToMerge) > 1 {
		if !jsonOutput {
			fmt.Println("Retargeting PRs to base branch before merge...")
		}
		for i := 1; i < len(prsToMerge); i++ {
			pm := prsToMerge[i]
			err = gh.UpdatePullRequestBase(pm.PR.Number, targetBase)
			if err != nil {
				return fmt.Errorf("failed to retarget PR #%d to %s: %w", pm.PR.Number, targetBase, err)
			}
			output.Retargeted = append(output.Retargeted, pm.PR.Number)
			if !jsonOutput {
				fmt.Printf("  Retargeted PR #%d to %s\n", pm.PR.Number, targetBase)
			}
		}
	}

	// Merge each PR in order (all now target main)
	for _, pm := range prsToMerge {
		err = gh.MergePullRequest(pm.PR.Number)
		if err != nil {
			return fmt.Errorf("failed to merge PR #%d (stack position %d): %w", pm.PR.Number, pm.StackIndex, err)
		}

		output.Merged = append(output.Merged, pm.PR.Number)

		if !jsonOutput {
			fmt.Printf("Merged PR #%d: %s\n", pm.PR.Number, pm.Branch.Slug)
		}
	}

	// IMPORTANT: Retarget remaining PRs BEFORE deleting branches
	// Otherwise GitHub may close PRs whose base branch gets deleted
	if remainingCommitCount > 0 && !mergeNoRebase {
		// Retarget next PR to base branch (it was targeting the last merged PR's branch)
		if maxMergeIndex < len(existingBranches) {
			nextBranch := &existingBranches[maxMergeIndex]
			nextPRs, err := gh.ListPullRequests(nextBranch.Branch)
			if err != nil {
				if !jsonOutput {
					fmt.Printf("Warning: could not find PR for %s: %v\n", nextBranch.Branch, err)
				}
			} else if len(nextPRs) == 0 {
				if !jsonOutput {
					fmt.Printf("Warning: no open PR found for %s\n", nextBranch.Branch)
				}
			} else {
				err = gh.UpdatePullRequestBase(nextPRs[0].Number, targetBase)
				if err != nil {
					return fmt.Errorf("failed to retarget PR #%d: %w", nextPRs[0].Number, err)
				}
				output.Retargeted = append(output.Retargeted, nextPRs[0].Number)

				if !jsonOutput {
					fmt.Printf("Retargeted PR #%d to %s\n", nextPRs[0].Number, targetBase)
				}
			}
		}
	}

	// Clean up merged branches (remote and local) - AFTER retargeting
	if !jsonOutput && len(prsToMerge) > 0 {
		fmt.Println("Cleaning up merged branches...")
	}
	for _, pm := range prsToMerge {
		branchName := pm.Branch.Branch

		// Delete remote branch
		err := g.PushBranch("origin", ":"+branchName, false)
		if err != nil {
			if !jsonOutput {
				fmt.Printf("  Warning: failed to delete remote %s: %v\n", branchName, err)
			}
		} else {
			output.DeletedBranches = append(output.DeletedBranches, branchName)
			if !jsonOutput {
				fmt.Printf("  Deleted remote: %s\n", branchName)
			}
		}

		// Delete local branch if it exists
		_, err = g.Run("branch", "-D", branchName)
		if err == nil {
			if !jsonOutput {
				fmt.Printf("  Deleted local: %s\n", branchName)
			}
		}
		// Ignore error - local branch may not exist
	}

	// Update remaining branches
	if remainingCommitCount > 0 && !mergeNoRebase {

		// Wait a moment for GitHub to process the merges, then fetch
		_, err = g.Run("fetch", "origin")
		if err != nil {
			return fmt.Errorf("failed to fetch origin: %w", err)
		}

		// Rebase onto the updated base (now includes our merged PRs)
		rebaseTarget := "origin/" + strings.TrimPrefix(baseBranch, "origin/")
		_, err = g.Run("rebase", rebaseTarget)
		if err != nil {
			g.AbortRebase()
			return fmt.Errorf(`unexpected conflict rebasing onto %s
This should not happen after pre-flight check passed.

To resolve manually:
  git fetch origin
  git rebase %s
  gh stacked push`, rebaseTarget, rebaseTarget)
		}

		output.Rebased = true

		// Get new commits and match them to existing branches by slug
		newCommits, err := g.GetCommitsSinceBase(baseBranch)
		if err != nil {
			return fmt.Errorf("failed to get commits after rebase: %w", err)
		}

		// Build remaining branches list (exclude merged ones)
		// positions are 1-based stack indices, so merged branches are at existingBranches[0..len(positions)-1]
		mergedBranches := make(map[string]bool)
		for _, pm := range prsToMerge {
			mergedBranches[pm.Branch.Branch] = true
		}
		var remainingBranches []stack.ExistingBranchInfo
		for _, eb := range existingBranches {
			if !mergedBranches[eb.Branch] {
				remainingBranches = append(remainingBranches, eb)
			}
		}

		// Match commits to their existing branches and push
		claimedBranches := make(map[int]bool)
		for _, commit := range newCommits {
			existing := stack.MatchCommitToExistingBranchExcluding(commit.Subject, remainingBranches, claimedBranches)
			if existing != nil {
				claimedBranches[existing.Position] = true

				err := g.CreateBranchAtCommit(existing.Branch, commit.Hash)
				if err != nil {
					return fmt.Errorf("failed to update branch %s: %w", existing.Branch, err)
				}

				err = g.PushBranch("origin", existing.Branch, true)
				if err != nil {
					return fmt.Errorf("failed to push %s: %w", existing.Branch, err)
				}

				// Find PR for this branch
				branchPRs, _ := gh.ListPullRequests(existing.Branch)
				if len(branchPRs) > 0 {
					output.RemainingPRs = append(output.RemainingPRs, branchPRs[0].Number)
				}

				if !jsonOutput {
					fmt.Printf("Updated: %s\n", existing.Branch)
				}
			}
		}
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	fmt.Printf("\nMerge complete. Merged %d PRs, %d remaining.\n", len(output.Merged), len(output.RemainingPRs))

	return nil
}
