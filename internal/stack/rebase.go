package stack

import (
	"fmt"
	"sort"
)

// RemotePullRequest is the minimal GitHub state needed to validate a remote
// stack before rewriting any refs.
type RemotePullRequest struct {
	Number int
	Branch string
	Base   string
}

// RemoteRebaseEntry is one open pull request in a validated stack.
type RemoteRebaseEntry struct {
	Position int
	PRNumber int
	Branch   string
	Base     string
}

// RemoteRebasePlan is an ordered, validated pull request chain.
type RemoteRebasePlan struct {
	StackID    string
	BaseBranch string
	Entries    []RemoteRebaseEntry
}

func (p RemoteRebasePlan) Tip() RemoteRebaseEntry {
	return p.Entries[len(p.Entries)-1]
}

// BuildRemoteRebasePlan validates that all supplied pull requests belong to
// one stack and form a single base-to-tip chain. Positions may contain gaps
// after earlier stack entries have been merged.
func BuildRemoteRebasePlan(requestedPR int, baseBranch string, prs []RemotePullRequest) (*RemoteRebasePlan, error) {
	if len(prs) == 0 {
		return nil, fmt.Errorf("no open pull requests found in stack")
	}

	entries := make([]RemoteRebaseEntry, 0, len(prs))
	stackID := ""
	requestedFound := false
	seenPositions := make(map[int]bool)

	for _, pr := range prs {
		parent, position, ok := ParseStackBranch(pr.Branch)
		if !ok {
			return nil, fmt.Errorf("PR #%d head %q is not a gh-stacked branch", pr.Number, pr.Branch)
		}
		if stackID == "" {
			stackID = parent
		} else if parent != stackID {
			return nil, fmt.Errorf("PR #%d belongs to different stack %q", pr.Number, parent)
		}
		if seenPositions[position] {
			return nil, fmt.Errorf("stack %q has duplicate position %d", stackID, position)
		}
		seenPositions[position] = true
		requestedFound = requestedFound || pr.Number == requestedPR
		entries = append(entries, RemoteRebaseEntry{
			Position: position,
			PRNumber: pr.Number,
			Branch:   pr.Branch,
			Base:     pr.Base,
		})
	}

	if !requestedFound {
		return nil, fmt.Errorf("PR #%d is not an open pull request in stack %q", requestedPR, stackID)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Position < entries[j].Position
	})

	expectedBase := baseBranch
	for _, entry := range entries {
		if entry.Base != expectedBase {
			return nil, fmt.Errorf("PR #%d has base %q; expected base %q", entry.PRNumber, entry.Base, expectedBase)
		}
		expectedBase = entry.Branch
	}

	return &RemoteRebasePlan{
		StackID:    stackID,
		BaseBranch: baseBranch,
		Entries:    entries,
	}, nil
}
