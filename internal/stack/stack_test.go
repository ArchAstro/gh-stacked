// internal/stack/stack_test.go
package stack

import (
	"strings"
	"testing"

	"github.com/ArchAstro/gh-stacked/internal/git"
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
		{"large-stack", 100, "Keep parsing", "stack/large-stack/100-keep-parsing"},
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
		{"stack/large-stack/100-keep-parsing", "large-stack", 100, true},
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
		{Position: 1, PRNumber: 0}, // This PR not yet created
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
		{Position: 2, PRNumber: 0}, // This PR not yet created
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

func TestGitCommitsToInfos(t *testing.T) {
	commits := []git.Commit{
		{Hash: "abc1234", Subject: "Add user model", Body: "Description of model"},
		{Hash: "def5678", Subject: "Add user API", Body: ""},
		{Hash: "ghi9012", Subject: "Add user tests", Body: "Test coverage details"},
	}

	result := GitCommitsToInfos(commits)

	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}

	// Verify all fields are copied correctly
	for i, r := range result {
		if r.Hash != commits[i].Hash {
			t.Errorf("commit %d: expected Hash %q, got %q", i, commits[i].Hash, r.Hash)
		}
		if r.Subject != commits[i].Subject {
			t.Errorf("commit %d: expected Subject %q, got %q", i, commits[i].Subject, r.Subject)
		}
		if r.Body != commits[i].Body {
			t.Errorf("commit %d: expected Body %q, got %q", i, commits[i].Body, r.Body)
		}
	}
}

func TestGitCommitsToInfos_Empty(t *testing.T) {
	commits := []git.Commit{}

	result := GitCommitsToInfos(commits)

	if len(result) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(result))
	}
}

func TestGitCommitsToInfos_Single(t *testing.T) {
	commits := []git.Commit{
		{Hash: "abc1234", Subject: "Single commit", Body: "Details here"},
	}

	result := GitCommitsToInfos(commits)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}

	if result[0].Hash != "abc1234" {
		t.Errorf("expected Hash abc1234, got %s", result[0].Hash)
	}
	if result[0].Subject != "Single commit" {
		t.Errorf("expected Subject 'Single commit', got %s", result[0].Subject)
	}
	if result[0].Body != "Details here" {
		t.Errorf("expected Body 'Details here', got %s", result[0].Body)
	}
}

func TestMatchCommitsToPositions_NoExistingBranches(t *testing.T) {
	commits := []CommitInfo{
		{"abc1234", "Add user model", ""},
		{"def5678", "Add user API", ""},
		{"ghi9012", "Add user tests", ""},
	}
	existingBranches := []ExistingBranchInfo{}

	result := MatchCommitsToPositions(commits, existingBranches, "feature/auth")

	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}

	// Should assign positions 1, 2, 3
	for i, r := range result {
		expectedPos := i + 1
		if r.Position != expectedPos {
			t.Errorf("commit %d: expected position %d, got %d", i, expectedPos, r.Position)
		}
		if !r.IsNew {
			t.Errorf("commit %d: expected IsNew=true", i)
		}
	}
}

func TestMatchCommitsToPositions_AllExisting(t *testing.T) {
	commits := []CommitInfo{
		{"abc1234", "Add user model", ""},
		{"def5678", "Add user API", ""},
	}
	existingBranches := []ExistingBranchInfo{
		{Branch: "stack/feature/auth/01-add-user-model", Position: 1, Slug: "add-user-model"},
		{Branch: "stack/feature/auth/02-add-user-api", Position: 2, Slug: "add-user-api"},
	}

	result := MatchCommitsToPositions(commits, existingBranches, "feature/auth")

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}

	// Should match to existing positions
	if result[0].Position != 1 {
		t.Errorf("commit 0: expected position 1, got %d", result[0].Position)
	}
	if result[0].IsNew {
		t.Errorf("commit 0: expected IsNew=false")
	}
	if result[1].Position != 2 {
		t.Errorf("commit 1: expected position 2, got %d", result[1].Position)
	}
}

func TestMatchCommitsToPositions_AfterMerge(t *testing.T) {
	// Simulates after PR 1 and 2 were merged - positions start at 3
	commits := []CommitInfo{
		{"abc1234", "Add user API", ""},
		{"def5678", "Add user tests", ""},
	}
	existingBranches := []ExistingBranchInfo{
		{Branch: "stack/feature/auth/03-add-user-api", Position: 3, Slug: "add-user-api"},
		{Branch: "stack/feature/auth/04-add-user-tests", Position: 4, Slug: "add-user-tests"},
	}

	result := MatchCommitsToPositions(commits, existingBranches, "feature/auth")

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}

	// Should match to positions 3, 4 (not 1, 2)
	if result[0].Position != 3 {
		t.Errorf("commit 0: expected position 3, got %d", result[0].Position)
	}
	if result[1].Position != 4 {
		t.Errorf("commit 1: expected position 4, got %d", result[1].Position)
	}
}

func TestMatchCommitsToPositions_NewCommitAfterExisting(t *testing.T) {
	// Existing stack has 3 commits, user adds a 4th
	commits := []CommitInfo{
		{"abc1234", "Add user model", ""},
		{"def5678", "Add user API", ""},
		{"ghi9012", "Add user tests", ""},
		{"jkl3456", "Add user docs", ""}, // New commit
	}
	existingBranches := []ExistingBranchInfo{
		{Branch: "stack/feature/auth/01-add-user-model", Position: 1, Slug: "add-user-model"},
		{Branch: "stack/feature/auth/02-add-user-api", Position: 2, Slug: "add-user-api"},
		{Branch: "stack/feature/auth/03-add-user-tests", Position: 3, Slug: "add-user-tests"},
	}

	result := MatchCommitsToPositions(commits, existingBranches, "feature/auth")

	if len(result) != 4 {
		t.Fatalf("expected 4 results, got %d", len(result))
	}

	// First 3 should match existing
	for i := 0; i < 3; i++ {
		if result[i].Position != i+1 {
			t.Errorf("commit %d: expected position %d, got %d", i, i+1, result[i].Position)
		}
		if result[i].IsNew {
			t.Errorf("commit %d: expected IsNew=false", i)
		}
	}

	// 4th should be new at position 4
	if result[3].Position != 4 {
		t.Errorf("commit 3: expected position 4, got %d", result[3].Position)
	}
	if !result[3].IsNew {
		t.Error("commit 3: expected IsNew=true")
	}
}

func TestMatchCommitsToPositions_SlugCollision(t *testing.T) {
	// Two commits with the same slug - should still get different positions
	commits := []CommitInfo{
		{"abc1234", "Add user model", ""},
		{"def5678", "Add user model", ""}, // Same message = same slug
	}
	existingBranches := []ExistingBranchInfo{
		{Branch: "stack/feature/auth/01-add-user-model", Position: 1, Slug: "add-user-model"},
	}

	result := MatchCommitsToPositions(commits, existingBranches, "feature/auth")

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}

	// First commit matches existing position 1
	if result[0].Position != 1 {
		t.Errorf("commit 0: expected position 1, got %d", result[0].Position)
	}
	if result[0].IsNew {
		t.Errorf("commit 0: expected IsNew=false")
	}

	// Second commit should get position 2 (claimed prevents double match)
	if result[1].Position != 2 {
		t.Errorf("commit 1: expected position 2, got %d", result[1].Position)
	}
	if !result[1].IsNew {
		t.Errorf("commit 1: expected IsNew=true")
	}
}

func TestMatchCommitsToPositions_GapInPositions(t *testing.T) {
	// Existing branches have a gap (position 2 was merged/deleted)
	commits := []CommitInfo{
		{"abc1234", "Add user model", ""},
		{"def5678", "Add user tests", ""},
	}
	existingBranches := []ExistingBranchInfo{
		{Branch: "stack/feature/auth/01-add-user-model", Position: 1, Slug: "add-user-model"},
		{Branch: "stack/feature/auth/03-add-user-tests", Position: 3, Slug: "add-user-tests"},
	}

	result := MatchCommitsToPositions(commits, existingBranches, "feature/auth")

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}

	// Should match to positions 1 and 3 (preserving gap)
	if result[0].Position != 1 {
		t.Errorf("commit 0: expected position 1, got %d", result[0].Position)
	}
	if result[1].Position != 3 {
		t.Errorf("commit 1: expected position 3, got %d", result[1].Position)
	}
}
