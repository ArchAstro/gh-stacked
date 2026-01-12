// cmd/merge.go - replace entire file
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
	Use:   "merge <pr-number>",
	Short: "Merge a PR and update the remaining stack",
	Long: `Merge a PR and update the remaining stack.

This command:
  1. Merges the specified PR via GitHub API
  2. Retargets the next PR to main
  3. Rebases remaining local commits onto main
  4. Force-pushes all remaining PR branches
  5. Updates PR body footers

PRs must be merged in order - you cannot merge PR 2 before PR 1.`,
	Example: `  # Merge the first PR when ready
  gh stacked merge 1

  # Merge without rebasing the rest (manual sync later)
  gh stacked merge 1 --no-rebase`,
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

	if prPosition < 1 || prPosition > len(commits) {
		return fmt.Errorf("PR %d not found in current stack (have %d commits)", prPosition, len(commits))
	}

	// Must merge in order
	if prPosition != 1 {
		return fmt.Errorf(`cannot merge PR #%d. PR #1 must be merged first.

Stack order:
  #1 [OPEN] %s <- merge this first
  #%d [OPEN] %s <- you tried to merge this`,
			prPosition, commits[0].Subject, prPosition, commits[prPosition-1].Subject)
	}

	// Get GitHub client
	gh, err := mergeNewGitHub()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Find the PR for position 1
	branchName := stack.StackBranchName(currentBranch, 1, commits[0].Subject)
	prs, err := gh.ListPullRequests(branchName)
	if err != nil {
		return fmt.Errorf("failed to find PR: %w", err)
	}
	if len(prs) == 0 {
		return fmt.Errorf("no PR found for branch %s", branchName)
	}

	prToMerge := prs[0]

	// Merge the PR
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
		fmt.Printf("Merged PR #%d: %s\n", prToMerge.Number, commits[0].Subject)
	}

	// If there are more commits, update them
	if len(commits) > 1 && !mergeNoRebase {
		// Retarget PR 2 to main
		secondBranch := stack.StackBranchName(currentBranch, 2, commits[1].Subject)
		secondPRs, err := gh.ListPullRequests(secondBranch)
		if err == nil && len(secondPRs) > 0 {
			err = gh.UpdatePullRequestBase(secondPRs[0].Number, baseBranch)
			if err != nil {
				return fmt.Errorf("failed to retarget PR #%d: %w", secondPRs[0].Number, err)
			}
			output.Retargeted = append(output.Retargeted, secondPRs[0].Number)

			if !jsonOutput {
				fmt.Printf("Retargeted PR #%d to %s\n", secondPRs[0].Number, baseBranch)
			}
		}

		// Pull latest base and rebase
		_, err = g.Run("fetch", "origin", baseBranch)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", baseBranch, err)
		}

		_, err = g.Run("rebase", "origin/"+baseBranch)
		if err != nil {
			g.AbortRebase()
			return fmt.Errorf(`conflict rebasing onto %s
Restored original branch state.

To resolve manually:
  git fetch origin %s
  git rebase origin/%s
  gh stacked push`, baseBranch, baseBranch, baseBranch)
		}

		output.Rebased = true

		// Get new commits and push remaining branches
		newCommits, err := g.GetCommitsSinceBase(baseBranch)
		if err != nil {
			return fmt.Errorf("failed to get commits after rebase: %w", err)
		}

		for i, commit := range newCommits {
			// New position is i+1 (renumbered after first PR merged)
			newPosition := i + 1
			branchName := stack.StackBranchName(currentBranch, newPosition, commit.Subject)

			err := g.CreateBranchAtCommit(branchName, commit.Hash)
			if err != nil {
				return fmt.Errorf("failed to update branch %s: %w", branchName, err)
			}

			err = g.PushBranch("origin", branchName, true)
			if err != nil {
				return fmt.Errorf("failed to push %s: %w", branchName, err)
			}

			// Find PR for this branch to get number
			branchPRs, _ := gh.ListPullRequests(branchName)
			if len(branchPRs) > 0 {
				output.RemainingPRs = append(output.RemainingPRs, branchPRs[0].Number)
			}

			if !jsonOutput {
				fmt.Printf("Updated: %s\n", branchName)
			}
		}
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	fmt.Printf("\nMerge complete. %d PRs remaining.\n", len(output.RemainingPRs))

	return nil
}
