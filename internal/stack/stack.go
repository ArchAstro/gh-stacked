// internal/stack/stack.go
package stack

import (
	"fmt"
	"regexp"
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
