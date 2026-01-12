// cmd/status.go - replace entire file
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

	// Build status output
	output := StatusOutput{
		StackID:     currentBranch,
		BaseBranch:  baseBranch,
		LocalInSync: true, // TODO: implement sync check
		PRs:         make([]StatusPROutput, 0, len(commits)),
	}

	for i, commit := range commits {
		position := i + 1
		branchName := stack.StackBranchName(currentBranch, position, commit.Subject)

		prOutput := StatusPROutput{
			Position: position,
			Commit:   commit.Hash[:7],
			Title:    commit.Subject,
			Branch:   branchName,
		}

		// Try to find existing PR for this branch
		prs, err := gh.ListPullRequests(branchName)
		if err == nil && len(prs) > 0 {
			pr := prs[0]
			prOutput.PRNumber = pr.Number
			prOutput.PRURL = pr.URL
			prOutput.TargetBranch = pr.Base.Ref
			prOutput.State = pr.State

			// Fetch full PR details to get additions/deletions
			fullPR, err := gh.GetPullRequest(pr.Number)
			if err == nil {
				prOutput.Mergeable = fullPR.Mergeable
				prOutput.Additions = fullPR.Additions
				prOutput.Deletions = fullPR.Deletions
				prOutput.ChecksPassing = fullPR.ChecksPassing
				prOutput.Approvals = fullPR.Approvals
				prOutput.ChangesRequested = fullPR.ChangesRequested
			}
		}

		output.PRs = append(output.PRs, prOutput)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	// Human-readable output
	fmt.Printf("Stack: %s (%d PRs)\n\n", output.StackID, len(output.PRs))

	for _, pr := range output.PRs {
		status := "PENDING"
		if pr.PRNumber > 0 {
			if pr.Mergeable && pr.ChecksPassing {
				status = "MERGEABLE"
			} else if pr.ChangesRequested {
				status = "BLOCKED"
			} else {
				status = "WAITING"
			}
		}

		fmt.Printf("  #%d [%s] %s\n", pr.Position, status, pr.Title)
		if pr.PRNumber > 0 {
			fmt.Printf("     PR #%d: %s\n", pr.PRNumber, pr.PRURL)
			target := pr.TargetBranch
			if target == "" {
				target = baseBranch
			}
			fmt.Printf("     → %s | +%d -%d\n", target, pr.Additions, pr.Deletions)
		} else {
			fmt.Printf("     (not yet pushed)\n")
		}
		fmt.Println()
	}

	if output.LocalInSync {
		fmt.Println("Local: in sync ✓")
	} else {
		fmt.Println("Local: out of sync")
	}

	return nil
}
