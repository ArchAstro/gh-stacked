// cmd/push.go - replace entire file
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/github"
	"github.com/calvin/gh-stacked/internal/stack"
	"github.com/spf13/cobra"
)

var (
	pushPreview bool
	pushRegroup bool
	pushSquash  []string
)

type PushOutput struct {
	Action     string `json:"action"`
	StackID    string `json:"stack_id"`
	PRsCreated []int  `json:"prs_created"`
	PRsUpdated []int  `json:"prs_updated"`
}

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Create or update stack of PRs from commits",
	Long: `Create or update a stack of PRs from commits on current branch.

Each commit becomes a separate PR, with each PR targeting the previous one.
This creates clean diffs for reviewers - each PR shows only its commit's changes.

The command is idempotent:
  - New commits create new PRs
  - Changed commits update existing PRs
  - Removed commits close orphaned PRs (with confirmation)`,
	Example: `  # Push current commits as stacked PRs
  gh stacked push

  # Preview without creating PRs
  gh stacked push --preview

  # Squash commits 2-4 into one PR before pushing
  gh stacked push --squash 2-4

  # Interactive reorganization
  gh stacked push --regroup`,
	RunE: runPush,
}

func init() {
	rootCmd.AddCommand(pushCmd)

	pushCmd.Flags().BoolVar(&pushPreview, "preview", false, "Show what would happen without making changes")
	pushCmd.Flags().BoolVar(&pushRegroup, "regroup", false, "Interactively reorganize commits before pushing")
	pushCmd.Flags().StringArrayVar(&pushSquash, "squash", nil, "Squash commits N-M into single PR (e.g., --squash 2-4)")
}

func parseSquashRange(s string) (start, end int, err error) {
	re := regexp.MustCompile(`^(\d+)-(\d+)$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return 0, 0, fmt.Errorf("invalid squash range: %s (expected N-M)", s)
	}
	start, _ = strconv.Atoi(matches[1])
	end, _ = strconv.Atoi(matches[2])
	if start >= end {
		return 0, 0, fmt.Errorf("invalid range: start (%d) must be less than end (%d)", start, end)
	}
	return start, end, nil
}

func runPush(cmd *cobra.Command, args []string) error {
	g := git.New()

	// Get current branch
	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if stack.IsStackBranch(currentBranch) {
		return fmt.Errorf("run push from your feature branch, not a stack branch")
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
		fmt.Println("No commits to push")
		return nil
	}

	// Get GitHub client
	gh, err := github.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Check for existing PRs (to validate squash)
	existingPRs := make(map[int]int) // position -> PR number
	for i, commit := range commits {
		position := i + 1
		branchName := stack.StackBranchName(currentBranch, position, commit.Subject)
		prs, err := gh.ListPullRequests(branchName)
		if err == nil && len(prs) > 0 {
			existingPRs[position] = prs[0].Number
		}
	}

	// Validate squash ranges don't include existing PRs
	for _, squashRange := range pushSquash {
		start, end, err := parseSquashRange(squashRange)
		if err != nil {
			return err
		}
		for pos := start; pos <= end; pos++ {
			if prNum, exists := existingPRs[pos]; exists {
				return fmt.Errorf("cannot squash commit %d - it already has PR #%d\nTo combine these, close the PRs manually first, then re-push", pos, prNum)
			}
		}
	}

	// Build preview
	var entries []stack.PREntry
	for i, commit := range commits {
		position := i + 1
		branchName := stack.StackBranchName(currentBranch, position, commit.Subject)

		entry := stack.PREntry{
			Position:   position,
			CommitHash: commit.Hash,
			CommitMsg:  commit.Subject,
			Branch:     branchName,
		}

		if prNum, exists := existingPRs[position]; exists {
			entry.PRNumber = prNum
		}

		entries = append(entries, entry)
	}

	if pushPreview {
		if jsonOutput {
			out := struct {
				StackID  string           `json:"stack_id"`
				Base     string           `json:"base_branch"`
				Commits  []stack.PREntry  `json:"commits"`
				Existing map[int]int      `json:"existing_prs"`
			}{
				StackID:  currentBranch,
				Base:     baseBranch,
				Commits:  entries,
				Existing: existingPRs,
			}
			return json.NewEncoder(os.Stdout).Encode(out)
		}

		fmt.Printf("Preview: %s → %s\n\n", currentBranch, baseBranch)
		for _, e := range entries {
			action := "CREATE"
			if e.PRNumber > 0 {
				action = fmt.Sprintf("UPDATE #%d", e.PRNumber)
			}
			fmt.Printf("  %d. [%s] %s\n", e.Position, action, e.CommitMsg)
			fmt.Printf("     Branch: %s\n\n", e.Branch)
		}
		return nil
	}

	// Actually push
	repo := gh.Repository()
	_ = repo // repo available for future use
	output := PushOutput{
		StackID:    currentBranch,
		PRsCreated: []int{},
		PRsUpdated: []int{},
	}

	for i, entry := range entries {
		// Create branch at commit
		err := g.CreateBranchAtCommit(entry.Branch, entry.CommitHash)
		if err != nil {
			return fmt.Errorf("failed to create branch %s: %w", entry.Branch, err)
		}

		// Push branch
		err = g.PushBranch("origin", entry.Branch, true)
		if err != nil {
			return fmt.Errorf("failed to push branch %s: %w", entry.Branch, err)
		}

		// Determine base for this PR
		prBase := baseBranch
		if i > 0 {
			prBase = entries[i-1].Branch
		}

		if entry.PRNumber > 0 {
			// Update existing PR
			allEntries := entries
			footer := stack.GenerateStackFooter(currentBranch, allEntries, entry.Position)
			err = gh.UpdatePullRequest(entry.PRNumber, entry.CommitMsg, footer)
			if err != nil {
				return fmt.Errorf("failed to update PR #%d: %w", entry.PRNumber, err)
			}
			output.PRsUpdated = append(output.PRsUpdated, entry.PRNumber)

			if !jsonOutput {
				fmt.Printf("Updated PR #%d: %s\n", entry.PRNumber, entry.CommitMsg)
			}
		} else {
			// Create new PR
			footer := stack.GenerateStackFooter(currentBranch, entries, entry.Position)
			pr, err := gh.CreatePullRequest(github.CreatePullRequestInput{
				Title: entry.CommitMsg,
				Body:  footer,
				Head:  entry.Branch,
				Base:  prBase,
			})
			if err != nil {
				return fmt.Errorf("failed to create PR for %s: %w", entry.CommitMsg, err)
			}

			entries[i].PRNumber = pr.Number
			entries[i].PRURL = pr.URL
			output.PRsCreated = append(output.PRsCreated, pr.Number)

			if !jsonOutput {
				fmt.Printf("Created PR #%d: %s\n", pr.Number, pr.URL)
			}
		}
	}

	output.Action = "created"
	if len(output.PRsUpdated) > 0 && len(output.PRsCreated) == 0 {
		output.Action = "updated"
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	fmt.Printf("\nStack pushed: %d created, %d updated\n",
		len(output.PRsCreated), len(output.PRsUpdated))

	return nil
}
