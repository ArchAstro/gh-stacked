// internal/github/github_test.go
package github

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func newRESTContractClient(t *testing.T, handler roundTripFunc) *Client {
	t.Helper()
	rest, err := api.NewRESTClient(api.ClientOptions{
		Host:      "github.com",
		AuthToken: "test-token",
		Transport: handler,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Client{rest: rest, repo: Repository{Owner: "acme", Name: "widgets"}}
}

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
		Title: "Add User model",
		Body:  "Description here",
		Head:  "stack/feature/01-add-user",
		Base:  "main",
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

func TestGetPullRequestDecodesHeadRepository(t *testing.T) {
	client := newRESTContractClient(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/repos/acme/widgets/pulls/42" {
			t.Fatalf("unexpected API path: %s", request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"number": 42,
				"state": "open",
				"head": {"ref": "stack/feature/x/01-one", "repo": {"full_name": "acme/widgets"}},
				"base": {"ref": "main"}
			}`)),
		}, nil
	})

	pullRequest, err := client.GetPullRequest(42)
	if err != nil {
		t.Fatal(err)
	}
	if pullRequest.HeadRepository != "acme/widgets" {
		t.Fatalf("head repository = %q", pullRequest.HeadRepository)
	}
}

func TestListPullRequestsURLQueryPreservesLegalRefCharacters(t *testing.T) {
	const branch = "stack/feature/a&b%25/01-one"
	client := newRESTContractClient(t, func(request *http.Request) (*http.Response, error) {
		if got := request.URL.Query().Get("head"); got != "acme:"+branch {
			t.Fatalf("head query = %q, want %q", got, "acme:"+branch)
		}
		if got := request.URL.Query().Get("state"); got != "open" {
			t.Fatalf("state query = %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`[]`)),
		}, nil
	})

	if _, err := client.ListPullRequests(branch); err != nil {
		t.Fatal(err)
	}
}

func TestGetRepositoryPermissionUsesCollaboratorPermissionEndpoint(t *testing.T) {
	client := newRESTContractClient(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/repos/acme/widgets/collaborators/octocat/permission" {
			t.Fatalf("unexpected API path: %s", request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"permission":"maintain"}`)),
		}, nil
	})

	permission, err := client.GetRepositoryPermission("octocat")
	if err != nil {
		t.Fatal(err)
	}
	if permission != "maintain" {
		t.Fatalf("permission = %q", permission)
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
			url:       "git@github.com:acme/widgets.git",
			wantOwner: "acme",
			wantRepo:  "widgets",
		},
		{
			name:      "SSH with alias",
			url:       "git@github.com-work:acme/widgets.git",
			wantOwner: "acme",
			wantRepo:  "widgets",
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
			url:       "https://github.com/ArchAstro/gh-stacked.git",
			wantOwner: "ArchAstro",
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
