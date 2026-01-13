// cmd/amend_test.go
package cmd

import (
	"errors"
	"testing"

	"github.com/calvin/gh-stacked/internal/git"
)

func TestAmendCommand(t *testing.T) {
	cmd := amendCmd

	if cmd.Use != "amend <position>" {
		t.Errorf("expected Use 'amend <position>', got %s", cmd.Use)
	}

	// Verify the command takes exactly 1 argument
	if cmd.Args == nil {
		t.Error("expected Args validator")
	}
}

// TestRunAmend_Success tests amending PR 2 with staged changes triggers fixup + rebase + push
func TestRunAmend_Success(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.StagedChanges = true
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
		{Hash: "ghi9012", Subject: "Add user tests"},
	}

	oldNewGit := amendNewGit
	defer func() { amendNewGit = oldNewGit }()
	amendNewGit = func() git.GitOperations { return mockGit }

	err := runAmend(nil, []string{"2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify fixup commit was created for commit 2
	if !mockGit.WasCalled("CreateFixupCommit") {
		t.Error("expected CreateFixupCommit to be called")
	}

	// Verify the fixup was created for the correct commit hash
	for _, call := range mockGit.Calls {
		if call.Method == "CreateFixupCommit" {
			if len(call.Args) > 0 && call.Args[0] != "def5678" {
				t.Errorf("expected fixup for commit def5678, got %v", call.Args[0])
			}
		}
	}

	// Verify autosquash rebase was called
	if !mockGit.WasCalled("AutosquashRebase") {
		t.Error("expected AutosquashRebase to be called")
	}

	// Verify branches were created and pushed for affected commits (2 and 3)
	createBranchCalls := 0
	pushBranchCalls := 0
	for _, call := range mockGit.Calls {
		if call.Method == "CreateBranchAtCommit" {
			createBranchCalls++
		}
		if call.Method == "PushBranch" {
			pushBranchCalls++
		}
	}

	// Should update branches from position 2 onwards (2 branches: PR2 and PR3)
	if createBranchCalls != 2 {
		t.Errorf("expected 2 CreateBranchAtCommit calls, got %d", createBranchCalls)
	}
	if pushBranchCalls != 2 {
		t.Errorf("expected 2 PushBranch calls, got %d", pushBranchCalls)
	}
}

// TestRunAmend_FirstPR tests amending the first PR
func TestRunAmend_FirstPR(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.StagedChanges = true
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
	}

	oldNewGit := amendNewGit
	defer func() { amendNewGit = oldNewGit }()
	amendNewGit = func() git.GitOperations { return mockGit }

	err := runAmend(nil, []string{"1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify fixup commit was created for commit 1
	for _, call := range mockGit.Calls {
		if call.Method == "CreateFixupCommit" {
			if len(call.Args) > 0 && call.Args[0] != "abc1234" {
				t.Errorf("expected fixup for commit abc1234, got %v", call.Args[0])
			}
		}
	}

	// Should update all branches from position 1 onwards (both PRs)
	pushBranchCalls := 0
	for _, call := range mockGit.Calls {
		if call.Method == "PushBranch" {
			pushBranchCalls++
		}
	}
	if pushBranchCalls != 2 {
		t.Errorf("expected 2 PushBranch calls (all PRs affected), got %d", pushBranchCalls)
	}
}

// TestRunAmend_NoStagedChanges tests error when no staged changes
func TestRunAmend_NoStagedChanges(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.StagedChanges = false
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	oldNewGit := amendNewGit
	defer func() { amendNewGit = oldNewGit }()
	amendNewGit = func() git.GitOperations { return mockGit }

	err := runAmend(nil, []string{"1"})
	if err == nil {
		t.Fatal("expected error for no staged changes")
	}

	expectedError := "no staged changes. Stage files first with `git add`"
	if err.Error() != expectedError {
		t.Errorf("expected error %q, got %q", expectedError, err.Error())
	}
}

// TestRunAmend_InvalidPRNumber tests error when PR number is out of range
func TestRunAmend_InvalidPRNumber(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.StagedChanges = true
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	oldNewGit := amendNewGit
	defer func() { amendNewGit = oldNewGit }()
	amendNewGit = func() git.GitOperations { return mockGit }

	err := runAmend(nil, []string{"5"}) // Only 1 commit exists
	if err == nil {
		t.Fatal("expected error for invalid PR number")
	}

	// Should indicate stack position out of range
	expectedSubstring := "stack position 5 is out of range"
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
	if !containsSubstring(err.Error(), expectedSubstring) {
		t.Errorf("expected error containing %q, got %q", expectedSubstring, err.Error())
	}
}

// TestRunAmend_PRNumberZero tests error when PR number is 0
func TestRunAmend_PRNumberZero(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.StagedChanges = true
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	oldNewGit := amendNewGit
	defer func() { amendNewGit = oldNewGit }()
	amendNewGit = func() git.GitOperations { return mockGit }

	err := runAmend(nil, []string{"0"})
	if err == nil {
		t.Fatal("expected error for PR number 0")
	}
}

// TestRunAmend_PRNumberNegative tests error when PR number is negative
func TestRunAmend_PRNumberNegative(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.StagedChanges = true
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	oldNewGit := amendNewGit
	defer func() { amendNewGit = oldNewGit }()
	amendNewGit = func() git.GitOperations { return mockGit }

	err := runAmend(nil, []string{"-1"})
	if err == nil {
		t.Fatal("expected error for negative PR number")
	}
}

// TestRunAmend_NonNumericArg tests error when argument is not a number
func TestRunAmend_NonNumericArg(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.StagedChanges = true

	oldNewGit := amendNewGit
	defer func() { amendNewGit = oldNewGit }()
	amendNewGit = func() git.GitOperations { return mockGit }

	err := runAmend(nil, []string{"abc"})
	if err == nil {
		t.Fatal("expected error for non-numeric argument")
	}

	expectedSubstring := "invalid stack position"
	if !containsSubstring(err.Error(), expectedSubstring) {
		t.Errorf("expected error containing %q, got %q", expectedSubstring, err.Error())
	}
}

// TestRunAmend_RebaseConflict tests abort and error when rebase conflict occurs
func TestRunAmend_RebaseConflict(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.StagedChanges = true
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}
	mockGit.Errors["AutosquashRebase"] = errors.New("conflict")

	oldNewGit := amendNewGit
	defer func() { amendNewGit = oldNewGit }()
	amendNewGit = func() git.GitOperations { return mockGit }

	err := runAmend(nil, []string{"1"})
	if err == nil {
		t.Fatal("expected error for rebase conflict")
	}

	// Should have called AbortRebase to clean up
	if !mockGit.WasCalled("AbortRebase") {
		t.Error("expected AbortRebase to be called on conflict")
	}

	// Should have called Run to save HEAD before fixup (rev-parse HEAD)
	if !mockGit.WasCalled("Run") {
		t.Error("expected Run to be called to save original HEAD")
	}

	// Should have called ResetHard to restore original state (before fixup commit)
	if !mockGit.WasCalled("ResetHard") {
		t.Error("expected ResetHard to be called to restore state")
	}

	// Error message should mention conflict
	if !containsSubstring(err.Error(), "conflict") {
		t.Errorf("expected error message to mention conflict, got %q", err.Error())
	}
}

// TestRunAmend_RejectsStackBranch tests error when running from a stack branch
func TestRunAmend_RejectsStackBranch(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "stack/feature/auth/01-add-user-model"
	mockGit.StagedChanges = true

	oldNewGit := amendNewGit
	defer func() { amendNewGit = oldNewGit }()
	amendNewGit = func() git.GitOperations { return mockGit }

	err := runAmend(nil, []string{"1"})
	if err == nil {
		t.Fatal("expected error when running from stack branch")
	}

	expectedError := "run amend from your feature branch, not a stack branch"
	if err.Error() != expectedError {
		t.Errorf("expected error %q, got %q", expectedError, err.Error())
	}
}

// TestRunAmend_LastPR tests amending the last PR (only that branch needs updating)
func TestRunAmend_LastPR(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.StagedChanges = true
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
		{Hash: "ghi9012", Subject: "Add user tests"},
	}

	oldNewGit := amendNewGit
	defer func() { amendNewGit = oldNewGit }()
	amendNewGit = func() git.GitOperations { return mockGit }

	err := runAmend(nil, []string{"3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify fixup commit was created for commit 3
	for _, call := range mockGit.Calls {
		if call.Method == "CreateFixupCommit" {
			if len(call.Args) > 0 && call.Args[0] != "ghi9012" {
				t.Errorf("expected fixup for commit ghi9012, got %v", call.Args[0])
			}
		}
	}

	// Should only update 1 branch (the last PR)
	pushBranchCalls := 0
	for _, call := range mockGit.Calls {
		if call.Method == "PushBranch" {
			pushBranchCalls++
		}
	}
	if pushBranchCalls != 1 {
		t.Errorf("expected 1 PushBranch call (only last PR affected), got %d", pushBranchCalls)
	}
}

// TestRunAmend_ForcePush tests that branches are force-pushed
func TestRunAmend_ForcePush(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.StagedChanges = true
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}

	oldNewGit := amendNewGit
	defer func() { amendNewGit = oldNewGit }()
	amendNewGit = func() git.GitOperations { return mockGit }

	err := runAmend(nil, []string{"1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify push was called with force=true
	for _, call := range mockGit.Calls {
		if call.Method == "PushBranch" {
			if len(call.Args) >= 3 {
				force, ok := call.Args[2].(bool)
				if !ok || !force {
					t.Error("expected force push (force=true)")
				}
			}
		}
	}
}

// TestRunAmend_CreateFixupFails tests error handling when CreateFixupCommit fails
func TestRunAmend_CreateFixupFails(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.StagedChanges = true
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}
	mockGit.Errors["CreateFixupCommit"] = errors.New("commit failed")

	oldNewGit := amendNewGit
	defer func() { amendNewGit = oldNewGit }()
	amendNewGit = func() git.GitOperations { return mockGit }

	err := runAmend(nil, []string{"1"})
	if err == nil {
		t.Fatal("expected error when CreateFixupCommit fails")
	}

	if !containsSubstring(err.Error(), "fixup commit") {
		t.Errorf("expected error about fixup commit, got %q", err.Error())
	}
}

// TestRunAmend_PushFails tests error handling when PushBranch fails
func TestRunAmend_PushFails(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.StagedChanges = true
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
	}
	mockGit.Errors["PushBranch"] = errors.New("push rejected")

	oldNewGit := amendNewGit
	defer func() { amendNewGit = oldNewGit }()
	amendNewGit = func() git.GitOperations { return mockGit }

	err := runAmend(nil, []string{"1"})
	if err == nil {
		t.Fatal("expected error when PushBranch fails")
	}

	if !containsSubstring(err.Error(), "push") {
		t.Errorf("expected error about push, got %q", err.Error())
	}
}

// TestRunAmend_UsesExistingFixup tests that amend uses an existing fixup commit at the top
func TestRunAmend_UsesExistingFixup(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.StagedChanges = false // No staged changes - should still work with existing fixup
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
		{Hash: "fixup123", Subject: "fixup! Add user model"}, // Existing fixup at top
	}

	oldNewGit := amendNewGit
	defer func() { amendNewGit = oldNewGit }()
	amendNewGit = func() git.GitOperations { return mockGit }

	err := runAmend(nil, []string{"1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should NOT have called CreateFixupCommit (uses existing)
	if mockGit.WasCalled("CreateFixupCommit") {
		t.Error("should not create new fixup when one already exists")
	}

	// Should have called AutosquashRebase
	if !mockGit.WasCalled("AutosquashRebase") {
		t.Error("expected AutosquashRebase to be called")
	}
}

// TestRunAmend_ExistingFixupWrongTarget tests that wrong fixup doesn't match
func TestRunAmend_ExistingFixupWrongTarget(t *testing.T) {
	mockGit := git.NewMockGit()
	mockGit.CurrentBranch = "feature/auth"
	mockGit.BaseBranch = "main"
	mockGit.StagedChanges = false // No staged changes
	mockGit.Commits = []git.Commit{
		{Hash: "abc1234", Subject: "Add user model"},
		{Hash: "def5678", Subject: "Add user API"},
		{Hash: "fixup123", Subject: "fixup! Add user API"}, // Fixup for commit 2, not 1
	}

	oldNewGit := amendNewGit
	defer func() { amendNewGit = oldNewGit }()
	amendNewGit = func() git.GitOperations { return mockGit }

	// Try to amend commit 1, but fixup is for commit 2
	err := runAmend(nil, []string{"1"})
	if err == nil {
		t.Fatal("expected error when no staged changes and fixup is for wrong target")
	}

	expectedError := "no staged changes. Stage files first with `git add`"
	if err.Error() != expectedError {
		t.Errorf("expected error %q, got %q", expectedError, err.Error())
	}
}

// Helper function to check if a string contains a substring
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
