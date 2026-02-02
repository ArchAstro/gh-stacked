// internal/github/github_test.go
package github

import (
	"errors"
	"testing"
)

func TestNewClient(t *testing.T) {
	// This test verifies the client can be created
	// Actual API calls would require mocking
	c, err := NewClient()
	if err != nil {
		t.Skipf("Skipping test - gh auth not configured: %v", err)
	}
	if c == nil {
		t.Error("expected non-nil client")
	}
}

func TestCreatePullRequestInput(t *testing.T) {
	// Test the PR input struct construction (no actual API call)
	input := CreatePullRequestInput{
		Title:  "Add User model",
		Body:   "Description here",
		Head:   "stack/feature/01-add-user",
		Base:   "main",
	}

	if input.Title != "Add User model" {
		t.Errorf("unexpected title: %s", input.Title)
	}
}

func TestPullRequestStatus(t *testing.T) {
	// Test status struct
	status := PullRequestStatus{
		Number:           42,
		Title:            "Add feature",
		URL:              "https://github.com/org/repo/pull/42",
		Mergeable:        true,
		ChecksPassing:    true,
		Approvals:        2,
		ChangesRequested: false,
	}

	if !status.Mergeable {
		t.Error("expected mergeable")
	}
}

func TestParseGitRemoteURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "SSH standard",
			url:       "git@github.com:ArchAstro/firstlanding.git",
			wantOwner: "ArchAstro",
			wantRepo:  "firstlanding",
		},
		{
			name:      "SSH with alias",
			url:       "git@github.com-aa:ArchAstro/firstlanding.git",
			wantOwner: "ArchAstro",
			wantRepo:  "firstlanding",
		},
		{
			name:      "SSH without .git suffix",
			url:       "git@github.com:owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "SSH with complex alias",
			url:       "git@github-work:myorg/myproject.git",
			wantOwner: "myorg",
			wantRepo:  "myproject",
		},
		{
			name:      "HTTPS standard",
			url:       "https://github.com/calvin/gh-stacked.git",
			wantOwner: "calvin",
			wantRepo:  "gh-stacked",
		},
		{
			name:      "HTTPS without .git suffix",
			url:       "https://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "HTTPS GitHub Enterprise",
			url:       "https://github.mycompany.com/team/project.git",
			wantOwner: "team",
			wantRepo:  "project",
		},
		{
			name:    "Invalid URL",
			url:     "not-a-valid-url",
			wantErr: true,
		},
		{
			name:    "Empty URL",
			url:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := parseRemoteURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestMockGitHub_AddLabelToIssue(t *testing.T) {
	mock := NewMockGitHub()

	err := mock.AddLabelToIssue(42, "stack/last")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.AddedLabels) != 1 {
		t.Fatalf("expected 1 added label, got %d", len(mock.AddedLabels))
	}
	if mock.AddedLabels[0].Number != 42 {
		t.Errorf("expected PR #42, got #%d", mock.AddedLabels[0].Number)
	}
	if mock.AddedLabels[0].Label != "stack/last" {
		t.Errorf("expected label 'stack/last', got %q", mock.AddedLabels[0].Label)
	}

	// Verify call was recorded
	found := false
	for _, call := range mock.Calls {
		if call.Method == "AddLabelToIssue" {
			found = true
			if call.Args[0].(int) != 42 {
				t.Errorf("expected call arg PR #42, got #%d", call.Args[0].(int))
			}
			if call.Args[1].(string) != "stack/last" {
				t.Errorf("expected call arg 'stack/last', got %q", call.Args[1].(string))
			}
		}
	}
	if !found {
		t.Error("AddLabelToIssue call not recorded")
	}
}

func TestMockGitHub_RemoveLabelFromIssue(t *testing.T) {
	mock := NewMockGitHub()

	err := mock.RemoveLabelFromIssue(42, "stack/last")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.RemovedLabels) != 1 {
		t.Fatalf("expected 1 removed label, got %d", len(mock.RemovedLabels))
	}
	if mock.RemovedLabels[0].Number != 42 {
		t.Errorf("expected PR #42, got #%d", mock.RemovedLabels[0].Number)
	}
	if mock.RemovedLabels[0].Label != "stack/last" {
		t.Errorf("expected label 'stack/last', got %q", mock.RemovedLabels[0].Label)
	}
}

func TestMockGitHub_LabelErrorInjection(t *testing.T) {
	mock := NewMockGitHub()
	mock.Errors["AddLabelToIssue"] = errors.New("API rate limited")

	err := mock.AddLabelToIssue(42, "stack/last")
	if err == nil {
		t.Fatal("expected error from injected error")
	}
	if err.Error() != "API rate limited" {
		t.Errorf("expected 'API rate limited', got %q", err.Error())
	}

	// Should not have recorded in AddedLabels since it errored
	if len(mock.AddedLabels) != 0 {
		t.Errorf("expected 0 added labels on error, got %d", len(mock.AddedLabels))
	}
}
