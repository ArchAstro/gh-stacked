// internal/stack/stack_test.go
package stack

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Add User model", "add-user-model"},
		{"Add User model validation", "add-user-model-validation"},
		{"Fix: bug in API endpoint", "fix-bug-in-api-endpoint"},
		{"Add something with way too many words in the title", "add-something-with-way-too"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := Slugify(tt.input, 5)
			if result != tt.expected {
				t.Errorf("Slugify(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestStackBranchName(t *testing.T) {
	tests := []struct {
		parentBranch string
		position     int
		commitMsg    string
		expected     string
	}{
		{"feature/auth", 1, "Add User model", "stack/feature/auth/01-add-user-model"},
		{"feature/auth", 2, "Add User API", "stack/feature/auth/02-add-user-api"},
		{"main-work", 10, "Fix bug", "stack/main-work/10-fix-bug"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := StackBranchName(tt.parentBranch, tt.position, tt.commitMsg)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseStackBranch(t *testing.T) {
	tests := []struct {
		branch       string
		wantParent   string
		wantPosition int
		wantOk       bool
	}{
		{"stack/feature/auth/01-add-user-model", "feature/auth", 1, true},
		{"stack/feature/auth/02-add-api", "feature/auth", 2, true},
		{"stack/main-work/10-fix-bug", "main-work", 10, true},
		{"feature/auth", "", 0, false},
		{"not-a-stack-branch", "", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			parent, pos, ok := ParseStackBranch(tt.branch)
			if ok != tt.wantOk {
				t.Errorf("ok = %v, want %v", ok, tt.wantOk)
			}
			if ok {
				if parent != tt.wantParent {
					t.Errorf("parent = %q, want %q", parent, tt.wantParent)
				}
				if pos != tt.wantPosition {
					t.Errorf("position = %d, want %d", pos, tt.wantPosition)
				}
			}
		})
	}
}

func TestGenerateStackFooter(t *testing.T) {
	entries := []PREntry{
		{Position: 1, PRNumber: 42, PRURL: "https://github.com/org/repo/pull/42"},
		{Position: 2, PRNumber: 43, PRURL: "https://github.com/org/repo/pull/43"},
		{Position: 3, PRNumber: 44, PRURL: "https://github.com/org/repo/pull/44"},
	}

	footer := GenerateStackFooter("feature/auth", entries, 2)

	if !strings.Contains(footer, "feature/auth") {
		t.Error("footer should contain stack ID")
	}
	if !strings.Contains(footer, "(2/3)") {
		t.Error("footer should contain position")
	}
	if !strings.Contains(footer, "#42") {
		t.Error("footer should reference previous PR")
	}
	if !strings.Contains(footer, "#44") {
		t.Error("footer should reference next PR")
	}
}

func TestGeneratePRTitle(t *testing.T) {
	tests := []struct {
		commitMsg string
		position  int
		total     int
		expected  string
	}{
		{"Add user model", 1, 3, "[1/3] Add user model"},
		{"Fix bug", 2, 5, "[2/5] Fix bug"},
		{"Update API", 10, 10, "[10/10] Update API"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := GeneratePRTitle(tt.commitMsg, tt.position, tt.total)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGeneratePRBody(t *testing.T) {
	entries := []PREntry{
		{Position: 1, PRNumber: 42, PRURL: "https://github.com/org/repo/pull/42"},
		{Position: 2, PRNumber: 43, PRURL: "https://github.com/org/repo/pull/43"},
		{Position: 3, PRNumber: 44, PRURL: "https://github.com/org/repo/pull/44"},
	}

	body := GeneratePRBody("Add user model", "", "feature/auth", entries, 1)

	// Should have header
	if !strings.Contains(body, "## Add user model") {
		t.Error("body should contain commit message as header")
	}

	// Should have description placeholder (since no commit body provided)
	if !strings.Contains(body, "<!-- Add description here -->") {
		t.Error("body should contain description placeholder when no commit body")
	}

	// Should have stack footer
	if !strings.Contains(body, "📚 **Stack:**") {
		t.Error("body should contain stack footer")
	}

	// Should have stack info
	if !strings.Contains(body, "feature/auth") {
		t.Error("body should contain stack ID")
	}
}

func TestGeneratePRBody_WithCommitBody(t *testing.T) {
	entries := []PREntry{
		{Position: 1, PRNumber: 42, PRURL: "https://github.com/org/repo/pull/42"},
	}

	commitBody := "This is a detailed description of the changes.\n\nIt includes multiple paragraphs."
	body := GeneratePRBody("Add user model", commitBody, "feature/auth", entries, 1)

	// Should have header
	if !strings.Contains(body, "## Add user model") {
		t.Error("body should contain commit message as header")
	}

	// Should have commit body
	if !strings.Contains(body, "This is a detailed description") {
		t.Error("body should contain commit body")
	}

	// Should NOT have placeholder since commit body was provided
	if strings.Contains(body, "<!-- Add description here -->") {
		t.Error("body should not contain placeholder when commit body is provided")
	}

	// Should have stack footer
	if !strings.Contains(body, "📚 **Stack:**") {
		t.Error("body should contain stack footer")
	}
}

func TestGeneratePRBody_FirstPR(t *testing.T) {
	entries := []PREntry{
		{Position: 1, PRNumber: 0},  // This PR not yet created
		{Position: 2, PRNumber: 43, PRURL: "https://github.com/org/repo/pull/43"},
	}

	body := GeneratePRBody("Add user model", "", "feature/auth", entries, 1)

	// First PR should link to next (if it exists)
	if !strings.Contains(body, "#43") {
		t.Error("body should reference next PR")
	}
}

func TestGeneratePRBody_LastPR(t *testing.T) {
	entries := []PREntry{
		{Position: 1, PRNumber: 42, PRURL: "https://github.com/org/repo/pull/42"},
		{Position: 2, PRNumber: 0},  // This PR not yet created
	}

	body := GeneratePRBody("Add API", "", "feature/auth", entries, 2)

	// Last PR should link to previous
	if !strings.Contains(body, "#42") {
		t.Error("body should reference previous PR")
	}
}

func TestStripStackFooter(t *testing.T) {
	body := "## My PR\n\nSome description\n\n---\n📚 Stack: `feature/auth` (1/3)"

	stripped := StripStackFooter(body)

	if strings.Contains(stripped, "Stack:") {
		t.Error("stripped body should not contain footer")
	}
	if !strings.Contains(stripped, "My PR") {
		t.Error("stripped body should keep header")
	}
	if !strings.Contains(stripped, "Some description") {
		t.Error("stripped body should keep description")
	}
}
