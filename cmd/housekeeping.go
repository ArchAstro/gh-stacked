// cmd/housekeeping.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/github"
	"github.com/calvin/gh-stacked/internal/stack"
	"github.com/spf13/cobra"
)

// Dependency injection for testing
var (
	housekeepingNewGit    = func() git.GitOperations { return git.New() }
	housekeepingNewGitHub = func() (github.GitHubOperations, error) { return github.NewClient() }
)

var housekeepingCmd = &cobra.Command{
	Use:   "housekeeping",
	Short: "Analyze stack health and show commit/PR mapping",
	Long: `Analyze the stack by comparing local commits with remote branches and their PRs.

Shows a table of:
  - Matched: Local commits that have corresponding open PRs
  - Dangling: Commits before first matched (already merged, need cleanup)
  - Local only: Commits without open PRs (need to be pushed)
  - Remote only: Open PRs without matching local commits (orphaned PRs)
  - Orphaned: Remote branches with merged/closed PRs (can be deleted)`,
	Example: `  # Show stack analysis
  gh stacked housekeeping

  # Machine-readable output
  gh stacked housekeeping --json`,
	RunE: runHousekeeping,
}

func init() {
	rootCmd.AddCommand(housekeepingCmd)
}

func runHousekeeping(cmd *cobra.Command, args []string) error {
	g := housekeepingNewGit()

	// Get current branch
	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if stack.IsStackBranch(currentBranch) {
		return fmt.Errorf("run housekeeping from your feature branch, not a stack branch")
	}

	// Get base branch
	baseBranch, err := g.GetBaseBranch()
	if err != nil {
		return fmt.Errorf("failed to get base branch: %w", err)
	}

	// Get local commits
	commits, err := g.GetCommitsSinceBase(baseBranch)
	if err != nil {
		return fmt.Errorf("failed to get commits: %w", err)
	}

	// Get GitHub client
	gh, err := housekeepingNewGitHub()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Fetch remote branches
	remoteBranches, err := g.ListRemoteBranches("origin/stack/" + currentBranch + "/*")
	if err != nil {
		remoteBranches = nil
	}

	// Create PR lookup function
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

	// Run analysis
	entries := stack.AnalyzeStack(commits, remoteBranches, currentBranch, getPRForBranch)
	summary := stack.SummarizeAnalysis(entries)

	if jsonOutput {
		out := struct {
			StackID string              `json:"stack_id"`
			Base    string              `json:"base_branch"`
			Entries []stack.StackEntry  `json:"entries"`
			Summary stack.StackAnalysisSummary `json:"summary"`
		}{
			StackID: currentBranch,
			Base:    baseBranch,
			Entries: entries,
			Summary: summary,
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}

	// Human-readable output
	fmt.Printf("Stack: %s\n", currentBranch)
	fmt.Printf("Base: %s\n\n", baseBranch)

	// Print table header
	fmt.Println("  #   Status       Position  PR       Commit/Branch")
	fmt.Println("  ─── ──────────── ──────── ──────── ─────────────────────────────────────────")

	for i, e := range entries {
		var posStr, prStr, descStr string

		if e.Position > 0 {
			posStr = fmt.Sprintf("%d", e.Position)
		} else {
			posStr = "-"
		}

		if e.PRNumber > 0 {
			prStr = fmt.Sprintf("#%d", e.PRNumber)
			if e.PRState != "open" {
				prStr += fmt.Sprintf(" (%s)", e.PRState)
			}
		} else {
			prStr = "-"
		}

		if e.CommitHash != "" {
			descStr = fmt.Sprintf("%s %s", e.CommitHash[:7], e.CommitMsg)
		} else {
			descStr = e.Branch
		}

		// Truncate description if too long
		if len(descStr) > 45 {
			descStr = descStr[:42] + "..."
		}

		statusIcon := ""
		switch e.Status {
		case "matched":
			statusIcon = "✓ matched"
		case "dangling":
			statusIcon = "✗ dangling"
		case "local_only":
			statusIcon = "⚠ local only"
		case "remote_only":
			statusIcon = "⚠ remote only"
		case "orphaned":
			statusIcon = "✗ orphaned"
		}

		fmt.Printf("  %-3d %-12s %-8s %-8s %s\n", i+1, statusIcon, posStr, prStr, descStr)
	}

	// Print summary
	fmt.Println()
	fmt.Println("Summary:")
	fmt.Printf("  Local commits: %d\n", summary.TotalCommits)
	fmt.Printf("  Open PRs: %d\n", summary.TotalOpenPRs)
	fmt.Printf("  Matched: %d\n", summary.Matched)
	if summary.Dangling > 0 {
		fmt.Printf("  Dangling (run 'gh stacked cleanup'): %d\n", summary.Dangling)
	}
	if summary.LocalOnly > 0 {
		fmt.Printf("  Local only (need push): %d\n", summary.LocalOnly)
	}
	if summary.RemoteOnly > 0 {
		fmt.Printf("  Remote only (orphaned PRs): %d\n", summary.RemoteOnly)
	}
	if summary.Orphaned > 0 {
		fmt.Printf("  Orphaned branches (can delete): %d\n", summary.Orphaned)
	}

	return nil
}
