package cmd

import (
	"encoding/json"
	"fmt"
	"os"
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
	Merged          int      `json:"merged"`
	Retargeted      []int    `json:"retargeted"`
	Rebased         bool     `json:"rebased"`
	RemainingPRs    []int    `json:"remaining_prs"`
	DeletedBranches []string `json:"deleted_branches"`
}

var mergeCmd = &cobra.Command{
	Use:   "merge",
	Short: "Merge the next PR in the stack",
	Long:  `Merge the first PR in the stack and rebase remaining commits.`,
	Args:  cobra.NoArgs,
	RunE:  runMerge,
}

func init() {
	rootCmd.AddCommand(mergeCmd)
	mergeCmd.Flags().BoolVar(&mergeNoRebase, "no-rebase", false, "Don't rebase remaining stack after merge")
}

func runMerge(cmd *cobra.Command, args []string) error {
	g := mergeNewGit()

	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if stack.IsStackBranch(currentBranch) {
		return fmt.Errorf("run merge from your feature branch, not a stack branch")
	}

	// Check sync status
	syncChecker := git.NewSyncChecker(g)
	if !jsonOutput {
		fmt.Println("Checking sync status...")
	}
	syncStatus, err := syncChecker.EnsureSynced(false)
	if err != nil {
		if !jsonOutput {
			fmt.Printf("Warning: could not check sync status: %v\n", err)
		}
	} else if syncStatus.NeedsRebase {
		return fmt.Errorf("branch is out of sync with %s\n\n%s\nAfter syncing, run 'gh stacked merge' again",
			syncStatus.BaseBranch, syncStatus.FormatSyncMessage())
	}

	baseBranch, err := g.GetBaseBranch()
	if err != nil {
		return fmt.Errorf("failed to get base branch: %w", err)
	}

	commits, err := g.GetCommitsSinceBase(baseBranch)
	if err != nil {
		return fmt.Errorf("failed to get commits: %w", err)
	}

	if len(commits) == 0 {
		return fmt.Errorf("no commits in stack")
	}

	gh, err := mergeNewGitHub()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	remoteBranches, err := g.ListRemoteBranches("origin/stack/" + currentBranch + "/*")
	if err != nil {
		remoteBranches = nil
	}

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

	// Find first open PR
	var firstPR *stack.StackEntry
	for i := range entries {
		if entries[i].Status == "matched" && entries[i].PRState == "open" {
			firstPR = &entries[i]
			break
		}
	}

	if firstPR == nil {
		return fmt.Errorf("no open PRs found - run 'gh stacked push' first")
	}

	// Get the PR details
	prs, err := gh.ListPullRequests(firstPR.Branch)
	if err != nil {
		return fmt.Errorf("failed to get PR: %w", err)
	}
	if len(prs) == 0 {
		return fmt.Errorf("no open PR found for branch %s", firstPR.Branch)
	}
	pr := prs[0]

	// Count remaining PRs
	remainingCount := 0
	var remainingBranches []stack.ExistingBranchInfo
	for _, e := range entries {
		if e.Status == "matched" && e.PRState == "open" && e.Branch != firstPR.Branch {
			remainingCount++
			remainingBranches = append(remainingBranches, stack.ExistingBranchInfo{
				Position: e.Position,
				Branch:   e.Branch,
				Slug:     stack.Slugify(e.CommitMsg, 5),
			})
		}
	}

	// Pre-flight rebase check (always check unless --no-rebase)
	if !mergeNoRebase {
		if !jsonOutput {
			fmt.Println("Checking if rebase will succeed...")
		}

		_, err = g.Run("fetch", "origin")
		if err != nil {
			return fmt.Errorf("failed to fetch origin: %w", err)
		}

		originalHead, err := g.Run("rev-parse", "HEAD")
		if err != nil {
			return fmt.Errorf("failed to get current HEAD: %w", err)
		}

		rebaseTarget := "origin/" + strings.TrimPrefix(baseBranch, "origin/")
		_, testErr := g.Run("rebase", rebaseTarget)
		if testErr != nil {
			g.AbortRebase()
			g.Run("reset", "--hard", originalHead)
			return fmt.Errorf(`rebase onto %s will conflict - resolve first:
  git fetch origin && git rebase %s
  gh stacked push
Then retry: gh stacked merge`, rebaseTarget, rebaseTarget)
		}

		g.AbortRebase()
		g.Run("reset", "--hard", originalHead)

		if !jsonOutput {
			fmt.Println("Pre-flight check passed.")
		}
	}

	output := MergeOutput{
		Retargeted:      []int{},
		RemainingPRs:    []int{},
		DeletedBranches: []string{},
	}

	targetBase := strings.TrimPrefix(baseBranch, "origin/")

	// Retarget next PR to main BEFORE merging
	if remainingCount > 0 {
		nextBranch := remainingBranches[0]
		nextPRs, err := gh.ListPullRequests(nextBranch.Branch)
		if err == nil && len(nextPRs) > 0 {
			if !jsonOutput {
				fmt.Printf("Retargeting PR #%d to %s...\n", nextPRs[0].Number, targetBase)
			}
			err = gh.UpdatePullRequestBase(nextPRs[0].Number, targetBase)
			if err != nil {
				return fmt.Errorf("failed to retarget PR #%d: %w", nextPRs[0].Number, err)
			}
			output.Retargeted = append(output.Retargeted, nextPRs[0].Number)
		}
	}

	// Merge the PR
	if !jsonOutput {
		fmt.Printf("Merging PR #%d...\n", pr.Number)
	}
	err = gh.MergePullRequest(pr.Number)
	if err != nil {
		return fmt.Errorf("failed to merge PR #%d: %w", pr.Number, err)
	}
	output.Merged = pr.Number

	// Clean up merged branch
	branchName := firstPR.Branch
	err = g.PushBranch("origin", ":"+branchName, false)
	if err != nil {
		if !jsonOutput {
			fmt.Printf("Warning: failed to delete remote %s: %v\n", branchName, err)
		}
	} else {
		output.DeletedBranches = append(output.DeletedBranches, branchName)
		if !jsonOutput {
			fmt.Printf("Deleted branch: %s\n", branchName)
		}
	}
	g.Run("branch", "-D", branchName) // Ignore error - local may not exist

	// Rebase local branch onto updated main
	if !mergeNoRebase {
		_, err = g.Run("fetch", "origin")
		if err != nil {
			return fmt.Errorf("failed to fetch origin: %w", err)
		}

		rebaseTarget := "origin/" + strings.TrimPrefix(baseBranch, "origin/")
		if !jsonOutput {
			fmt.Printf("Rebasing onto %s...\n", rebaseTarget)
		}
		_, err = g.Run("rebase", rebaseTarget)
		if err != nil {
			g.AbortRebase()
			return fmt.Errorf(`unexpected rebase conflict - resolve manually:
  git fetch origin && git rebase %s
  gh stacked push`, rebaseTarget)
		}

		output.Rebased = true

		// Update remaining branches if any
		if remainingCount > 0 {
			newCommits, err := g.GetCommitsSinceBase(baseBranch)
			if err != nil {
				return fmt.Errorf("failed to get commits after rebase: %w", err)
			}

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
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	fmt.Printf("\nMerged PR #%d. %d PRs remaining.\n", output.Merged, len(output.RemainingPRs))

	return nil
}
