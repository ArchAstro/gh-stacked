// cmd/amend.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/stack"
	"github.com/spf13/cobra"
)

// Dependency injection points for testing
var amendNewGit = func() git.GitOperations { return git.New() }

type AmendOutput struct {
	Amended int      `json:"amended"`
	Commit  string   `json:"commit"`
	Rebased []int    `json:"rebased"`
	Pushed  []string `json:"pushed"`
}

var amendCmd = &cobra.Command{
	Use:   "amend <pr-number>",
	Short: "Amend a specific PR with staged changes",
	Long: `Amend a specific PR in the stack with your staged changes.

This command:
  1. Creates a fixup commit targeting the specified PR's commit
  2. Runs an autosquash rebase to fold it in
  3. Force-pushes all affected PR branches

You must stage your changes before running this command.`,
	Example: `  # Fix PR 2 after code review
  git add .
  gh stacked amend 2`,
	Args: cobra.ExactArgs(1),
	RunE: runAmend,
}

func init() {
	rootCmd.AddCommand(amendCmd)
}

func runAmend(cmd *cobra.Command, args []string) error {
	prPosition, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid PR number: %s", args[0])
	}

	g := amendNewGit()

	// Check for staged changes
	hasStaged, err := g.HasStagedChanges()
	if err != nil {
		return fmt.Errorf("failed to check staged changes: %w", err)
	}
	if !hasStaged {
		return fmt.Errorf("no staged changes. Stage files first with `git add`")
	}

	// Get current branch
	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if stack.IsStackBranch(currentBranch) {
		return fmt.Errorf("run amend from your feature branch, not a stack branch")
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

	targetCommit := commits[prPosition-1]

	// Save original HEAD for recovery
	origHead, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to save original state: %w", err)
	}
	_ = origHead // We'll use ORIG_HEAD from git

	// Create fixup commit
	err = g.CreateFixupCommit(targetCommit.Hash)
	if err != nil {
		return fmt.Errorf("failed to create fixup commit: %w", err)
	}

	// Run autosquash rebase
	err = g.AutosquashRebase(baseBranch)
	if err != nil {
		// Attempt to abort and restore
		g.AbortRebase()
		origRef, _ := g.GetOriginalHead()
		if origRef != "" {
			g.ResetHard(origRef)
		}

		return fmt.Errorf(`conflict rebasing "%s" onto %s
Restored original branch state.

To resolve manually:
  git rebase %s
  # fix conflicts
  git rebase --continue
  gh stacked push`, targetCommit.Subject, baseBranch, baseBranch)
	}

	// Get new commits after rebase
	newCommits, err := g.GetCommitsSinceBase(baseBranch)
	if err != nil {
		return fmt.Errorf("failed to get commits after rebase: %w", err)
	}

	// Push affected branches (from amended position onward)
	output := AmendOutput{
		Amended: prPosition,
		Commit:  newCommits[prPosition-1].Hash[:7],
		Rebased: []int{},
		Pushed:  []string{},
	}

	for i := prPosition - 1; i < len(newCommits); i++ {
		commit := newCommits[i]
		branchName := stack.StackBranchName(currentBranch, i+1, commit.Subject)

		err := g.CreateBranchAtCommit(branchName, commit.Hash)
		if err != nil {
			return fmt.Errorf("failed to update branch %s: %w", branchName, err)
		}

		err = g.PushBranch("origin", branchName, true)
		if err != nil {
			return fmt.Errorf("failed to push %s: %w", branchName, err)
		}

		if i+1 > prPosition {
			output.Rebased = append(output.Rebased, i+1)
		}
		output.Pushed = append(output.Pushed, branchName)

		if !jsonOutput {
			if i+1 == prPosition {
				fmt.Printf("Amended: %s\n", branchName)
			} else {
				fmt.Printf("Rebased: %s\n", branchName)
			}
		}
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	fmt.Printf("\nAmended PR %d, rebased %d commits\n", prPosition, len(output.Rebased))

	return nil
}
