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

func GenerateStackFooter(stackID string, entries []PREntry, currentPosition int) string {
	total := len(entries)
	var prev, next string

	for _, e := range entries {
		if e.Position == currentPosition-1 {
			prev = fmt.Sprintf("[#%d](%s)", e.PRNumber, e.PRURL)
		}
		if e.Position == currentPosition+1 {
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

	return fmt.Sprintf("\n---\n📚 Stack: %s", strings.Join(parts, " | "))
}

func StripStackFooter(body string) string {
	// Remove existing stack footer if present
	if idx := strings.LastIndex(body, "\n---\n📚 Stack:"); idx != -1 {
		return strings.TrimSpace(body[:idx])
	}
	return body
}
