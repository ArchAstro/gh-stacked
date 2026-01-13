// internal/stack/stack.go
package stack

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/calvin/gh-stacked/internal/git"
)

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)
var stackBranchRe = regexp.MustCompile(`^stack/.+/(\d{2})-(.+)$`)

// ExistingBranchInfo holds info about an existing remote stack branch
type ExistingBranchInfo struct {
	Branch   string
	Position int
	Slug     string
}

// ParseExistingBranches parses remote branches to extract position and slug
func ParseExistingBranches(branches []string, parentBranch string) []ExistingBranchInfo {
	var result []ExistingBranchInfo
	prefix := "stack/" + parentBranch + "/"

	for _, branch := range branches {
		if !strings.HasPrefix(branch, prefix) {
			continue
		}
		matches := stackBranchRe.FindStringSubmatch(branch)
		if matches == nil {
			continue
		}
		pos, _ := strconv.Atoi(matches[1])
		result = append(result, ExistingBranchInfo{
			Branch:   branch,
			Position: pos,
			Slug:     matches[2],
		})
	}
	return result
}

// MatchCommitToExistingBranch finds existing branch for a commit by slug match
func MatchCommitToExistingBranch(commitMsg string, existing []ExistingBranchInfo) *ExistingBranchInfo {
	slug := Slugify(commitMsg, 5)
	for i := range existing {
		if existing[i].Slug == slug {
			return &existing[i]
		}
	}
	return nil
}

// MatchCommitToExistingBranchExcluding finds existing branch by slug, excluding claimed ones
func MatchCommitToExistingBranchExcluding(commitMsg string, existing []ExistingBranchInfo, claimed map[int]bool) *ExistingBranchInfo {
	slug := Slugify(commitMsg, 5)
	for i := range existing {
		if claimed[existing[i].Position] {
			continue
		}
		if existing[i].Slug == slug {
			return &existing[i]
		}
	}
	return nil
}

// FindMaxPosition returns the highest position number from existing branches
func FindMaxPosition(existing []ExistingBranchInfo) int {
	maxPos := 0
	for _, e := range existing {
		if e.Position > maxPos {
			maxPos = e.Position
		}
	}
	return maxPos
}

// FindMinPosition returns the lowest position number from existing branches
func FindMinPosition(existing []ExistingBranchInfo) int {
	if len(existing) == 0 {
		return 0
	}
	minPos := existing[0].Position
	for _, e := range existing {
		if e.Position < minPos {
			minPos = e.Position
		}
	}
	return minPos
}

func Slugify(s string, maxWords int) string {
	s = strings.ToLower(s)
	s = nonAlphaNum.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)

	words := strings.Fields(s)
	if len(words) > maxWords {
		words = words[:maxWords]
	}

	return strings.Join(words, "-")
}

func StackBranchName(parentBranch string, position int, commitMsg string) string {
	slug := Slugify(commitMsg, 5)
	return fmt.Sprintf("stack/%s/%02d-%s", parentBranch, position, slug)
}

var stackBranchPattern = regexp.MustCompile(`^stack/(.+)/(\d{2})-[a-z0-9-]+$`)

func ParseStackBranch(branch string) (parentBranch string, position int, ok bool) {
	matches := stackBranchPattern.FindStringSubmatch(branch)
	if matches == nil {
		return "", 0, false
	}

	pos, err := strconv.Atoi(matches[2])
	if err != nil {
		return "", 0, false
	}

	return matches[1], pos, true
}

func IsStackBranch(branch string) bool {
	_, _, ok := ParseStackBranch(branch)
	return ok
}

type PREntry struct {
	Position      int
	CommitHash    string
	CommitMsg     string
	CommitBody    string
	Branch        string
	PRNumber      int
	PRURL         string
	CurrentBase   string // Current base branch of existing PR (for retargeting detection)
	Mergeable     bool
	ChecksPassing bool
	Approvals     int
	ChangesReq    bool
	Additions     int
	Deletions     int
}

// GeneratePRTitle creates the PR title with [X/N] prefix
func GeneratePRTitle(commitMsg string, position, total int) string {
	return fmt.Sprintf("[%d/%d] %s", position, total, commitMsg)
}

// GeneratePRBody creates the full PR body with description and stack footer
func GeneratePRBody(commitMsg string, commitBody string, stackID string, entries []PREntry, currentPosition int) string {
	var body strings.Builder

	// Header with commit message
	body.WriteString(fmt.Sprintf("## %s\n\n", commitMsg))

	// Include commit body if present, otherwise add placeholder
	if commitBody != "" {
		body.WriteString(commitBody)
		body.WriteString("\n")
	} else {
		body.WriteString("<!-- Add description here -->\n")
	}

	// Stack footer
	body.WriteString(generateStackFooter(stackID, entries, currentPosition))

	return body.String()
}

// GenerateStackFooter generates just the footer (for backwards compatibility)
func GenerateStackFooter(stackID string, entries []PREntry, currentPosition int) string {
	return generateStackFooter(stackID, entries, currentPosition)
}

func generateStackFooter(stackID string, entries []PREntry, currentIndex int) string {
	total := len(entries)
	var prev, next string

	// currentIndex is 1-based, entries array is 0-based
	// Previous entry is at entries[currentIndex-2] (if currentIndex > 1)
	// Next entry is at entries[currentIndex] (if currentIndex < total)
	if currentIndex > 1 {
		e := entries[currentIndex-2]
		if e.PRNumber > 0 {
			prev = fmt.Sprintf("[#%d](%s)", e.PRNumber, e.PRURL)
		}
	}
	if currentIndex < total {
		e := entries[currentIndex]
		if e.PRNumber > 0 {
			next = fmt.Sprintf("[#%d](%s)", e.PRNumber, e.PRURL)
		}
	}

	parts := []string{fmt.Sprintf("`%s` (%d/%d)", stackID, currentIndex, total)}
	if prev != "" {
		parts = append(parts, "← "+prev)
	}
	if next != "" {
		parts = append(parts, "→ "+next)
	}

	return fmt.Sprintf("\n---\n📚 **Stack:** %s", strings.Join(parts, " | "))
}

func StripStackFooter(body string) string {
	// Remove existing stack footer if present
	if idx := strings.LastIndex(body, "\n---\n📚 **Stack:**"); idx != -1 {
		return strings.TrimSpace(body[:idx])
	}
	// Also handle old format without bold
	if idx := strings.LastIndex(body, "\n---\n📚 Stack:"); idx != -1 {
		return strings.TrimSpace(body[:idx])
	}
	return body
}

// titlePrefixRe matches the [X/N] prefix pattern in PR titles
var titlePrefixRe = regexp.MustCompile(`^\[(\d+)/(\d+)\]\s*`)

// UpdatePRTitle updates the [X/N] prefix in a PR title while preserving user edits
// If the title has our [X/N] prefix, update it. Otherwise, leave title unchanged.
func UpdatePRTitle(currentTitle string, newPosition, newTotal int) (string, bool) {
	matches := titlePrefixRe.FindStringSubmatch(currentTitle)
	if matches == nil {
		// Title doesn't have our prefix - user customized it, don't touch
		return currentTitle, false
	}

	// Extract the description part (after the prefix)
	description := titlePrefixRe.ReplaceAllString(currentTitle, "")

	// Check if position/total actually changed
	oldPos, _ := strconv.Atoi(matches[1])
	oldTotal, _ := strconv.Atoi(matches[2])
	if oldPos == newPosition && oldTotal == newTotal {
		return currentTitle, false
	}

	// Build new title with updated prefix
	newTitle := fmt.Sprintf("[%d/%d] %s", newPosition, newTotal, description)
	return newTitle, true
}

// UpdatePRBody preserves user content in PR body while updating the stack footer
func UpdatePRBody(currentBody string, stackID string, entries []PREntry, currentPosition int) string {
	// Strip old footer, preserve everything else
	userContent := StripStackFooter(currentBody)

	// Append new footer
	return userContent + generateStackFooter(stackID, entries, currentPosition)
}

// CommitInfo is the minimal commit data needed for position matching
type CommitInfo struct {
	Hash    string
	Subject string
	Body    string
}

// GitCommitsToInfos converts git.Commit slice to CommitInfo slice
func GitCommitsToInfos(commits []git.Commit) []CommitInfo {
	result := make([]CommitInfo, len(commits))
	for i, c := range commits {
		result[i] = CommitInfo{Hash: c.Hash, Subject: c.Subject, Body: c.Body}
	}
	return result
}

// CommitPosition represents a commit matched to its stack position
type CommitPosition struct {
	CommitHash string
	CommitMsg  string
	CommitBody string
	Position   int
	Branch     string
	IsNew      bool // True if this is a new commit without existing branch
}

// MatchCommitsToPositions matches commits to their stack positions using remote branches as source of truth
// Returns positions in the same order as the input commits
func MatchCommitsToPositions(commits []CommitInfo, existingBranches []ExistingBranchInfo, parentBranch string) []CommitPosition {
	result := make([]CommitPosition, 0, len(commits))

	claimedBranches := make(map[int]bool)
	nextPosition := FindMaxPosition(existingBranches) + 1
	if nextPosition == 1 {
		nextPosition = 1
	}

	for _, commit := range commits {
		var position int
		var branchName string
		var isNew bool

		// Match commit to existing branch by slug
		existing := MatchCommitToExistingBranchExcluding(commit.Subject, existingBranches, claimedBranches)
		if existing != nil {
			claimedBranches[existing.Position] = true
			position = existing.Position
			branchName = existing.Branch
			isNew = false
		} else {
			// New commit - assign next available position
			position = nextPosition
			branchName = StackBranchName(parentBranch, position, commit.Subject)
			nextPosition++
			isNew = true
		}

		result = append(result, CommitPosition{
			CommitHash: commit.Hash,
			CommitMsg:  commit.Subject,
			CommitBody: commit.Body,
			Position:   position,
			Branch:     branchName,
			IsNew:      isNew,
		})
	}

	return result
}
