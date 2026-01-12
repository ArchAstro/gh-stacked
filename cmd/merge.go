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
	Merged       int    `json:"merged"`
	MergeCommit  string `json:"merge_commit,omitempty"`
	Retargeted   []int  `json:"retargeted"`
	Rebased      bool   `json:"rebased"`
	RemainingPRs []int  `json:"remaining_prs"`
}

var mergeCmd = &cobra.Command{
	Use:   "merge <position>",
	Short: "Merge a PR and update the remaining stack",
	Long: `Merge a PR by its stack position and update the remaining stack.

Use 'gh stacked status' to see the current stack positions.

This command:
  1. Merges the specified PR via GitHub API
  2. Retargets the next PR to the base branch
  3. Rebases remaining local commits onto the base branch
  4. Force-pushes all remaining PR branches

PRs must be merged in order - you can only merge the first position
in the current stack. After merging, the next position becomes first.`,
	Example: `  # Merge the first position in the stack
  gh stacked merge 2  # if positions are 2, 3, 4 after PR 1 was merged

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
	prPosition, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid PR number: %s", args[0])
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

	// Must merge in order - only the first position can be merged
	if prPosition != minPosition {
		// Find commit message for min position
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
			prPosition, minPosition, minPosition, minCommitMsg, prPosition)
	}

	// Get GitHub client
	gh, err := mergeNewGitHub()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Find the branch for this position
	var branchToMerge *stack.ExistingBranchInfo
	for i := range existingBranches {
		if existingBranches[i].Position == prPosition {
			branchToMerge = &existingBranches[i]
			break
		}
	}
	if branchToMerge == nil {
		return fmt.Errorf("no branch found for position %d", prPosition)
	}

	// Find the PR for this branch
	prs, err := gh.ListPullRequests(branchToMerge.Branch)
	if err != nil {
		return fmt.Errorf("failed to find PR: %w", err)
	}
	if len(prs) == 0 {
		// Check if there's a merged/closed PR for this branch
		allPRs, err := gh.ListAllPullRequests(branchToMerge.Branch)
		if err == nil && len(allPRs) > 0 {
			pr := allPRs[0]
			if pr.State == "closed" {
				return fmt.Errorf(`PR #%d for position %d is already closed/merged

The stack is out of sync. To fix:
  git fetch origin
  git rebase origin/%s
  gh stacked push

This will sync your local branch with the merged changes.`, pr.Number, prPosition, strings.TrimPrefix(baseBranch, "origin/"))
			}
		}
		return fmt.Errorf("no open PR found for branch %s - run 'gh stacked push' first", branchToMerge.Branch)
	}

	prToMerge := prs[0]

	// Find the commit message for logging
	commitMsg := branchToMerge.Slug

	// Pre-flight check: If we'll need to rebase, verify it won't conflict
	// BEFORE making any irreversible changes (merge, retarget)
	if len(commits) > 1 && !mergeNoRebase {
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
		// Use origin/baseBranch since that's what we'll rebase onto after merge
		rebaseTarget := "origin/" + strings.TrimPrefix(baseBranch, "origin/")
		_, testErr := g.Run("rebase", rebaseTarget)
		if testErr != nil {
			// Rebase failed - abort and tell user to fix first
			g.AbortRebase()
			// Reset to original state just in case
			g.Run("reset", "--hard", originalHead)
			return fmt.Errorf(`rebase onto %s will conflict - cannot merge safely

Resolve the conflicts first:
  git fetch origin
  git rebase %s
  # resolve conflicts
  gh stacked push

Then retry: gh stacked merge %d`, rebaseTarget, rebaseTarget, prPosition)
		}

		// Test passed - abort it so we can do the real rebase after merge
		g.AbortRebase()
		g.Run("reset", "--hard", originalHead)

		if !jsonOutput {
			fmt.Println("Pre-flight check passed.")
		}
	}

	// Now safe to merge
	err = gh.MergePullRequest(prToMerge.Number)
	if err != nil {
		return fmt.Errorf("failed to merge PR #%d: %w", prToMerge.Number, err)
	}

	output := MergeOutput{
		Merged:       prToMerge.Number,
		Retargeted:   []int{},
		Rebased:      false,
		RemainingPRs: []int{},
	}

	if !jsonOutput {
		fmt.Printf("Merged PR #%d: %s\n", prToMerge.Number, commitMsg)
	}

	// Find the next position (second lowest) to retarget to base
	var nextPosition int
	for _, eb := range existingBranches {
		if eb.Position > minPosition {
			if nextPosition == 0 || eb.Position < nextPosition {
				nextPosition = eb.Position
			}
		}
	}

	// If there are more commits, update them
	if len(commits) > 1 && !mergeNoRebase {
		// Retarget next PR to base branch
		if nextPosition > 0 {
			var nextBranch *stack.ExistingBranchInfo
			for i := range existingBranches {
				if existingBranches[i].Position == nextPosition {
					nextBranch = &existingBranches[i]
					break
				}
			}
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

		// Wait a moment for GitHub to process the merge, then fetch
		_, err = g.Run("fetch", "origin")
		if err != nil {
			return fmt.Errorf("failed to fetch origin: %w", err)
		}

		// Rebase onto the updated base (now includes our merged PR)
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
		// We keep existing positions - no renumbering!
		newCommits, err := g.GetCommitsSinceBase(baseBranch)
		if err != nil {
			return fmt.Errorf("failed to get commits after rebase: %w", err)
		}

		// Build remaining branches list (exclude the merged position)
		var remainingBranches []stack.ExistingBranchInfo
		for _, eb := range existingBranches {
			if eb.Position != minPosition {
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

	fmt.Printf("\nMerge complete. %d PRs remaining.\n", len(output.RemainingPRs))

	return nil
}
