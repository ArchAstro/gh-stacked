// internal/stack/stack_test.go
package stack

import "testing"

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
