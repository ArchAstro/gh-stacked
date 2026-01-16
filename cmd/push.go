// cmd/push.go - replace entire file
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/github"
	"github.com/calvin/gh-stacked/internal/stack"
	"github.com/spf13/cobra"
)

// pushMode represents the execution mode for the push command
type pushMode int

const (
	pushModeNormal pushMode = iota
	pushModePreview
	pushModeDraft
)

// String returns the string representation of pushMode
func (m pushMode) String() string {
	switch m {
	case pushModePreview:
		return "preview"
	case pushModeDraft:
		return "draft"
	default:
		return ""
	}
}

// parsePushMode converts a string to pushMode, returning an error for invalid values
func parsePushMode(s string) (pushMode, error) {
	switch s {
	case "", "normal":
		return pushModeNormal, nil
	case "preview":
		return pushModePreview, nil
	case "draft":
		return pushModeDraft, nil
	default:
		return pushModeNormal, fmt.Errorf("invalid mode %q: must be 'preview' or 'draft'", s)
	}
}

var (
	pushModeStr string // raw flag value, validated in runPush
	pushRegroup bool
	pushSquash  []string
)

// Dependency injection points for testing
var (
	newGit    = func() git.GitOperations { return git.New() }
	newGitHub = func() (github.GitHubOperations, error) { return github.NewClient() }
)

type PushOutput struct {
	Action        string `json:"action"`
	StackID       string `json:"stack_id"`
	PRsCreated    []int  `json:"prs_created"`
	PRsUpdated    []int  `json:"prs_updated"`
	PRsRetargeted []int  `json:"prs_retargeted"`
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
  gh stacked push --mode=preview

  # Create PRs as drafts (not ready for review)
  gh stacked push --mode=draft

  # Squash commits 2-4 into one PR before pushing
  gh stacked push --squash 2-4

  # Interactive reorganization
  gh stacked push --regroup`,
	RunE: runPush,
}

func init() {
	rootCmd.AddCommand(pushCmd)

	pushCmd.Flags().StringVar(&pushModeStr, "mode", "", "Push mode: 'preview' (dry run) or 'draft' (create draft PRs)")
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

// existingBranchInfo holds info about an existing remote stack branch
type existingBranchInfo struct {
	branch   string
	position int
	slug     string
}

// parseExistingBranches parses remote branches to extract position and slug
func parseExistingBranches(branches []string, parentBranch string) []existingBranchInfo {
	var result []existingBranchInfo
	prefix := "stack/" + parentBranch + "/"

	// Pattern: stack/{parent}/{NN}-{slug}
	re := regexp.MustCompile(`^stack/.+/(\d{2})-(.+)$`)

	for _, branch := range branches {
		if !strings.HasPrefix(branch, prefix) {
			continue
		}
		matches := re.FindStringSubmatch(branch)
		if matches == nil {
			continue
		}
		pos, _ := strconv.Atoi(matches[1])
		result = append(result, existingBranchInfo{
			branch:   branch,
			position: pos,
			slug:     matches[2],
		})
	}
	return result
}

// matchCommitToExistingBranch finds existing branch for a commit by slug match
func matchCommitToExistingBranch(commitMsg string, existing []existingBranchInfo) *existingBranchInfo {
	slug := stack.Slugify(commitMsg, 5)
	for i := range existing {
		if existing[i].slug == slug {
			return &existing[i]
		}
	}
	return nil
}

// matchCommitToExistingBranchExcluding finds existing branch by slug, excluding claimed ones
func matchCommitToExistingBranchExcluding(commitMsg string, existing []existingBranchInfo, claimed map[int]bool) *existingBranchInfo {
	slug := stack.Slugify(commitMsg, 5)
	for i := range existing {
		// Skip branches that have already been claimed by previous commits
		if claimed[existing[i].position] {
			continue
		}
		if existing[i].slug == slug {
			return &existing[i]
		}
	}
	return nil
}

// findMaxPosition returns the highest position number from existing branches
func findMaxPosition(existing []existingBranchInfo) int {
	maxPos := 0
	for _, e := range existing {
		if e.position > maxPos {
			maxPos = e.position
		}
	}
	return maxPos
}

func runPush(cmd *cobra.Command, args []string) error {
	// Parse and validate mode flag
	mode, err := parsePushMode(pushModeStr)
	if err != nil {
		return err
	}

	g := newGit()

	// Get current branch
	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if stack.IsStackBranch(currentBranch) {
		return fmt.Errorf("run push from your feature branch, not a stack branch")
	}

	// Check sync status with remote - fetch and detect merged commits
	if mode != pushModePreview {
		syncChecker := git.NewSyncChecker(g)
		if !jsonOutput {
			fmt.Println("Checking sync status...")
		}
		syncStatus, err := syncChecker.EnsureSynced(false) // Don't auto-rebase, just check
		if err != nil {
			// Non-fatal - continue if fetch fails
			if !jsonOutput {
				fmt.Printf("Warning: could not check sync status: %v\n", err)
			}
		} else if syncStatus.NeedsRebase {
			return fmt.Errorf("branch is out of sync with %s\n\n%s\nAfter syncing, run 'gh stacked push' again",
				syncStatus.BaseBranch, syncStatus.FormatSyncMessage())
		}
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

	// Handle --regroup: interactive rebase before pushing
	if pushRegroup {
		fmt.Printf("Opening interactive rebase for %d commits...\n", len(commits))
		fmt.Println("Reorder, squash, or edit commits as needed. Save and close to continue.")
		fmt.Println()

		err := g.InteractiveRebase(baseBranch)
		if err != nil {
			return fmt.Errorf("interactive rebase failed: %w\n\nIf there were conflicts, resolve them and run: gh stacked push", err)
		}

		// Re-read commits after rebase
		commits, err = g.GetCommitsSinceBase(baseBranch)
		if err != nil {
			return fmt.Errorf("failed to get commits after rebase: %w", err)
		}

		if len(commits) == 0 {
			fmt.Println("No commits remaining after rebase")
			return nil
		}

		fmt.Printf("\nRebase complete. Proceeding with %d commits.\n\n", len(commits))
	}

	// Get GitHub client
	gh, err := newGitHub()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Fetch existing remote branches for this stack
	// This is the SOURCE OF TRUTH for position numbers
	remoteBranches, err := g.ListRemoteBranches("origin/stack/" + currentBranch + "/*")
	if err != nil {
		// Non-fatal - might be first push
		remoteBranches = nil
	}
	allExistingBranches := parseExistingBranches(remoteBranches, currentBranch)

	// Filter to only branches that have open PRs
	// This excludes orphaned branches from merged/closed PRs
	var existingBranches []existingBranchInfo
	branchToPR := make(map[string]github.PullRequest)
	for _, eb := range allExistingBranches {
		prs, err := gh.ListPullRequests(eb.branch)
		if err == nil && len(prs) > 0 {
			existingBranches = append(existingBranches, eb)
			branchToPR[eb.branch] = prs[0]
		}
	}

	// Build entries by matching commits to existing branches
	// Key insight: use slug matching, not position, to find existing branches
	// New commits get assigned to "holes" in the position sequence

	// First pass: match all commits to existing branches and record positions
	type commitMatchInfo struct {
		commit   git.Commit
		matched  bool
		existing *existingBranchInfo
		prInfo   *github.PullRequest
	}

	matchInfos := make([]commitMatchInfo, len(commits))
	usedPositions := make(map[int]bool)
	claimedBranches := make(map[int]bool)

	for i, commit := range commits {
		existing := matchCommitToExistingBranchExcluding(commit.Subject, existingBranches, claimedBranches)
		if existing != nil {
			claimedBranches[existing.position] = true
			usedPositions[existing.position] = true
			if pr, ok := branchToPR[existing.branch]; ok {
				matchInfos[i] = commitMatchInfo{
					commit:   commit,
					matched:  true,
					existing: existing,
					prInfo:   &pr,
				}
			} else {
				matchInfos[i] = commitMatchInfo{
					commit:   commit,
					matched:  true,
					existing: existing,
				}
			}
		} else {
			matchInfos[i] = commitMatchInfo{
				commit:  commit,
				matched: false,
			}
		}
	}

	// Second pass: assign positions to unmatched commits by finding holes
	var entries []stack.PREntry
	existingPRs := make(map[int]int) // position -> PR number (for squash validation)

	for i, info := range matchInfos {
		var entry stack.PREntry

		if info.matched {
			entry = stack.PREntry{
				Position:   info.existing.position,
				CommitHash: info.commit.Hash,
				CommitMsg:  info.commit.Subject,
				CommitBody: info.commit.Body,
				Branch:     info.existing.branch,
			}
			if info.prInfo != nil {
				entry.PRNumber = info.prInfo.Number
				entry.PRURL = info.prInfo.URL
				entry.CurrentBase = info.prInfo.Base.Ref
				existingPRs[info.existing.position] = info.prInfo.Number
			}
		} else {
			// Find position bounds for this new commit
			// prevPos = position of previous entry (0 if first)
			prevPos := 0
			if i > 0 {
				prevPos = entries[i-1].Position
			}

			// nextPos = position of next matched commit (or 100 as practical max)
			nextPos := 100
			for j := i + 1; j < len(matchInfos); j++ {
				if matchInfos[j].matched {
					nextPos = matchInfos[j].existing.position
					break
				}
			}

			// Find first available position in (prevPos, nextPos)
			foundPos := 0
			for p := prevPos + 1; p < nextPos; p++ {
				if !usedPositions[p] {
					foundPos = p
					break
				}
			}

			if foundPos == 0 {
				return fmt.Errorf("no available position for commit %q between positions %d and %d\nThis can happen when commits are reordered. Consider closing some PRs to free up positions",
					info.commit.Subject, prevPos, nextPos)
			}

			usedPositions[foundPos] = true
			branchName := stack.StackBranchName(currentBranch, foundPos, info.commit.Subject)
			entry = stack.PREntry{
				Position:   foundPos,
				CommitHash: info.commit.Hash,
				CommitMsg:  info.commit.Subject,
				CommitBody: info.commit.Body,
				Branch:     branchName,
			}
		}

		entries = append(entries, entry)
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

	if mode == pushModePreview {
		if jsonOutput {
			out := struct {
				StackID  string          `json:"stack_id"`
				Base     string          `json:"base_branch"`
				Commits  []stack.PREntry `json:"commits"`
				Existing map[int]int     `json:"existing_prs"`
			}{
				StackID:  currentBranch,
				Base:     baseBranch,
				Commits:  entries,
				Existing: existingPRs,
			}
			return json.NewEncoder(os.Stdout).Encode(out)
		}

		// Table format matching housekeeping
		fmt.Printf("Stack: %s\n", currentBranch)
		fmt.Printf("Base: %s\n\n", baseBranch)

		// Print table header
		fmt.Println("  #   Action       Branch    PR       Commit/Branch")
		fmt.Println("  ─── ──────────── ──────── ──────── ─────────────────────────────────────────")

		for i, e := range entries {
			stackIndex := i + 1 // 1-based index

			// Determine expected base for this PR
			expectedBase := strings.TrimPrefix(baseBranch, "origin/")
			if i > 0 {
				expectedBase = entries[i-1].Branch
			}

			var actionIcon string
			var prStr string
			if e.PRNumber > 0 {
				actionIcon = "↑ update"
				// OSC 8 hyperlink: \e]8;;URL\e\\TEXT\e]8;;\e\\
				// Add padding AFTER escape sequence since Printf counts escape bytes
				prText := fmt.Sprintf("#%d", e.PRNumber)
				prLink := fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", e.PRURL, prText)
				prStr = prLink + strings.Repeat(" ", max(0, 6-len(prText)))
				// Check if PR will need retargeting
				if e.CurrentBase != "" && e.CurrentBase != expectedBase {
					actionIcon = "↻ retarget"
				}
			} else {
				actionIcon = "✚ create"
				prStr = fmt.Sprintf("%-6s", "-")
			}

			posStr := fmt.Sprintf("%d", e.Position)

			descStr := fmt.Sprintf("%s %s", e.CommitHash[:7], e.CommitMsg)
			if len(descStr) > 45 {
				descStr = descStr[:42] + "..."
			}

			fmt.Printf("  %-3d %-12s %-8s %s %s\n", stackIndex, actionIcon, posStr, prStr, descStr)
		}

		// Summary
		createCount := 0
		updateCount := 0
		for _, e := range entries {
			if e.PRNumber > 0 {
				updateCount++
			} else {
				createCount++
			}
		}

		fmt.Println()
		fmt.Println("Summary:")
		fmt.Printf("  Total: %d\n", len(entries))
		if createCount > 0 {
			fmt.Printf("  To create: %d\n", createCount)
		}
		if updateCount > 0 {
			fmt.Printf("  To update: %d\n", updateCount)
		}

		fmt.Println()
		fmt.Println("Run 'gh stacked push' to create PRs, or 'gh stacked push --mode=draft' to create as drafts.")

		return nil
	}

	// Actually push
	repo := gh.Repository()
	_ = repo // repo available for future use
	output := PushOutput{
		StackID:       currentBranch,
		PRsCreated:    []int{},
		PRsUpdated:    []int{},
		PRsRetargeted: []int{},
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

		// Determine base for this PR (strip origin/ prefix for GitHub API)
		prBase := strings.TrimPrefix(baseBranch, "origin/")
		if i > 0 {
			prBase = entries[i-1].Branch
		}

		total := len(entries)
		stackIndex := i + 1 // 1-based index for display (stack order, not branch position)
		prTitle := stack.GeneratePRTitle(entry.CommitMsg, stackIndex, total)

		if entry.PRNumber > 0 {
			// Existing PR - branch was already pushed above
			// Check if PR needs retargeting (base changed due to new PR inserted before it)
			if entry.CurrentBase != "" && entry.CurrentBase != prBase {
				err = gh.UpdatePullRequestBase(entry.PRNumber, prBase)
				if err != nil {
					return fmt.Errorf("failed to retarget PR #%d to %s: %w", entry.PRNumber, prBase, err)
				}
				output.PRsRetargeted = append(output.PRsRetargeted, entry.PRNumber)
				if !jsonOutput {
					fmt.Printf("Retargeted PR #%d: %s → %s\n", entry.PRNumber, entry.CurrentBase, prBase)
				}
			}

			output.PRsUpdated = append(output.PRsUpdated, entry.PRNumber)
			if !jsonOutput {
				fmt.Printf("Pushed PR #%d: %s\n", entry.PRNumber, entry.CommitMsg)
			}
		} else {
			// Create new PR
			prBody := stack.GeneratePRBody(entry.CommitMsg, entry.CommitBody, currentBranch, entries, stackIndex)
			pr, err := gh.CreatePullRequest(github.CreatePullRequestInput{
				Title: prTitle,
				Body:  prBody,
				Head:  entry.Branch,
				Base:  prBase,
				Draft: mode == pushModeDraft,
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

	// Second pass: Update PR metadata (title prefix and stack footer)
	// Smart update: preserve user edits, only update our managed parts
	if !jsonOutput {
		fmt.Println("\nUpdating PR metadata...")
	}
	for i, entry := range entries {
		if entry.PRNumber > 0 {
			// Fetch current PR to check existing title/body
			currentPR, err := gh.GetPullRequest(entry.PRNumber)
			if err != nil {
				if !jsonOutput {
					fmt.Printf("Warning: failed to fetch PR #%d: %v\n", entry.PRNumber, err)
				}
				continue
			}

			stackIndex := i + 1 // 1-based index for display (stack order, not branch position)

			// Update title (only changes [X/N] prefix, preserves rest)
			newTitle, titleChanged := stack.UpdatePRTitle(currentPR.Title, stackIndex, len(entries))

			// Update body (preserves user content, updates stack footer)
			newBody := stack.UpdatePRBody(currentPR.Body, currentBranch, entries, stackIndex)
			bodyChanged := newBody != currentPR.Body

			// Only call API if something actually changed
			if titleChanged || bodyChanged {
				err = gh.UpdatePullRequest(entry.PRNumber, newTitle, newBody)
				if err != nil {
					if !jsonOutput {
						fmt.Printf("Warning: failed to update PR #%d metadata: %v\n", entry.PRNumber, err)
					}
				}
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

	summary := fmt.Sprintf("\nStack pushed: %d created, %d updated", len(output.PRsCreated), len(output.PRsUpdated))
	if len(output.PRsRetargeted) > 0 {
		summary += fmt.Sprintf(", %d retargeted", len(output.PRsRetargeted))
	}
	fmt.Println(summary)

	return nil
}

// contains checks if a slice contains a value
func contains(slice []int, val int) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
