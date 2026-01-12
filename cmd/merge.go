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
	Merged       []int  `json:"merged"`
	MergeCommit  string `json:"merge_commit,omitempty"`
	Retargeted   []int  `json:"retargeted"`
	Rebased      bool   `json:"rebased"`
	RemainingPRs []int  `json:"remaining_prs"`
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

	// Find the minimum position (first in stack that can be merged)
	minPosition := stack.FindMinPosition(existingBranches)

	// Validate: positions must start from minPosition and be contiguous
	for i, pos := range positions {
		expectedPos := minPosition + i
		if pos != expectedPos {
			if i == 0 {
				// First position doesn't match minimum
				var minCommitMsg string
				for _, eb := range existingBranches {
					if eb.Position == minPosition {
						minCommitMsg = eb.Slug
						break
					}
				}
				return fmt.Errorf(`cannot merge position %d - position %d must be merged first

Stack order:
  #%d [OPEN] %s <- merge this first
  #%d [OPEN] ... <- you tried to merge this

Use 'gh stacked status' to see the current stack.`,
					pos, minPosition, minPosition, minCommitMsg, pos)
			}
			// Gap in range
			return fmt.Errorf("positions must be contiguous: expected %d but got %d", expectedPos, pos)
		}
		// Verify position exists
		found := false
		for _, eb := range existingBranches {
			if eb.Position == pos {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("position %d not found in stack", pos)
		}
	}

	// Get GitHub client
	gh, err := mergeNewGitHub()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Build map of position -> branch info for quick lookup
	branchByPosition := make(map[int]*stack.ExistingBranchInfo)
	for i := range existingBranches {
		branchByPosition[existingBranches[i].Position] = &existingBranches[i]
	}

	// Collect PRs to merge (validate they all exist and are open)
	type prToMerge struct {
		Position int
		Branch   *stack.ExistingBranchInfo
		PR       github.PullRequest
	}
	var prsToMerge []prToMerge

	for _, pos := range positions {
		branch := branchByPosition[pos]
		if branch == nil {
			return fmt.Errorf("no branch found for position %d", pos)
		}

		prs, err := gh.ListPullRequests(branch.Branch)
		if err != nil {
			return fmt.Errorf("failed to find PR for position %d: %w", pos, err)
		}
		if len(prs) == 0 {
			// Check if there's a merged/closed PR for this branch
			allPRs, err := gh.ListAllPullRequests(branch.Branch)
			if err == nil && len(allPRs) > 0 {
				pr := allPRs[0]
				if pr.State == "closed" {
					return fmt.Errorf(`PR #%d for position %d is already closed/merged

The stack is out of sync. To fix:
  git fetch origin
  git rebase origin/%s
  gh stacked push

This will sync your local branch with the merged changes.`, pr.Number, pos, strings.TrimPrefix(baseBranch, "origin/"))
				}
			}
			return fmt.Errorf("no open PR found for branch %s - run 'gh stacked push' first", branch.Branch)
		}

		prsToMerge = append(prsToMerge, prToMerge{
			Position: pos,
			Branch:   branch,
			PR:       prs[0],
		})
	}

	// Find positions that will remain after merge
	maxMergePosition := positions[len(positions)-1]
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

	// Now safe to merge - merge each PR in order
	output := MergeOutput{
		Merged:       []int{},
		Retargeted:   []int{},
		Rebased:      false,
		RemainingPRs: []int{},
	}

	for _, pm := range prsToMerge {
		err = gh.MergePullRequest(pm.PR.Number)
		if err != nil {
			return fmt.Errorf("failed to merge PR #%d (position %d): %w", pm.PR.Number, pm.Position, err)
		}

		output.Merged = append(output.Merged, pm.PR.Number)

		if !jsonOutput {
			fmt.Printf("Merged PR #%d: %s\n", pm.PR.Number, pm.Branch.Slug)
		}
	}

	// Find the next position after all merged ones to retarget to base
	var nextPosition int
	for _, eb := range existingBranches {
		if eb.Position > maxMergePosition {
			if nextPosition == 0 || eb.Position < nextPosition {
				nextPosition = eb.Position
			}
		}
	}

	// If there are remaining commits, update them
	if remainingCommitCount > 0 && !mergeNoRebase {
		// Retarget next PR to base branch
		if nextPosition > 0 {
			nextBranch := branchByPosition[nextPosition]
			if nextBranch != nil {
				nextPRs, err := gh.ListPullRequests(nextBranch.Branch)
				if err == nil && len(nextPRs) > 0 {
					targetBase := strings.TrimPrefix(baseBranch, "origin/")
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

		// Build remaining branches list (exclude merged positions)
		mergedPositions := make(map[int]bool)
		for _, pos := range positions {
			mergedPositions[pos] = true
		}
		var remainingBranches []stack.ExistingBranchInfo
		for _, eb := range existingBranches {
			if !mergedPositions[eb.Position] {
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
