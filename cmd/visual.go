// cmd/visual.go
package cmd

import (
	"fmt"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/github"
	"github.com/calvin/gh-stacked/internal/stack"
	"github.com/spf13/cobra"
)

// Dependency injection points for testing
var (
	visualNewGit    = func() git.GitOperations { return git.New() }
	visualNewGitHub = func() (github.GitHubOperations, error) { return github.NewClient() }
)

var visualCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the stack top-to-bottom with PR titles and links",
	Long: `Show a visual list of your stack from top to bottom.

Each entry shows:
  - Commit hash prefix
  - PR title (or commit subject if no PR)
  - PR link (or a placeholder if not created yet)`,
	Example: `  # Show stack
  gh stacked show`,
	RunE: runVisual,
}

func init() {
	rootCmd.AddCommand(visualCmd)
}

func runVisual(cmd *cobra.Command, args []string) error {
	g := visualNewGit()

	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if stack.IsStackBranch(currentBranch) {
		return fmt.Errorf("run visual from your feature branch, not a stack branch")
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
		fmt.Println("No commits found since", baseBranch)
		return nil
	}

	gh, err := visualNewGitHub()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	remoteBranches, err := g.ListRemoteBranches("origin/stack/" + currentBranch + "/*")
	if err != nil {
		remoteBranches = nil
	}

	prLookup := make(map[string]github.PullRequest)
	getPRForBranch := func(branch string) (*stack.PRInfo, error) {
		prs, err := gh.ListAllPullRequests(branch)
		if err != nil {
			return nil, err
		}
		if len(prs) == 0 {
			return nil, nil
		}
		pr := prs[0]
		prLookup[branch] = pr
		return &stack.PRInfo{
			Number: pr.Number,
			URL:    pr.URL,
			State:  pr.State,
			Merged: pr.Merged,
			Base:   pr.Base.Ref,
		}, nil
	}

	entries := stack.AnalyzeStack(commits, remoteBranches, currentBranch, getPRForBranch)

	var display []stack.StackEntry
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.CommitHash == "" {
			continue
		}
		if entry.Status == "orphaned" || entry.Status == "remote_only" {
			continue
		}
		display = append(display, entry)
	}

	if len(display) == 0 {
		fmt.Println("No commits found since", baseBranch)
		return nil
	}

	for i, entry := range display {
		commitHash := entry.CommitHash
		if len(commitHash) > 7 {
			commitHash = commitHash[:7]
		}

		title := entry.CommitMsg
		if entry.PRNumber > 0 {
			if pr, ok := prLookup[entry.Branch]; ok && pr.Title != "" {
				title = pr.Title
			}
		}

		fmt.Printf("%s %s\n", commitHash, title)
		if entry.PRURL != "" {
			fmt.Println(entry.PRURL)
		} else {
			fmt.Println("(no PR)")
		}

		if i < len(display)-1 {
			fmt.Println()
		}
	}

	return nil
}
