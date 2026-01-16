// cmd/status.go - replace entire file
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

// Dependency injection points for testing
var (
	statusNewGit    = func() git.GitOperations { return git.New() }
	statusNewGitHub = func() (github.GitHubOperations, error) { return github.NewClient() }
)

type StatusOutput struct {
	StackID     string           `json:"stack_id"`
	BaseBranch  string           `json:"base_branch"`
	LocalInSync bool             `json:"local_in_sync"`
	PRs         []StatusPROutput `json:"prs"`
}

type StatusPROutput struct {
	Position         int    `json:"position"`
	Commit           string `json:"commit"`
	Title            string `json:"title"`
	PRNumber         int    `json:"pr_number"`
	PRURL            string `json:"pr_url"`
	Branch           string `json:"branch"`
	TargetBranch     string `json:"target_branch"`
	State            string `json:"state"`
	Draft            bool   `json:"draft"`
	Status           string `json:"status"` // "matched", "local_only", "dangling"
	Mergeable        bool   `json:"mergeable"`
	ChecksPassing    bool   `json:"checks_passing"`
	Approvals        int    `json:"approvals"`
	ChangesRequested bool   `json:"changes_requested"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current stack state with PR links",
	Long: `Show the current state of your PR stack.

Displays each PR with:
  - Position in stack
  - Merge status (MERGEABLE, BLOCKED, WAITING)
  - PR link
  - Target branch
  - Diff stats
  - Review status`,
	Example: `  # Show stack status
  gh stacked status

  # Machine-readable output
  gh stacked status --json`,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	g := statusNewGit()

	// Get current branch
	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	// Check if we're on a stack branch or the parent branch
	if stack.IsStackBranch(currentBranch) {
		return fmt.Errorf("run status from your feature branch, not a stack branch")
	}

	// Check sync status with remote - fetch and detect merged commits
	var syncWarning string
	syncChecker := git.NewSyncChecker(g)
	syncStatus, err := syncChecker.EnsureSynced(false) // Don't auto-rebase, just check
	if err != nil {
		// Non-fatal for status - just show warning
		if !jsonOutput {
			fmt.Printf("Warning: could not check sync status: %v\n\n", err)
		}
	} else if syncStatus.NeedsRebase {
		syncWarning = syncStatus.FormatSyncMessage()
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
		if jsonOutput {
			out := StatusOutput{
				StackID:     currentBranch,
				BaseBranch:  baseBranch,
				LocalInSync: true,
				PRs:         []StatusPROutput{},
			}
			return json.NewEncoder(os.Stdout).Encode(out)
		}
		fmt.Println("No commits found since", baseBranch)
		return nil
	}

	// Try to get GitHub client
	gh, err := statusNewGitHub()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Fetch remote branches
	remoteBranches, err := g.ListRemoteBranches("origin/stack/" + currentBranch + "/*")
	if err != nil {
		remoteBranches = nil
	}

	// Use AnalyzeStack for consistent matching logic
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

	// Build status output from analysis
	output := StatusOutput{
		StackID:     currentBranch,
		BaseBranch:  baseBranch,
		LocalInSync: true,
		PRs:         make([]StatusPROutput, 0, len(entries)),
	}

	for _, e := range entries {
		// Skip orphaned and remote-only entries for status display
		if e.Status == "orphaned" || e.Status == "remote_only" {
			continue
		}

		prOutput := StatusPROutput{
			Position: e.Position,
			Commit:   e.CommitHash[:7],
			Title:    e.CommitMsg,
			Branch:   e.Branch,
			PRNumber: e.PRNumber,
			PRURL:    e.PRURL,
			State:    e.PRState,
			Status:   e.Status,
		}

		// Fetch full PR details if we have a PR
		if e.PRNumber > 0 {
			fullPR, err := gh.GetPullRequest(e.PRNumber)
			if err == nil {
				prOutput.Draft = fullPR.Draft
				prOutput.Mergeable = fullPR.Mergeable
				prOutput.Additions = fullPR.Additions
				prOutput.Deletions = fullPR.Deletions
				prOutput.ChecksPassing = fullPR.ChecksPassing
				prOutput.Approvals = fullPR.Approvals
				prOutput.ChangesRequested = fullPR.ChangesRequested
				prOutput.TargetBranch = fullPR.BaseBranch
			}
		}

		output.PRs = append(output.PRs, prOutput)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	// Human-readable output - table format matching housekeeping
	fmt.Printf("Stack: %s\n", output.StackID)
	fmt.Printf("Base: %s\n\n", baseBranch)

	// Print table header
	fmt.Println("  #   Status       Branch    PR       Commit                                    +/-")
	fmt.Println("  ─── ──────────── ──────── ──────── ───────────────────────────────────────── ─────────")

	for i, pr := range output.PRs {
		stackIndex := i + 1 // 1-based index

		var statusIcon string
		if pr.PRNumber > 0 {
			if pr.Draft {
				statusIcon = "📝 draft"
			} else if pr.Mergeable && pr.ChecksPassing {
				statusIcon = "✓ mergeable"
			} else if pr.ChangesRequested {
				statusIcon = "✗ blocked"
			} else {
				statusIcon = "⏳ waiting"
			}
		} else if pr.Status == "dangling" {
			statusIcon = "✗ dangling"
		} else {
			statusIcon = "⚠ local"
		}

		var posStr, prStr, diffStr string
		if pr.Position > 0 {
			posStr = fmt.Sprintf("%d", pr.Position)
		} else {
			posStr = "-"
		}

		if pr.PRNumber > 0 {
			// OSC 8 hyperlink: \e]8;;URL\e\\TEXT\e]8;;\e\\
			// Add padding AFTER escape sequence since Printf counts escape bytes
			prText := fmt.Sprintf("#%d", pr.PRNumber)
			prLink := fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", pr.PRURL, prText)
			prStr = prLink + strings.Repeat(" ", 6-len(prText))
			diffStr = fmt.Sprintf("+%d -%d", pr.Additions, pr.Deletions)
		} else {
			prStr = fmt.Sprintf("%-6s", "-")
			diffStr = ""
		}

		descStr := fmt.Sprintf("%s %s", pr.Commit, pr.Title)
		if len(descStr) > 45 {
			descStr = descStr[:42] + "..."
		}

		fmt.Printf("  %-3d %-12s %-8s %s %-45s %s\n", stackIndex, statusIcon, posStr, prStr, descStr, diffStr)
	}

	// Summary
	matchedCount := 0
	localOnlyCount := 0
	danglingCount := 0
	for _, pr := range output.PRs {
		switch pr.Status {
		case "matched":
			matchedCount++
		case "local_only":
			localOnlyCount++
		case "dangling":
			danglingCount++
		}
	}

	fmt.Println()
	fmt.Println("Summary:")
	fmt.Printf("  Commits: %d\n", len(output.PRs))
	if matchedCount > 0 {
		fmt.Printf("  With PRs: %d\n", matchedCount)
	}
	if localOnlyCount > 0 {
		fmt.Printf("  Local only (run 'gh stacked push'): %d\n", localOnlyCount)
	}
	if danglingCount > 0 {
		fmt.Printf("  Dangling (run 'gh stacked cleanup'): %d\n", danglingCount)
	}

	if syncWarning != "" {
		fmt.Println()
		fmt.Println("Sync status:")
		fmt.Print(syncWarning)
	}

	return nil
}
