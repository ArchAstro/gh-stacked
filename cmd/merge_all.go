package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ArchAstro/gh-stacked/internal/git"
	"github.com/ArchAstro/gh-stacked/internal/github"
	"github.com/ArchAstro/gh-stacked/internal/stack"
	"github.com/spf13/cobra"
)

// Dependency injection points for testing
var (
	mergeAllNewGit    = func() git.GitOperations { return git.New() }
	mergeAllNewGitHub = func() (github.GitHubOperations, error) { return github.NewClient() }
)

type MergeAllOutput struct {
	Merged          int      `json:"merged"`
	Closed          []int    `json:"closed"`
	DeletedBranches []string `json:"deleted_branches"`
	SquashedCommits int      `json:"squashed_commits"`
}

var mergeAllNoVerify bool

var mergeAllCmd = &cobra.Command{
	Use:   "merge-all",
	Short: "Squash and merge entire stack as one PR",
	Long:  `Squash all commits into one, update the last PR, merge it, and close the rest.`,
	Args:  cobra.NoArgs,
	RunE:  runMergeAll,
}

func init() {
	rootCmd.AddCommand(mergeAllCmd)
	mergeAllCmd.Flags().BoolVar(&mergeAllNoVerify, "no-verify", false, "Skip pre-commit and commit-msg hooks")
}

func runMergeAll(cmd *cobra.Command, args []string) error {
	g := mergeAllNewGit()

	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if stack.IsStackBranch(currentBranch) {
		return fmt.Errorf("run merge-all from your feature branch, not a stack branch")
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

	gh, err := mergeAllNewGitHub()
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

	// Collect all open PRs from already-fetched stack entries (no redundant API calls)
	var openPRs []stack.StackEntry
	for _, e := range entries {
		if e.Status == "matched" && e.PRState == "open" {
			openPRs = append(openPRs, e)
		}
	}

	if len(openPRs) == 0 {
		return fmt.Errorf("no open PRs found - run 'gh stacked push' first")
	}

	output := MergeAllOutput{
		Closed:          []int{},
		DeletedBranches: []string{},
		SquashedCommits: len(commits),
	}

	// The last PR in the stack (highest position) will be the one we merge
	lastPR := openPRs[len(openPRs)-1]
	targetBase := strings.TrimPrefix(baseBranch, "origin/")

	if !jsonOutput {
		fmt.Printf("Squashing %d commits into one...\n", len(commits))
	}

	// Fetch latest base
	_, err = g.Run("fetch", "origin")
	if err != nil {
		return fmt.Errorf("failed to fetch origin: %w", err)
	}

	// Create squash commit message from all commit messages
	var commitMsgs []string
	for _, c := range commits {
		commitMsgs = append(commitMsgs, fmt.Sprintf("- %s", c.Subject))
	}
	squashMsg := fmt.Sprintf("Squashed %d commits:\n\n%s", len(commits), strings.Join(commitMsgs, "\n"))

	// Save current HEAD for recovery
	originalHead, err := g.Run("rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("failed to get current HEAD: %w", err)
	}
	originalHead = strings.TrimSpace(originalHead)

	// Soft reset to base and create single commit
	rebaseTarget := "origin/" + targetBase
	_, err = g.Run("reset", "--soft", rebaseTarget)
	if err != nil {
		return fmt.Errorf("failed to reset to base: %w", err)
	}

	// Create the squashed commit
	commitArgs := []string{"commit", "-m", squashMsg}
	if mergeAllNoVerify {
		commitArgs = append(commitArgs, "--no-verify")
	}
	_, err = g.Run(commitArgs...)
	if err != nil {
		// Recovery: restore original state
		g.Run("reset", "--hard", originalHead)
		return fmt.Errorf("failed to create squash commit: %w", err)
	}

	// Helper to rebase onto latest main and push
	rebaseAndPush := func() error {
		if !jsonOutput {
			fmt.Printf("Rebasing onto %s...\n", rebaseTarget)
		}

		_, err = g.Run("fetch", "origin")
		if err != nil {
			return fmt.Errorf("failed to fetch origin: %w", err)
		}

		_, err = g.Run("rebase", rebaseTarget)
		if err != nil {
			g.AbortRebase()
			return fmt.Errorf("rebase conflict - cannot merge automatically. Resolve conflicts manually and retry:\n  git fetch origin && git rebase %s", rebaseTarget)
		}

		if !jsonOutput {
			fmt.Printf("Pushing to %s...\n", lastPR.Branch)
		}

		err = g.CreateBranchAtCommit(lastPR.Branch, "HEAD")
		if err != nil {
			return fmt.Errorf("failed to update branch %s: %w", lastPR.Branch, err)
		}

		err = g.PushBranch("origin", lastPR.Branch, true)
		if err != nil {
			return fmt.Errorf("failed to push %s: %w", lastPR.Branch, err)
		}

		return nil
	}

	// Initial rebase and push
	if err := rebaseAndPush(); err != nil {
		g.Run("reset", "--hard", originalHead)
		return err
	}

	// Retarget the last PR to main (it may have been targeting another stack branch)
	if !jsonOutput {
		fmt.Printf("Retargeting PR #%d to %s...\n", lastPR.PRNumber, targetBase)
	}
	err = gh.UpdatePullRequestBase(lastPR.PRNumber, targetBase)
	if err != nil {
		return fmt.Errorf("failed to retarget PR #%d: %w", lastPR.PRNumber, err)
	}

	// Merge with retry logic to handle main moving between push and merge.
	// If merge fails due to branch being out of date, rebase and try again.
	const maxMergeRetries = 3
	var mergeErr error
	for attempt := 1; attempt <= maxMergeRetries; attempt++ {
		if !jsonOutput {
			if attempt > 1 {
				fmt.Printf("Retrying merge (attempt %d/%d)...\n", attempt, maxMergeRetries)
			}
			fmt.Printf("Merging PR #%d...\n", lastPR.PRNumber)
		}

		mergeErr = gh.MergePullRequest(lastPR.PRNumber)
		if mergeErr == nil {
			break // Success
		}

		// Check if this is a "not up to date" error that we can retry
		errStr := mergeErr.Error()
		isStaleError := strings.Contains(errStr, "not up to date") ||
			strings.Contains(errStr, "out of date") ||
			strings.Contains(errStr, "Head branch was modified")

		if !isStaleError || attempt == maxMergeRetries {
			break // Non-retryable error or out of retries
		}

		if !jsonOutput {
			fmt.Printf("PR is out of date with %s, rebasing...\n", targetBase)
		}

		if err := rebaseAndPush(); err != nil {
			g.Run("reset", "--hard", originalHead)
			return fmt.Errorf("failed to update PR after main moved: %w", err)
		}
	}

	if mergeErr != nil {
		// Provide actionable error message
		errStr := mergeErr.Error()
		if strings.Contains(errStr, "not up to date") || strings.Contains(errStr, "out of date") {
			return fmt.Errorf("PR #%d could not be merged - branch protection requires it to be up to date with %s.\n"+
				"The PR has been pushed and retargeted. You can retry with:\n"+
				"  gh pr merge %d --squash", lastPR.PRNumber, targetBase, lastPR.PRNumber)
		}
		if strings.Contains(errStr, "review") || strings.Contains(errStr, "approval") {
			return fmt.Errorf("PR #%d could not be merged - requires review approval.\n"+
				"The PR has been pushed and retargeted to %s. Merge manually after approval:\n"+
				"  gh pr merge %d --squash", lastPR.PRNumber, targetBase, lastPR.PRNumber)
		}
		if strings.Contains(errStr, "check") || strings.Contains(errStr, "status") {
			return fmt.Errorf("PR #%d could not be merged - required status checks have not passed.\n"+
				"The PR has been pushed and retargeted to %s. Merge manually after checks pass:\n"+
				"  gh pr merge %d --squash", lastPR.PRNumber, targetBase, lastPR.PRNumber)
		}
		return fmt.Errorf("failed to merge PR #%d: %w\n"+
			"The PR has been pushed and retargeted to %s. You may be able to merge manually:\n"+
			"  gh pr merge %d --squash", lastPR.PRNumber, mergeErr, targetBase, lastPR.PRNumber)
	}
	output.Merged = lastPR.PRNumber

	// CRITICAL: Reset local to main IMMEDIATELY after merge succeeds.
	// This ensures local state is clean regardless of whether subsequent
	// cleanup operations (close PRs, delete branches) fail.
	// Any failure after this point is cosmetic - local state is safe.
	if !jsonOutput {
		fmt.Println("Syncing local branch to merged main...")
	}
	_, err = g.Run("fetch", "origin")
	if err != nil {
		// Non-fatal: warn but continue - merge succeeded
		if !jsonOutput {
			fmt.Printf("Warning: failed to fetch origin: %v\n", err)
		}
	} else {
		_, err = g.Run("reset", "--hard", rebaseTarget)
		if err != nil {
			if !jsonOutput {
				fmt.Printf("Warning: failed to reset to %s: %v\n", rebaseTarget, err)
			}
		}
	}

	// Close all other PRs
	for _, pr := range openPRs {
		if pr.PRNumber == lastPR.PRNumber {
			continue
		}
		if !jsonOutput {
			fmt.Printf("Closing PR #%d...\n", pr.PRNumber)
		}
		err = gh.ClosePullRequest(pr.PRNumber)
		if err != nil {
			if !jsonOutput {
				fmt.Printf("Warning: failed to close PR #%d: %v\n", pr.PRNumber, err)
			}
		} else {
			output.Closed = append(output.Closed, pr.PRNumber)
		}
	}

	// Delete all stack branches
	if !jsonOutput {
		fmt.Println("Cleaning up branches...")
	}
	for _, pr := range openPRs {
		// Delete remote branch
		err := g.PushBranch("origin", ":"+pr.Branch, false)
		if err != nil {
			if !jsonOutput {
				fmt.Printf("Warning: failed to delete remote %s: %v\n", pr.Branch, err)
			}
		} else {
			output.DeletedBranches = append(output.DeletedBranches, pr.Branch)
			if !jsonOutput {
				fmt.Printf("  Deleted: %s\n", pr.Branch)
			}
		}

		// Delete local branch if it exists
		g.Run("branch", "-D", pr.Branch)
	}

	// Local branch already reset to main above - no rebase needed

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	fmt.Printf("\nMerged PR #%d with %d squashed commits. Closed %d other PRs.\n",
		output.Merged, output.SquashedCommits, len(output.Closed))

	return nil
}
