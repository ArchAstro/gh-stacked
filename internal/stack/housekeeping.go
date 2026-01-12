// internal/stack/housekeeping.go
package stack

import (
	"sort"

	"github.com/calvin/gh-stacked/internal/git"
)

// StackEntry represents a commit or PR in the stack analysis
type StackEntry struct {
	Position    int
	CommitHash  string // Local commit hash (empty if no local match)
	CommitMsg   string // Local commit message
	Branch      string // Remote branch name
	PRNumber    int    // PR number (0 if no PR)
	PRURL       string
	PRState     string // "open", "merged", "closed"
	Status      string // "matched", "local_only", "remote_only", "orphaned"
	CommitIndex int    // Index in local commit list (for sorting)
}

// PRInfo contains PR details from GitHub
type PRInfo struct {
	Number int
	URL    string
	State  string
	Base   string
}

// AnalyzeStack compares local commits with remote branches and their PRs
// Returns entries sorted by commit order (local commits first, then remote-only)
func AnalyzeStack(
	commits []git.Commit,
	remoteBranches []string,
	parentBranch string,
	getPRForBranch func(branch string) (*PRInfo, error),
) []StackEntry {
	// Parse remote branches
	existingBranches := ParseExistingBranches(remoteBranches, parentBranch)

	// Build map of branch -> PR info
	branchPRs := make(map[string]*PRInfo)
	for _, eb := range existingBranches {
		pr, err := getPRForBranch(eb.Branch)
		if err == nil && pr != nil {
			branchPRs[eb.Branch] = pr
		}
	}

	// Track which branches have been matched
	matchedBranches := make(map[string]bool)
	var entries []StackEntry

	// First pass: match local commits to remote branches with open PRs
	for i, commit := range commits {
		slug := Slugify(commit.Subject, 5)

		var matchedEntry *StackEntry

		// Find matching branch by slug (only consider branches with open PRs)
		for _, eb := range existingBranches {
			if matchedBranches[eb.Branch] {
				continue
			}
			if eb.Slug == slug {
				pr := branchPRs[eb.Branch]
				if pr != nil && pr.State == "open" {
					matchedBranches[eb.Branch] = true
					matchedEntry = &StackEntry{
						Position:    eb.Position,
						CommitHash:  commit.Hash,
						CommitMsg:   commit.Subject,
						Branch:      eb.Branch,
						PRNumber:    pr.Number,
						PRURL:       pr.URL,
						PRState:     pr.State,
						Status:      "matched",
						CommitIndex: i,
					}
					break
				}
			}
		}

		if matchedEntry != nil {
			entries = append(entries, *matchedEntry)
		} else {
			// Local commit with no matching open PR
			entries = append(entries, StackEntry{
				Position:    0, // Will be assigned later
				CommitHash:  commit.Hash,
				CommitMsg:   commit.Subject,
				Status:      "local_only",
				CommitIndex: i,
			})
		}
	}

	// Second pass: find remote branches with open PRs that don't match any local commit
	for _, eb := range existingBranches {
		if matchedBranches[eb.Branch] {
			continue
		}
		pr := branchPRs[eb.Branch]
		if pr != nil && pr.State == "open" {
			entries = append(entries, StackEntry{
				Position:    eb.Position,
				Branch:      eb.Branch,
				PRNumber:    pr.Number,
				PRURL:       pr.URL,
				PRState:     pr.State,
				Status:      "remote_only",
				CommitIndex: len(commits) + eb.Position, // Sort after local commits
			})
		}
	}

	// Third pass: find orphaned branches (have branches but PRs are merged/closed)
	for _, eb := range existingBranches {
		if matchedBranches[eb.Branch] {
			continue
		}
		pr := branchPRs[eb.Branch]
		if pr != nil && pr.State != "open" {
			entries = append(entries, StackEntry{
				Position:    eb.Position,
				Branch:      eb.Branch,
				PRNumber:    pr.Number,
				PRURL:       pr.URL,
				PRState:     pr.State,
				Status:      "orphaned",
				CommitIndex: len(commits) + 1000 + eb.Position, // Sort at end
			})
		} else if pr == nil {
			// Branch exists but no PR at all
			entries = append(entries, StackEntry{
				Position:    eb.Position,
				Branch:      eb.Branch,
				Status:      "orphaned",
				CommitIndex: len(commits) + 1000 + eb.Position,
			})
		}
	}

	// Sort by commit index (preserves chronological order)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CommitIndex < entries[j].CommitIndex
	})

	return entries
}

// StackAnalysisSummary provides counts for the analysis
type StackAnalysisSummary struct {
	TotalCommits   int
	TotalOpenPRs   int
	Matched        int
	LocalOnly      int
	RemoteOnly     int
	Orphaned       int
}

// SummarizeAnalysis returns counts from the analysis
func SummarizeAnalysis(entries []StackEntry) StackAnalysisSummary {
	var summary StackAnalysisSummary
	for _, e := range entries {
		switch e.Status {
		case "matched":
			summary.Matched++
			summary.TotalCommits++
			summary.TotalOpenPRs++
		case "local_only":
			summary.LocalOnly++
			summary.TotalCommits++
		case "remote_only":
			summary.RemoteOnly++
			summary.TotalOpenPRs++
		case "orphaned":
			summary.Orphaned++
		}
	}
	return summary
}
