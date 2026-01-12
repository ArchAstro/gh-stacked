// internal/stack/stack.go
package stack

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

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
	Branch        string
	PRNumber      int
	PRURL         string
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
func GeneratePRBody(commitMsg string, stackID string, entries []PREntry, currentPosition int) string {
	var body strings.Builder

	// Header with commit message
	body.WriteString(fmt.Sprintf("## %s\n\n", commitMsg))

	// Placeholder for description
	body.WriteString("<!-- Add description here -->\n")

	// Stack footer
	body.WriteString(generateStackFooter(stackID, entries, currentPosition))

	return body.String()
}

// GenerateStackFooter generates just the footer (for backwards compatibility)
func GenerateStackFooter(stackID string, entries []PREntry, currentPosition int) string {
	return generateStackFooter(stackID, entries, currentPosition)
}

func generateStackFooter(stackID string, entries []PREntry, currentPosition int) string {
	total := len(entries)
	var prev, next string

	for _, e := range entries {
		if e.Position == currentPosition-1 && e.PRNumber > 0 {
			prev = fmt.Sprintf("[#%d](%s)", e.PRNumber, e.PRURL)
		}
		if e.Position == currentPosition+1 && e.PRNumber > 0 {
			next = fmt.Sprintf("[#%d](%s)", e.PRNumber, e.PRURL)
		}
	}

	parts := []string{fmt.Sprintf("`%s` (%d/%d)", stackID, currentPosition, total)}
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
	if idx := strings.LastIndex(body, "\n---\n📚 Stack:"); idx != -1 {
		return strings.TrimSpace(body[:idx])
	}
	return body
}
