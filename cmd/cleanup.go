// cmd/cleanup.go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ArchAstro/gh-stacked/internal/git"
	"github.com/ArchAstro/gh-stacked/internal/github"
	"github.com/ArchAstro/gh-stacked/internal/stack"
	"github.com/spf13/cobra"
)

// Dependency injection for testing
var (
	cleanupNewGit    = func() git.GitOperations { return git.New() }
	cleanupNewGitHub = func() (github.GitHubOperations, error) { return github.NewClient() }
)

var cleanupDryRun bool

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove dangling commits and orphaned remote branches",
	Long: `Clean up the stack by removing dangling commits and orphaned branches.

Dangling commits are local commits that appear before the first matched PR,
indicating they have already been merged into the base branch (via squash merge)
but still exist in your local history.

Orphaned branches are remote stack branches whose PRs have been merged or closed.
These can safely be deleted from the remote.

This command will:
  1. Identify dangling commits and orphaned branches
  2. Show what will be removed
  3. Rebase to remove dangling commits
  4. Delete orphaned remote branches`,
	Example: `  # Preview what would be cleaned up
  gh stacked cleanup --dry-run

  # Remove dangling commits and orphaned branches
  gh stacked cleanup`,
	RunE: runCleanup,
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().BoolVar(&cleanupDryRun, "dry-run", false, "Show what would be removed without making changes")
}

func runCleanup(cmd *cobra.Command, args []string) error {
	g := cleanupNewGit()

	// Get current branch
	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if stack.IsStackBranch(currentBranch) {
		return fmt.Errorf("run cleanup from your feature branch, not a stack branch")
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
	gh, err := cleanupNewGitHub()
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
			Merged: pr.Merged,
			Base:   pr.Base.Ref,
		}, nil
	}

	// Run analysis
	entries := stack.AnalyzeStack(commits, remoteBranches, currentBranch, getPRForBranch)
	dangling := stack.GetDanglingCommits(entries)
	orphaned := stack.GetOrphanedBranches(entries)

	if len(dangling) == 0 && len(orphaned) == 0 {
		fmt.Println("Nothing to clean up. Stack is clean!")
		return nil
	}

	// Show what will be removed
	if len(dangling) > 0 {
		fmt.Printf("Found %d dangling commit(s) to remove:\n", len(dangling))
		for _, d := range dangling {
			fmt.Printf("  %s %s\n", d.CommitHash[:7], d.CommitMsg)
		}
		fmt.Println()
	}

	if len(orphaned) > 0 {
		fmt.Printf("Found %d orphaned remote branch(es) to delete:\n", len(orphaned))
		for _, o := range orphaned {
			prInfo := ""
			if o.PRNumber > 0 {
				prInfo = fmt.Sprintf(" (PR #%d %s)", o.PRNumber, o.PRState)
			}
			fmt.Printf("  %s%s\n", o.Branch, prInfo)
		}
		fmt.Println()
	}

	if cleanupDryRun {
		fmt.Println("Dry run - no changes made.")
		fmt.Println("Run without --dry-run to clean up.")
		return nil
	}

	// Remove dangling commits
	if len(dangling) > 0 {
		var dropCommits []string
		for _, d := range dangling {
			dropCommits = append(dropCommits, d.CommitHash[:7])
		}

		fmt.Printf("Rebasing to remove %d dangling commit(s)...\n", len(dangling))
		err = runRebaseWithDrops(g, baseBranch, dropCommits)
		if err != nil {
			return fmt.Errorf("rebase failed: %w\n\nYou may need to resolve conflicts manually.\nRun 'git rebase --abort' to undo.", err)
		}
		fmt.Printf("Removed %d dangling commit(s).\n\n", len(dangling))
	}

	// Delete orphaned remote branches
	if len(orphaned) > 0 {
		fmt.Printf("Deleting %d orphaned remote branch(es)...\n", len(orphaned))
		for _, o := range orphaned {
			err := g.PushBranch("origin", ":"+o.Branch, false)
			if err != nil {
				fmt.Printf("  Warning: failed to delete %s: %v\n", o.Branch, err)
			} else {
				fmt.Printf("  Deleted %s\n", o.Branch)
			}
		}
		fmt.Println()
	}

	fmt.Println("Cleanup complete!")
	return nil
}

// runRebaseWithDrops performs a rebase that drops specific commits
func runRebaseWithDrops(_ git.GitOperations, baseBranch string, dropHashes []string) error {
	// Build sed command to replace pick with drop for our commits
	var patterns []string
	for _, hash := range dropHashes {
		patterns = append(patterns, fmt.Sprintf("s/^pick %s/drop %s/", hash, hash))
	}
	sedScript := strings.Join(patterns, "; ")

	// Use GIT_SEQUENCE_EDITOR environment variable to automate the rebase
	// The sed command changes "pick <hash>" to "drop <hash>" for dangling commits
	sequenceEditor := fmt.Sprintf(`sed -i "" "%s"`, sedScript)

	cmd := exec.Command("git", "rebase", "-i", baseBranch)
	cmd.Env = append(os.Environ(), "GIT_SEQUENCE_EDITOR="+sequenceEditor)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
