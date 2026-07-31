package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ArchAstro/gh-stacked/internal/git"
	"github.com/ArchAstro/gh-stacked/internal/github"
	"github.com/ArchAstro/gh-stacked/internal/stack"
	"github.com/spf13/cobra"
)

var (
	rebasePR                    int
	rebaseActor                 string
	rebaseDryRun                bool
	rebaseRejectWorkflowChanges bool
)

var (
	rebaseNewGit    = func() git.GitOperations { return git.New() }
	rebaseNewGitHub = func() (github.GitHubOperations, error) { return github.NewClient() }
)

type RebaseOutput struct {
	Action   string   `json:"action"`
	StackID  string   `json:"stack_id"`
	Base     string   `json:"base_branch"`
	PRs      []int    `json:"pull_requests"`
	Branches []string `json:"branches"`
}

var rebaseCmd = &cobra.Command{
	Use:   "rebase",
	Short: "Rebase an entire remote PR stack onto its base branch",
	Long: `Rebase an entire remote PR stack as one guarded operation.

The command discovers the stack containing --pr, validates its pull request
base chain, rebases the cumulative tip in a temporary worktree, and publishes
all rewritten stack refs with one atomic force-with-lease push. The caller's
current branch and worktree are not changed.`,
	Example: `  # Rebase the stack containing PR #123
  gh stacked rebase --pr 123

  # Preflight without updating remote refs
  gh stacked rebase --pr 123 --dry-run

  # Verify a GitHub comment actor has write access before rebasing
  gh stacked rebase --pr 123 --actor octocat`,
	Args: cobra.NoArgs,
	RunE: runRebase,
}

func init() {
	rootCmd.AddCommand(rebaseCmd)
	rebaseCmd.Flags().IntVar(&rebasePR, "pr", 0, "Pull request number belonging to the stack")
	rebaseCmd.Flags().StringVar(&rebaseActor, "actor", "", "Require this GitHub user to have repository write access")
	rebaseCmd.Flags().BoolVar(&rebaseDryRun, "dry-run", false, "Preflight the complete rebase without pushing refs")
	rebaseCmd.Flags().BoolVar(&rebaseRejectWorkflowChanges, "reject-workflow-changes", false, "Reject stacks that modify .github/workflows before publishing refs")
}

func hasStackWritePermission(permission string) bool {
	switch permission {
	case "admin", "maintain", "write":
		return true
	default:
		return false
	}
}

func authorizeRebaseActor(gh github.GitHubOperations, actor string) error {
	if actor == "" {
		return nil
	}
	permission, err := gh.GetRepositoryPermission(actor)
	if err != nil {
		return fmt.Errorf("check repository permission for %s: %w", actor, err)
	}
	if !hasStackWritePermission(permission) {
		return fmt.Errorf("GitHub user %s does not have repository write permission", actor)
	}
	return nil
}

func discoverRemoteRebasePlan(
	gh github.GitHubOperations,
	g git.GitOperations,
	requestedPR int,
	stackID string,
) (*stack.RemoteRebasePlan, string, error) {
	repo := gh.Repository()
	expectedRepository := repo.Owner + "/" + repo.Name
	remoteBranches, err := g.ListRemoteBranches("origin/stack/" + stackID + "/*")
	if err != nil {
		return nil, "", fmt.Errorf("list remote stack branches: %w", err)
	}
	existing := stack.ParseExistingBranches(remoteBranches, stackID)
	sort.Slice(existing, func(i, j int) bool { return existing[i].Position < existing[j].Position })

	remotePRs := make([]stack.RemotePullRequest, 0, len(existing))
	for _, branch := range existing {
		prs, err := gh.ListPullRequests(branch.Branch)
		if err != nil {
			return nil, "", fmt.Errorf("list pull requests for %s: %w", branch.Branch, err)
		}
		if len(prs) == 0 {
			continue
		}
		if len(prs) != 1 {
			return nil, "", fmt.Errorf("stack branch %s has %d open pull requests", branch.Branch, len(prs))
		}
		headRepository := prs[0].Head.Repo.FullName
		if headRepository == "" {
			return nil, "", fmt.Errorf("PR #%d head repository is unavailable; refusing remote ref update", prs[0].Number)
		}
		if !strings.EqualFold(headRepository, expectedRepository) {
			return nil, "", fmt.Errorf("PR #%d comes from fork %q; remote stack rebase requires every branch to be in %s", prs[0].Number, headRepository, expectedRepository)
		}
		remotePRs = append(remotePRs, stack.RemotePullRequest{
			Number: prs[0].Number,
			Branch: prs[0].Head.Ref,
			Base:   prs[0].Base.Ref,
		})
	}
	if len(remotePRs) == 0 {
		return nil, "", fmt.Errorf("no open pull requests found in stack %q", stackID)
	}

	sort.Slice(remotePRs, func(i, j int) bool {
		_, left, _ := stack.ParseStackBranch(remotePRs[i].Branch)
		_, right, _ := stack.ParseStackBranch(remotePRs[j].Branch)
		return left < right
	})
	baseBranch := remotePRs[0].Base
	if _, _, isStackBranch := stack.ParseStackBranch(baseBranch); isStackBranch {
		return nil, "", fmt.Errorf("root PR #%d is still based on missing stack branch %q", remotePRs[0].Number, baseBranch)
	}
	if _, err := g.Run("check-ref-format", "--branch", baseBranch); err != nil {
		return nil, "", fmt.Errorf("invalid root PR base branch %q: %w", baseBranch, err)
	}
	baseRef := "origin/" + baseBranch
	if _, err := g.Run("rev-parse", "--verify", baseRef); err != nil {
		return nil, "", fmt.Errorf("resolve root PR base %s: %w", baseRef, err)
	}

	plan, err := stack.BuildRemoteRebasePlan(requestedPR, baseBranch, remotePRs)
	if err != nil {
		return nil, "", fmt.Errorf("validate stack: %w", err)
	}
	return plan, baseRef, nil
}

func sameRemoteRebasePlan(left, right *stack.RemoteRebasePlan) bool {
	if left.StackID != right.StackID || left.BaseBranch != right.BaseBranch || len(left.Entries) != len(right.Entries) {
		return false
	}
	for i := range left.Entries {
		if left.Entries[i] != right.Entries[i] {
			return false
		}
	}
	return true
}

func writeRebaseOutput(output RebaseOutput) error {
	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(output)
	}
	verb := "Rebased"
	if output.Action == "preview" {
		verb = "Validated"
	}
	fmt.Printf("%s stack %s: %d PRs onto %s\n", verb, output.StackID, len(output.PRs), output.Base)
	return nil
}

func rebasedStackChangesWorkflows(g git.GitOperations, commits []git.Commit) (bool, error) {
	for _, commit := range commits {
		workflowPaths, err := g.Run(
			"diff-tree", "--no-commit-id", "--name-only", "-r",
			commit.Hash, "--", ".github/workflows",
		)
		if err != nil {
			return false, fmt.Errorf("inspect rebased commit %s for workflow changes: %w", commit.Hash, err)
		}
		if strings.TrimSpace(workflowPaths) != "" {
			return true, nil
		}
	}
	return false, nil
}

func runRebase(cmd *cobra.Command, args []string) error {
	if rebasePR <= 0 {
		return fmt.Errorf("--pr must be a positive pull request number")
	}

	gh, err := rebaseNewGitHub()
	if err != nil {
		return fmt.Errorf("create GitHub client: %w", err)
	}
	if err := authorizeRebaseActor(gh, rebaseActor); err != nil {
		return err
	}

	requested, err := gh.GetPullRequest(rebasePR)
	if err != nil {
		return fmt.Errorf("get PR #%d: %w", rebasePR, err)
	}
	if requested.State != "open" {
		return fmt.Errorf("PR #%d is not open", rebasePR)
	}
	stackID, _, ok := stack.ParseStackBranch(requested.Branch)
	if !ok {
		return fmt.Errorf("PR #%d head %q is not a gh-stacked branch", rebasePR, requested.Branch)
	}
	repo := gh.Repository()
	expectedRepository := repo.Owner + "/" + repo.Name
	if requested.HeadRepository == "" {
		return fmt.Errorf("PR #%d head repository is unavailable; refusing remote ref update", rebasePR)
	}
	if !strings.EqualFold(requested.HeadRepository, expectedRepository) {
		return fmt.Errorf("PR #%d comes from fork %q; remote stack rebase requires a branch in %s", rebasePR, requested.HeadRepository, expectedRepository)
	}

	g := rebaseNewGit()
	const maxBaseRaceAttempts = 3
	for attempt := 1; attempt <= maxBaseRaceAttempts; attempt++ {
		if _, err := g.Run("fetch", "origin", "--prune"); err != nil {
			return fmt.Errorf("fetch origin: %w", err)
		}
		plan, baseRef, err := discoverRemoteRebasePlan(gh, g, rebasePR, stackID)
		if err != nil {
			return err
		}
		branches := make([]string, len(plan.Entries))
		for i, entry := range plan.Entries {
			branches[i] = entry.Branch
		}

		result, err := g.PrepareStackRebase(baseRef, branches)
		if err != nil {
			return fmt.Errorf("preflight stack rebase: %w", err)
		}
		if len(result.Commits) != len(plan.Entries) {
			return fmt.Errorf("preflight returned %d commits for %d pull requests", len(result.Commits), len(plan.Entries))
		}
		if rebaseRejectWorkflowChanges {
			changesWorkflows, err := rebasedStackChangesWorkflows(g, result.Commits)
			if err != nil {
				return err
			}
			if changesWorkflows {
				return fmt.Errorf("stack modifies .github/workflows and the configured publication token cannot update workflow files")
			}
		}

		// GitHub PR metadata and Git refs are separate systems. Re-read both
		// after the potentially long rebase and immediately before publication.
		if _, err := g.Run("fetch", "origin", "--prune"); err != nil {
			return fmt.Errorf("refresh origin before publication: %w", err)
		}
		latestPlan, latestBaseRef, err := discoverRemoteRebasePlan(gh, g, rebasePR, stackID)
		if err != nil {
			return fmt.Errorf("revalidate stack before publication: %w", err)
		}
		if latestBaseRef != baseRef || !sameRemoteRebasePlan(plan, latestPlan) {
			return fmt.Errorf("pull request stack changed during rebase; retry against the latest topology")
		}

		updates := make([]git.RefUpdate, len(plan.Entries))
		output := RebaseOutput{
			Action:   "rebased",
			StackID:  plan.StackID,
			Base:     plan.BaseBranch,
			PRs:      make([]int, len(plan.Entries)),
			Branches: branches,
		}
		if rebaseDryRun {
			output.Action = "preview"
		}
		for i, entry := range plan.Entries {
			oldOID := result.OldOIDs[entry.Branch]
			if oldOID == "" {
				return fmt.Errorf("preflight did not snapshot remote ref %s", entry.Branch)
			}
			updates[i] = git.RefUpdate{Branch: entry.Branch, OldOID: oldOID, NewOID: result.Commits[i].Hash}
			output.PRs[i] = entry.PRNumber
		}

		if rebaseDryRun {
			return writeRebaseOutput(output)
		}

		// A commenter's access can be revoked while a large stack rebases.
		// Recheck immediately before the write-capable operation.
		if err := authorizeRebaseActor(gh, rebaseActor); err != nil {
			return err
		}
		if err := g.AtomicPushStackRefs("origin", updates); err != nil {
			return err
		}

		// Git cannot compare-and-swap an unchanged base ref in the same push:
		// it omits up-to-date refs from receive-pack. Query the server after
		// publication and retry the complete coherent stack if the base moved
		// during that narrow window.
		remoteBaseOID, err := g.RemoteRefOID("origin", plan.BaseBranch)
		if err != nil {
			return fmt.Errorf("verify base branch after publication: %w", err)
		}
		if remoteBaseOID == result.BaseOID {
			return writeRebaseOutput(output)
		}
		if attempt == maxBaseRaceAttempts {
			return fmt.Errorf("base branch %s kept moving during rebase; stack refs remain internally consistent but may not include base %s; retry", plan.BaseBranch, remoteBaseOID)
		}
	}
	return fmt.Errorf("stack rebase exhausted retry attempts")
}
