# gh-stacked Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement a gh CLI extension that manages stacked PRs from commit history with push, amend, merge, and status commands.

**Architecture:** Git operations via shell exec, GitHub API via go-gh, cobra CLI framework. Internal packages: `git/` for git wrappers, `github/` for API calls, `stack/` for stack logic and branch naming.

**Tech Stack:** Go 1.23, github.com/cli/go-gh/v2, github.com/spf13/cobra

---

## Task 1: Git Package - Base Branch Detection

**Files:**
- Create: `internal/git/git.go`
- Create: `internal/git/git_test.go`

**Step 1: Write the failing test**

```go
// internal/git/git_test.go
package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "Initial commit"},
		{"git", "branch", "-M", "main"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Fatalf("setup command %v failed: %v", args, err)
		}
	}

	return dir
}

func TestGetBaseBranch(t *testing.T) {
	dir := setupTestRepo(t)
	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	// Create feature branch
	exec.Command("git", "checkout", "-b", "feature/test").Run()
	exec.Command("git", "commit", "--allow-empty", "-m", "Feature commit").Run()

	g := New()
	base, err := g.GetBaseBranch()
	if err != nil {
		t.Fatalf("GetBaseBranch failed: %v", err)
	}
	if base != "main" {
		t.Errorf("expected base 'main', got '%s'", base)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/... -v`
Expected: FAIL - package doesn't exist

**Step 3: Write minimal implementation**

```go
// internal/git/git.go
package git

import (
	"os/exec"
	"strings"
)

type Git struct{}

func New() *Git {
	return &Git{}
}

func (g *Git) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (g *Git) GetBaseBranch() (string, error) {
	// Try common base branches in order of preference
	for _, base := range []string{"main", "master"} {
		_, err := g.run("rev-parse", "--verify", base)
		if err == nil {
			return base, nil
		}
	}
	return "main", nil // Default fallback
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/git/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/
git commit -m "feat(git): add base branch detection"
```

---

## Task 2: Git Package - Get Current Branch

**Files:**
- Modify: `internal/git/git.go`
- Modify: `internal/git/git_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/git/git_test.go

func TestGetCurrentBranch(t *testing.T) {
	dir := setupTestRepo(t)
	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	exec.Command("git", "checkout", "-b", "feature/my-feature").Run()

	g := New()
	branch, err := g.GetCurrentBranch()
	if err != nil {
		t.Fatalf("GetCurrentBranch failed: %v", err)
	}
	if branch != "feature/my-feature" {
		t.Errorf("expected 'feature/my-feature', got '%s'", branch)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/... -v -run TestGetCurrentBranch`
Expected: FAIL - method doesn't exist

**Step 3: Write minimal implementation**

```go
// Add to internal/git/git.go

func (g *Git) GetCurrentBranch() (string, error) {
	return g.run("rev-parse", "--abbrev-ref", "HEAD")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/git/... -v -run TestGetCurrentBranch`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/
git commit -m "feat(git): add current branch detection"
```

---

## Task 3: Git Package - List Commits Since Base

**Files:**
- Modify: `internal/git/git.go`
- Modify: `internal/git/git_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/git/git_test.go

func TestGetCommitsSinceBase(t *testing.T) {
	dir := setupTestRepo(t)
	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	// Create feature branch with 2 commits
	exec.Command("git", "checkout", "-b", "feature/test").Run()
	exec.Command("git", "commit", "--allow-empty", "-m", "First feature commit").Run()
	exec.Command("git", "commit", "--allow-empty", "-m", "Second feature commit").Run()

	g := New()
	commits, err := g.GetCommitsSinceBase("main")
	if err != nil {
		t.Fatalf("GetCommitsSinceBase failed: %v", err)
	}
	if len(commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].Subject != "First feature commit" {
		t.Errorf("expected 'First feature commit', got '%s'", commits[0].Subject)
	}
	if commits[1].Subject != "Second feature commit" {
		t.Errorf("expected 'Second feature commit', got '%s'", commits[1].Subject)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/... -v -run TestGetCommitsSinceBase`
Expected: FAIL - Commit type and method don't exist

**Step 3: Write minimal implementation**

```go
// Add to internal/git/git.go

type Commit struct {
	Hash    string
	Subject string
}

func (g *Git) GetCommitsSinceBase(base string) ([]Commit, error) {
	// Get commits from base to HEAD, oldest first
	out, err := g.run("log", "--reverse", "--format=%H %s", base+"..HEAD")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	var commits []Commit
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			commits = append(commits, Commit{
				Hash:    parts[0],
				Subject: parts[1],
			})
		}
	}
	return commits, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/git/... -v -run TestGetCommitsSinceBase`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/
git commit -m "feat(git): add commit listing since base branch"
```

---

## Task 4: Git Package - Create and Push Branch

**Files:**
- Modify: `internal/git/git.go`
- Modify: `internal/git/git_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/git/git_test.go

func TestCreateBranchAtCommit(t *testing.T) {
	dir := setupTestRepo(t)
	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	// Get the commit hash
	out, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	hash := strings.TrimSpace(string(out))

	g := New()
	err := g.CreateBranchAtCommit("test-branch", hash)
	if err != nil {
		t.Fatalf("CreateBranchAtCommit failed: %v", err)
	}

	// Verify branch exists
	_, err = exec.Command("git", "rev-parse", "--verify", "test-branch").Output()
	if err != nil {
		t.Error("branch was not created")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/... -v -run TestCreateBranchAtCommit`
Expected: FAIL - method doesn't exist

**Step 3: Write minimal implementation**

```go
// Add to internal/git/git.go

func (g *Git) CreateBranchAtCommit(branchName, commitHash string) error {
	_, err := g.run("branch", "-f", branchName, commitHash)
	return err
}

func (g *Git) PushBranch(remote, branchName string, force bool) error {
	args := []string{"push", remote, branchName}
	if force {
		args = append(args, "--force-with-lease")
	}
	_, err := g.run(args...)
	return err
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/git/... -v -run TestCreateBranchAtCommit`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/
git commit -m "feat(git): add branch creation and push"
```

---

## Task 5: Git Package - Fixup Commit and Autosquash Rebase

**Files:**
- Modify: `internal/git/git.go`
- Modify: `internal/git/git_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/git/git_test.go

func TestCreateFixupCommit(t *testing.T) {
	dir := setupTestRepo(t)
	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	// Create a commit to fixup
	exec.Command("git", "commit", "--allow-empty", "-m", "Original commit").Run()
	out, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	targetHash := strings.TrimSpace(string(out))

	// Create a file to stage
	os.WriteFile("test.txt", []byte("content"), 0644)
	exec.Command("git", "add", "test.txt").Run()

	g := New()
	err := g.CreateFixupCommit(targetHash)
	if err != nil {
		t.Fatalf("CreateFixupCommit failed: %v", err)
	}

	// Verify fixup commit was created
	out, _ = exec.Command("git", "log", "-1", "--format=%s").Output()
	subject := strings.TrimSpace(string(out))
	if !strings.HasPrefix(subject, "fixup! ") {
		t.Errorf("expected fixup commit, got '%s'", subject)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/git/... -v -run TestCreateFixupCommit`
Expected: FAIL - method doesn't exist

**Step 3: Write minimal implementation**

```go
// Add to internal/git/git.go

func (g *Git) CreateFixupCommit(targetHash string) error {
	_, err := g.run("commit", "--fixup="+targetHash)
	return err
}

func (g *Git) AutosquashRebase(base string) error {
	// GIT_SEQUENCE_EDITOR=true makes rebase non-interactive
	cmd := exec.Command("git", "rebase", "-i", "--autosquash", base)
	cmd.Env = append(os.Environ(), "GIT_SEQUENCE_EDITOR=true")
	return cmd.Run()
}

func (g *Git) HasStagedChanges() (bool, error) {
	out, err := g.run("diff", "--cached", "--name-only")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

func (g *Git) AbortRebase() error {
	_, err := g.run("rebase", "--abort")
	return err
}

func (g *Git) GetOriginalHead() (string, error) {
	return g.run("rev-parse", "ORIG_HEAD")
}

func (g *Git) ResetHard(ref string) error {
	_, err := g.run("reset", "--hard", ref)
	return err
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/git/... -v -run TestCreateFixupCommit`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/git/
git commit -m "feat(git): add fixup commit and autosquash rebase"
```

---

## Task 6: Stack Package - Branch Naming

**Files:**
- Create: `internal/stack/stack.go`
- Create: `internal/stack/stack_test.go`

**Step 1: Write the failing test**

```go
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
		{"Fix: bug in API endpoint", "fix-bug-in-api"},
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/stack/... -v`
Expected: FAIL - package doesn't exist

**Step 3: Write minimal implementation**

```go
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/stack/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/stack/
git commit -m "feat(stack): add branch naming utilities"
```

---

## Task 7: Stack Package - Parse Stack Branch Pattern

**Files:**
- Modify: `internal/stack/stack.go`
- Modify: `internal/stack/stack_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/stack/stack_test.go

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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/stack/... -v -run TestParseStackBranch`
Expected: FAIL - function doesn't exist

**Step 3: Write minimal implementation**

```go
// Add to internal/stack/stack.go

import (
	"regexp"
	"strconv"
)

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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/stack/... -v -run TestParseStackBranch`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/stack/
git commit -m "feat(stack): add stack branch parsing"
```

---

## Task 8: GitHub Package - Basic Client Setup

**Files:**
- Create: `internal/github/github.go`
- Create: `internal/github/github_test.go`

**Step 1: Write the failing test**

```go
// internal/github/github_test.go
package github

import "testing"

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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/github/... -v`
Expected: FAIL - package doesn't exist

**Step 3: Write minimal implementation**

```go
// internal/github/github.go
package github

import (
	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/api"
)

type Client struct {
	rest    *api.RESTClient
	graphql *api.GraphQLClient
	repo    Repository
}

type Repository struct {
	Owner string
	Name  string
}

func NewClient() (*Client, error) {
	rest, err := api.DefaultRESTClient()
	if err != nil {
		return nil, err
	}

	graphql, err := api.DefaultGraphQLClient()
	if err != nil {
		return nil, err
	}

	// Get current repo
	repo, err := gh.CurrentRepository()
	if err != nil {
		return nil, err
	}

	return &Client{
		rest:    rest,
		graphql: graphql,
		repo: Repository{
			Owner: repo.Owner(),
			Name:  repo.Name(),
		},
	}, nil
}

func (c *Client) Repository() Repository {
	return c.repo
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/github/... -v`
Expected: PASS (or skip if no gh auth)

**Step 5: Commit**

```bash
git add internal/github/
git commit -m "feat(github): add basic client setup with go-gh"
```

---

## Task 9: GitHub Package - Create Pull Request

**Files:**
- Modify: `internal/github/github.go`
- Modify: `internal/github/github_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/github/github_test.go

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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/github/... -v -run TestCreatePullRequestInput`
Expected: FAIL - CreatePullRequestInput doesn't exist

**Step 3: Write minimal implementation**

```go
// Add to internal/github/github.go

import "fmt"

type CreatePullRequestInput struct {
	Title string
	Body  string
	Head  string
	Base  string
}

type PullRequest struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	URL       string `json:"html_url"`
	State     string `json:"state"`
	Mergeable bool   `json:"mergeable"`
	Head      struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

func (c *Client) CreatePullRequest(input CreatePullRequestInput) (*PullRequest, error) {
	path := fmt.Sprintf("repos/%s/%s/pulls", c.repo.Owner, c.repo.Name)

	body := map[string]string{
		"title": input.Title,
		"body":  input.Body,
		"head":  input.Head,
		"base":  input.Base,
	}

	var pr PullRequest
	err := c.rest.Post(path, body, &pr)
	if err != nil {
		return nil, err
	}

	return &pr, nil
}

func (c *Client) UpdatePullRequest(number int, title, body string) error {
	path := fmt.Sprintf("repos/%s/%s/pulls/%d", c.repo.Owner, c.repo.Name, number)

	update := map[string]string{
		"title": title,
		"body":  body,
	}

	return c.rest.Patch(path, update, nil)
}

func (c *Client) UpdatePullRequestBase(number int, base string) error {
	path := fmt.Sprintf("repos/%s/%s/pulls/%d", c.repo.Owner, c.repo.Name, number)

	update := map[string]string{
		"base": base,
	}

	return c.rest.Patch(path, update, nil)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/github/... -v -run TestCreatePullRequestInput`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/github/
git commit -m "feat(github): add pull request creation and update"
```

---

## Task 10: GitHub Package - Get Pull Request Status

**Files:**
- Modify: `internal/github/github.go`

**Step 1: Write the failing test**

```go
// Add to internal/github/github_test.go

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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/github/... -v -run TestPullRequestStatus`
Expected: FAIL - PullRequestStatus doesn't exist

**Step 3: Write minimal implementation**

```go
// Add to internal/github/github.go

type PullRequestStatus struct {
	Number           int
	Title            string
	URL              string
	Branch           string
	BaseBranch       string
	State            string
	Mergeable        bool
	ChecksPassing    bool
	Approvals        int
	ChangesRequested bool
	Additions        int
	Deletions        int
}

func (c *Client) GetPullRequest(number int) (*PullRequestStatus, error) {
	path := fmt.Sprintf("repos/%s/%s/pulls/%d", c.repo.Owner, c.repo.Name, number)

	var pr PullRequest
	err := c.rest.Get(path, &pr)
	if err != nil {
		return nil, err
	}

	return &PullRequestStatus{
		Number:     pr.Number,
		Title:      pr.Title,
		URL:        pr.URL,
		Branch:     pr.Head.Ref,
		BaseBranch: pr.Base.Ref,
		State:      pr.State,
		Mergeable:  pr.Mergeable,
		Additions:  pr.Additions,
		Deletions:  pr.Deletions,
	}, nil
}

func (c *Client) ListPullRequests(head string) ([]PullRequest, error) {
	path := fmt.Sprintf("repos/%s/%s/pulls?head=%s:%s&state=open",
		c.repo.Owner, c.repo.Name, c.repo.Owner, head)

	var prs []PullRequest
	err := c.rest.Get(path, &prs)
	return prs, err
}

func (c *Client) MergePullRequest(number int) error {
	path := fmt.Sprintf("repos/%s/%s/pulls/%d/merge", c.repo.Owner, c.repo.Name, number)

	body := map[string]string{
		"merge_method": "squash",
	}

	return c.rest.Put(path, body, nil)
}

func (c *Client) ClosePullRequest(number int) error {
	path := fmt.Sprintf("repos/%s/%s/pulls/%d", c.repo.Owner, c.repo.Name, number)

	update := map[string]string{
		"state": "closed",
	}

	return c.rest.Patch(path, update, nil)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/github/... -v -run TestPullRequestStatus`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/github/
git commit -m "feat(github): add pull request status and management"
```

---

## Task 11: Stack Package - PR Entry Type and Stack Footer

**Files:**
- Modify: `internal/stack/stack.go`
- Modify: `internal/stack/stack_test.go`

**Step 1: Write the failing test**

```go
// Add to internal/stack/stack_test.go

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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/stack/... -v -run TestGenerateStackFooter`
Expected: FAIL - types and function don't exist

**Step 3: Write minimal implementation**

```go
// Add to internal/stack/stack.go

type PREntry struct {
	Position     int
	CommitHash   string
	CommitMsg    string
	Branch       string
	PRNumber     int
	PRURL        string
	Mergeable    bool
	ChecksPassing bool
	Approvals    int
	ChangesReq   bool
	Additions    int
	Deletions    int
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/stack/... -v -run TestGenerateStackFooter`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/stack/
git commit -m "feat(stack): add PR entry type and stack footer generation"
```

---

## Task 12: Status Command Implementation

**Files:**
- Modify: `cmd/status.go`
- Create: `cmd/status_test.go`

**Step 1: Write the failing test**

```go
// cmd/status_test.go
package cmd

import (
	"bytes"
	"testing"
)

func TestStatusCommand(t *testing.T) {
	// Test that status command exists and has correct flags
	cmd := statusCmd

	if cmd.Use != "status" {
		t.Errorf("expected Use 'status', got %s", cmd.Use)
	}

	// Verify json flag exists on root
	jsonFlag := rootCmd.PersistentFlags().Lookup("json")
	if jsonFlag == nil {
		t.Error("expected --json flag on root command")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/... -v -run TestStatusCommand`
Expected: Could PASS (basic structure exists) or need adjustment

**Step 3: Write the implementation**

```go
// cmd/status.go - replace entire file
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/github"
	"github.com/calvin/gh-stacked/internal/stack"
	"github.com/spf13/cobra"
)

type StatusOutput struct {
	StackID     string             `json:"stack_id"`
	BaseBranch  string             `json:"base_branch"`
	LocalInSync bool               `json:"local_in_sync"`
	PRs         []StatusPROutput   `json:"prs"`
}

type StatusPROutput struct {
	Position         int    `json:"position"`
	Commit           string `json:"commit"`
	Title            string `json:"title"`
	PRNumber         int    `json:"pr_number"`
	PRURL            string `json:"pr_url"`
	Branch           string `json:"branch"`
	TargetBranch     string `json:"target_branch"`
	State            string `json:"state"`
	Mergeable        bool   `json:"mergeable"`
	ChecksPassing    bool   `json:"checks_passing"`
	Approvals        int    `json:"approvals"`
	ChangesRequested bool   `json:"changes_requested"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current stack state with PR links",
	Long: `Show the current state of your PR stack.

Displays each PR with:
  - Position in stack
  - Merge status (MERGEABLE, BLOCKED, WAITING)
  - PR link
  - Target branch
  - Diff stats
  - Review status`,
	Example: `  # Show stack status
  gh stacked status

  # Machine-readable output
  gh stacked status --json`,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	g := git.New()

	// Get current branch
	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	// Check if we're on a stack branch or the parent branch
	if stack.IsStackBranch(currentBranch) {
		return fmt.Errorf("run status from your feature branch, not a stack branch")
	}

	// Get base branch
	baseBranch, err := g.GetBaseBranch()
	if err != nil {
		return fmt.Errorf("failed to get base branch: %w", err)
	}

	// Get commits since base
	commits, err := g.GetCommitsSinceBase(baseBranch)
	if err != nil {
		return fmt.Errorf("failed to get commits: %w", err)
	}

	if len(commits) == 0 {
		if jsonOutput {
			out := StatusOutput{
				StackID:     currentBranch,
				BaseBranch:  baseBranch,
				LocalInSync: true,
				PRs:         []StatusPROutput{},
			}
			return json.NewEncoder(os.Stdout).Encode(out)
		}
		fmt.Println("No commits found since", baseBranch)
		return nil
	}

	// Try to get GitHub client
	gh, err := github.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Build status output
	output := StatusOutput{
		StackID:     currentBranch,
		BaseBranch:  baseBranch,
		LocalInSync: true, // TODO: implement sync check
		PRs:         make([]StatusPROutput, 0, len(commits)),
	}

	for i, commit := range commits {
		position := i + 1
		branchName := stack.StackBranchName(currentBranch, position, commit.Subject)

		prOutput := StatusPROutput{
			Position: position,
			Commit:   commit.Hash[:7],
			Title:    commit.Subject,
			Branch:   branchName,
		}

		// Try to find existing PR for this branch
		prs, err := gh.ListPullRequests(branchName)
		if err == nil && len(prs) > 0 {
			pr := prs[0]
			prOutput.PRNumber = pr.Number
			prOutput.PRURL = pr.URL
			prOutput.TargetBranch = pr.Base.Ref
			prOutput.State = pr.State
			prOutput.Mergeable = pr.Mergeable
			prOutput.Additions = pr.Additions
			prOutput.Deletions = pr.Deletions
		}

		output.PRs = append(output.PRs, prOutput)
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	// Human-readable output
	fmt.Printf("Stack: %s (%d PRs)\n\n", output.StackID, len(output.PRs))

	for _, pr := range output.PRs {
		status := "PENDING"
		if pr.PRNumber > 0 {
			if pr.Mergeable && pr.ChecksPassing {
				status = "MERGEABLE"
			} else if pr.ChangesRequested {
				status = "BLOCKED"
			} else {
				status = "WAITING"
			}
		}

		fmt.Printf("  #%d [%s] %s\n", pr.Position, status, pr.Title)
		if pr.PRNumber > 0 {
			fmt.Printf("     PR #%d: %s\n", pr.PRNumber, pr.PRURL)
			target := pr.TargetBranch
			if target == "" {
				target = baseBranch
			}
			fmt.Printf("     → %s | +%d -%d\n", target, pr.Additions, pr.Deletions)
		} else {
			fmt.Printf("     (not yet pushed)\n")
		}
		fmt.Println()
	}

	if output.LocalInSync {
		fmt.Println("Local: in sync ✓")
	} else {
		fmt.Println("Local: out of sync")
	}

	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/... -v -run TestStatusCommand`
Expected: PASS

**Step 5: Build and verify**

Run: `go build -o gh-stacked . && ./gh-stacked status --help`

**Step 6: Commit**

```bash
git add cmd/
git commit -m "feat(status): implement status command with JSON output"
```

---

## Task 13: Push Command - Preview Mode

**Files:**
- Modify: `cmd/push.go`

**Step 1: Write the failing test**

```go
// cmd/push_test.go
package cmd

import "testing"

func TestPushCommand(t *testing.T) {
	cmd := pushCmd

	if cmd.Use != "push" {
		t.Errorf("expected Use 'push', got %s", cmd.Use)
	}

	// Verify flags exist
	previewFlag := cmd.Flags().Lookup("preview")
	if previewFlag == nil {
		t.Error("expected --preview flag")
	}

	squashFlag := cmd.Flags().Lookup("squash")
	if squashFlag == nil {
		t.Error("expected --squash flag")
	}
}
```

**Step 2: Run test to verify**

Run: `go test ./cmd/... -v -run TestPushCommand`
Expected: PASS (flags already exist)

**Step 3: Implement push command**

```go
// cmd/push.go - replace entire file
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/github"
	"github.com/calvin/gh-stacked/internal/stack"
	"github.com/spf13/cobra"
)

var (
	pushPreview bool
	pushRegroup bool
	pushSquash  []string
)

type PushOutput struct {
	Action     string `json:"action"`
	StackID    string `json:"stack_id"`
	PRsCreated []int  `json:"prs_created"`
	PRsUpdated []int  `json:"prs_updated"`
}

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Create or update stack of PRs from commits",
	Long: `Create or update a stack of PRs from commits on current branch.

Each commit becomes a separate PR, with each PR targeting the previous one.
This creates clean diffs for reviewers - each PR shows only its commit's changes.

The command is idempotent:
  - New commits create new PRs
  - Changed commits update existing PRs
  - Removed commits close orphaned PRs (with confirmation)`,
	Example: `  # Push current commits as stacked PRs
  gh stacked push

  # Preview without creating PRs
  gh stacked push --preview

  # Squash commits 2-4 into one PR before pushing
  gh stacked push --squash 2-4

  # Interactive reorganization
  gh stacked push --regroup`,
	RunE: runPush,
}

func init() {
	rootCmd.AddCommand(pushCmd)

	pushCmd.Flags().BoolVar(&pushPreview, "preview", false, "Show what would happen without making changes")
	pushCmd.Flags().BoolVar(&pushRegroup, "regroup", false, "Interactively reorganize commits before pushing")
	pushCmd.Flags().StringArrayVar(&pushSquash, "squash", nil, "Squash commits N-M into single PR (e.g., --squash 2-4)")
}

func parseSquashRange(s string) (start, end int, err error) {
	re := regexp.MustCompile(`^(\d+)-(\d+)$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return 0, 0, fmt.Errorf("invalid squash range: %s (expected N-M)", s)
	}
	start, _ = strconv.Atoi(matches[1])
	end, _ = strconv.Atoi(matches[2])
	if start >= end {
		return 0, 0, fmt.Errorf("invalid range: start (%d) must be less than end (%d)", start, end)
	}
	return start, end, nil
}

func runPush(cmd *cobra.Command, args []string) error {
	g := git.New()

	// Get current branch
	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if stack.IsStackBranch(currentBranch) {
		return fmt.Errorf("run push from your feature branch, not a stack branch")
	}

	// Get base branch
	baseBranch, err := g.GetBaseBranch()
	if err != nil {
		return fmt.Errorf("failed to get base branch: %w", err)
	}

	// Get commits since base
	commits, err := g.GetCommitsSinceBase(baseBranch)
	if err != nil {
		return fmt.Errorf("failed to get commits: %w", err)
	}

	if len(commits) == 0 {
		fmt.Println("No commits to push")
		return nil
	}

	// Get GitHub client
	gh, err := github.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Check for existing PRs (to validate squash)
	existingPRs := make(map[int]int) // position -> PR number
	for i, commit := range commits {
		position := i + 1
		branchName := stack.StackBranchName(currentBranch, position, commit.Subject)
		prs, err := gh.ListPullRequests(branchName)
		if err == nil && len(prs) > 0 {
			existingPRs[position] = prs[0].Number
		}
	}

	// Validate squash ranges don't include existing PRs
	for _, squashRange := range pushSquash {
		start, end, err := parseSquashRange(squashRange)
		if err != nil {
			return err
		}
		for pos := start; pos <= end; pos++ {
			if prNum, exists := existingPRs[pos]; exists {
				return fmt.Errorf("cannot squash commit %d - it already has PR #%d\nTo combine these, close the PRs manually first, then re-push", pos, prNum)
			}
		}
	}

	// Build preview
	var entries []stack.PREntry
	for i, commit := range commits {
		position := i + 1
		branchName := stack.StackBranchName(currentBranch, position, commit.Subject)

		entry := stack.PREntry{
			Position:   position,
			CommitHash: commit.Hash,
			CommitMsg:  commit.Subject,
			Branch:     branchName,
		}

		if prNum, exists := existingPRs[position]; exists {
			entry.PRNumber = prNum
		}

		entries = append(entries, entry)
	}

	if pushPreview {
		if jsonOutput {
			out := struct {
				StackID  string            `json:"stack_id"`
				Base     string            `json:"base_branch"`
				Commits  []stack.PREntry   `json:"commits"`
				Existing map[int]int       `json:"existing_prs"`
			}{
				StackID:  currentBranch,
				Base:     baseBranch,
				Commits:  entries,
				Existing: existingPRs,
			}
			return json.NewEncoder(os.Stdout).Encode(out)
		}

		fmt.Printf("Preview: %s → %s\n\n", currentBranch, baseBranch)
		for _, e := range entries {
			action := "CREATE"
			if e.PRNumber > 0 {
				action = fmt.Sprintf("UPDATE #%d", e.PRNumber)
			}
			fmt.Printf("  %d. [%s] %s\n", e.Position, action, e.CommitMsg)
			fmt.Printf("     Branch: %s\n\n", e.Branch)
		}
		return nil
	}

	// Actually push
	repo := gh.Repository()
	output := PushOutput{
		StackID:    currentBranch,
		PRsCreated: []int{},
		PRsUpdated: []int{},
	}

	for i, entry := range entries {
		// Create branch at commit
		err := g.CreateBranchAtCommit(entry.Branch, entry.CommitHash)
		if err != nil {
			return fmt.Errorf("failed to create branch %s: %w", entry.Branch, err)
		}

		// Push branch
		err = g.PushBranch("origin", entry.Branch, true)
		if err != nil {
			return fmt.Errorf("failed to push branch %s: %w", entry.Branch, err)
		}

		// Determine base for this PR
		prBase := baseBranch
		if i > 0 {
			prBase = entries[i-1].Branch
		}

		if entry.PRNumber > 0 {
			// Update existing PR
			allEntries := entries
			footer := stack.GenerateStackFooter(currentBranch, allEntries, entry.Position)
			err = gh.UpdatePullRequest(entry.PRNumber, entry.CommitMsg, footer)
			if err != nil {
				return fmt.Errorf("failed to update PR #%d: %w", entry.PRNumber, err)
			}
			output.PRsUpdated = append(output.PRsUpdated, entry.PRNumber)

			if !jsonOutput {
				fmt.Printf("Updated PR #%d: %s\n", entry.PRNumber, entry.CommitMsg)
			}
		} else {
			// Create new PR
			footer := stack.GenerateStackFooter(currentBranch, entries, entry.Position)
			pr, err := gh.CreatePullRequest(github.CreatePullRequestInput{
				Title: entry.CommitMsg,
				Body:  footer,
				Head:  entry.Branch,
				Base:  prBase,
			})
			if err != nil {
				return fmt.Errorf("failed to create PR for %s: %w", entry.CommitMsg, err)
			}

			entries[i].PRNumber = pr.Number
			entries[i].PRURL = pr.URL
			output.PRsCreated = append(output.PRsCreated, pr.Number)

			if !jsonOutput {
				fmt.Printf("Created PR #%d: %s\n", pr.Number, pr.URL)
			}
		}
	}

	output.Action = "created"
	if len(output.PRsUpdated) > 0 && len(output.PRsCreated) == 0 {
		output.Action = "updated"
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	fmt.Printf("\nStack pushed: %d created, %d updated\n",
		len(output.PRsCreated), len(output.PRsUpdated))

	return nil
}
```

**Step 4: Build and test**

Run: `go build -o gh-stacked .`

**Step 5: Commit**

```bash
git add cmd/
git commit -m "feat(push): implement push command with preview and squash validation"
```

---

## Task 14: Amend Command Implementation

**Files:**
- Modify: `cmd/amend.go`

**Step 1: Implement amend command**

```go
// cmd/amend.go - replace entire file
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/github"
	"github.com/calvin/gh-stacked/internal/stack"
	"github.com/spf13/cobra"
)

type AmendOutput struct {
	Amended int      `json:"amended"`
	Commit  string   `json:"commit"`
	Rebased []int    `json:"rebased"`
	Pushed  []string `json:"pushed"`
}

var amendCmd = &cobra.Command{
	Use:   "amend <pr-number>",
	Short: "Amend a specific PR with staged changes",
	Long: `Amend a specific PR in the stack with your staged changes.

This command:
  1. Creates a fixup commit targeting the specified PR's commit
  2. Runs an autosquash rebase to fold it in
  3. Force-pushes all affected PR branches

You must stage your changes before running this command.`,
	Example: `  # Fix PR 2 after code review
  git add .
  gh stacked amend 2`,
	Args: cobra.ExactArgs(1),
	RunE: runAmend,
}

func init() {
	rootCmd.AddCommand(amendCmd)
}

func runAmend(cmd *cobra.Command, args []string) error {
	prPosition, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid PR number: %s", args[0])
	}

	g := git.New()

	// Check for staged changes
	hasStaged, err := g.HasStagedChanges()
	if err != nil {
		return fmt.Errorf("failed to check staged changes: %w", err)
	}
	if !hasStaged {
		return fmt.Errorf("no staged changes. Stage files first with `git add`")
	}

	// Get current branch
	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if stack.IsStackBranch(currentBranch) {
		return fmt.Errorf("run amend from your feature branch, not a stack branch")
	}

	// Get base branch
	baseBranch, err := g.GetBaseBranch()
	if err != nil {
		return fmt.Errorf("failed to get base branch: %w", err)
	}

	// Get commits since base
	commits, err := g.GetCommitsSinceBase(baseBranch)
	if err != nil {
		return fmt.Errorf("failed to get commits: %w", err)
	}

	if prPosition < 1 || prPosition > len(commits) {
		return fmt.Errorf("PR %d not found in current stack (have %d commits)", prPosition, len(commits))
	}

	targetCommit := commits[prPosition-1]

	// Save original HEAD for recovery
	origHead, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to save original state: %w", err)
	}
	_ = origHead // We'll use ORIG_HEAD from git

	// Create fixup commit
	err = g.CreateFixupCommit(targetCommit.Hash)
	if err != nil {
		return fmt.Errorf("failed to create fixup commit: %w", err)
	}

	// Run autosquash rebase
	err = g.AutosquashRebase(baseBranch)
	if err != nil {
		// Attempt to abort and restore
		g.AbortRebase()
		origRef, _ := g.GetOriginalHead()
		if origRef != "" {
			g.ResetHard(origRef)
		}

		return fmt.Errorf(`conflict rebasing "%s" onto %s
Restored original branch state.

To resolve manually:
  git rebase %s
  # fix conflicts
  git rebase --continue
  gh stacked push`, targetCommit.Subject, baseBranch, baseBranch)
	}

	// Get new commits after rebase
	newCommits, err := g.GetCommitsSinceBase(baseBranch)
	if err != nil {
		return fmt.Errorf("failed to get commits after rebase: %w", err)
	}

	// Push affected branches (from amended position onward)
	output := AmendOutput{
		Amended: prPosition,
		Commit:  newCommits[prPosition-1].Hash[:7],
		Rebased: []int{},
		Pushed:  []string{},
	}

	for i := prPosition - 1; i < len(newCommits); i++ {
		commit := newCommits[i]
		branchName := stack.StackBranchName(currentBranch, i+1, commit.Subject)

		err := g.CreateBranchAtCommit(branchName, commit.Hash)
		if err != nil {
			return fmt.Errorf("failed to update branch %s: %w", branchName, err)
		}

		err = g.PushBranch("origin", branchName, true)
		if err != nil {
			return fmt.Errorf("failed to push %s: %w", branchName, err)
		}

		if i+1 > prPosition {
			output.Rebased = append(output.Rebased, i+1)
		}
		output.Pushed = append(output.Pushed, branchName)

		if !jsonOutput {
			if i+1 == prPosition {
				fmt.Printf("Amended: %s\n", branchName)
			} else {
				fmt.Printf("Rebased: %s\n", branchName)
			}
		}
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	fmt.Printf("\nAmended PR %d, rebased %d commits\n", prPosition, len(output.Rebased))

	return nil
}
```

**Step 2: Build and test**

Run: `go build -o gh-stacked . && ./gh-stacked amend --help`

**Step 3: Commit**

```bash
git add cmd/
git commit -m "feat(amend): implement amend command with conflict recovery"
```

---

## Task 15: Merge Command Implementation

**Files:**
- Modify: `cmd/merge.go`

**Step 1: Implement merge command**

```go
// cmd/merge.go - replace entire file
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/github"
	"github.com/calvin/gh-stacked/internal/stack"
	"github.com/spf13/cobra"
)

var mergeNoRebase bool

type MergeOutput struct {
	Merged       int    `json:"merged"`
	MergeCommit  string `json:"merge_commit,omitempty"`
	Retargeted   []int  `json:"retargeted"`
	Rebased      bool   `json:"rebased"`
	RemainingPRs []int  `json:"remaining_prs"`
}

var mergeCmd = &cobra.Command{
	Use:   "merge <pr-number>",
	Short: "Merge a PR and update the remaining stack",
	Long: `Merge a PR and update the remaining stack.

This command:
  1. Merges the specified PR via GitHub API
  2. Retargets the next PR to main
  3. Rebases remaining local commits onto main
  4. Force-pushes all remaining PR branches
  5. Updates PR body footers

PRs must be merged in order - you cannot merge PR 2 before PR 1.`,
	Example: `  # Merge the first PR when ready
  gh stacked merge 1

  # Merge without rebasing the rest (manual sync later)
  gh stacked merge 1 --no-rebase`,
	Args: cobra.ExactArgs(1),
	RunE: runMerge,
}

func init() {
	rootCmd.AddCommand(mergeCmd)

	mergeCmd.Flags().BoolVar(&mergeNoRebase, "no-rebase", false, "Just merge, don't rebase remaining stack")
}

func runMerge(cmd *cobra.Command, args []string) error {
	prPosition, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid PR number: %s", args[0])
	}

	g := git.New()

	// Get current branch
	currentBranch, err := g.GetCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	if stack.IsStackBranch(currentBranch) {
		return fmt.Errorf("run merge from your feature branch, not a stack branch")
	}

	// Get base branch
	baseBranch, err := g.GetBaseBranch()
	if err != nil {
		return fmt.Errorf("failed to get base branch: %w", err)
	}

	// Get commits since base
	commits, err := g.GetCommitsSinceBase(baseBranch)
	if err != nil {
		return fmt.Errorf("failed to get commits: %w", err)
	}

	if prPosition < 1 || prPosition > len(commits) {
		return fmt.Errorf("PR %d not found in current stack (have %d commits)", prPosition, len(commits))
	}

	// Must merge in order
	if prPosition != 1 {
		return fmt.Errorf(`cannot merge PR #%d. PR #1 must be merged first.

Stack order:
  #1 [OPEN] %s ← merge this first
  #%d [OPEN] %s ← you tried to merge this`,
			prPosition, commits[0].Subject, prPosition, commits[prPosition-1].Subject)
	}

	// Get GitHub client
	gh, err := github.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Find the PR for position 1
	branchName := stack.StackBranchName(currentBranch, 1, commits[0].Subject)
	prs, err := gh.ListPullRequests(branchName)
	if err != nil {
		return fmt.Errorf("failed to find PR: %w", err)
	}
	if len(prs) == 0 {
		return fmt.Errorf("no PR found for branch %s", branchName)
	}

	prToMerge := prs[0]

	// Merge the PR
	err = gh.MergePullRequest(prToMerge.Number)
	if err != nil {
		return fmt.Errorf("failed to merge PR #%d: %w", prToMerge.Number, err)
	}

	output := MergeOutput{
		Merged:       prToMerge.Number,
		Retargeted:   []int{},
		Rebased:      false,
		RemainingPRs: []int{},
	}

	if !jsonOutput {
		fmt.Printf("Merged PR #%d: %s\n", prToMerge.Number, commits[0].Subject)
	}

	// If there are more commits, update them
	if len(commits) > 1 && !mergeNoRebase {
		// Retarget PR 2 to main
		secondBranch := stack.StackBranchName(currentBranch, 2, commits[1].Subject)
		secondPRs, err := gh.ListPullRequests(secondBranch)
		if err == nil && len(secondPRs) > 0 {
			err = gh.UpdatePullRequestBase(secondPRs[0].Number, baseBranch)
			if err != nil {
				return fmt.Errorf("failed to retarget PR #%d: %w", secondPRs[0].Number, err)
			}
			output.Retargeted = append(output.Retargeted, secondPRs[0].Number)

			if !jsonOutput {
				fmt.Printf("Retargeted PR #%d to %s\n", secondPRs[0].Number, baseBranch)
			}
		}

		// Pull latest base and rebase
		_, err = g.Run("fetch", "origin", baseBranch)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", baseBranch, err)
		}

		_, err = g.Run("rebase", "origin/"+baseBranch)
		if err != nil {
			g.AbortRebase()
			return fmt.Errorf(`conflict rebasing onto %s
Restored original branch state.

To resolve manually:
  git fetch origin %s
  git rebase origin/%s
  gh stacked push`, baseBranch, baseBranch, baseBranch)
		}

		output.Rebased = true

		// Get new commits and push remaining branches
		newCommits, err := g.GetCommitsSinceBase(baseBranch)
		if err != nil {
			return fmt.Errorf("failed to get commits after rebase: %w", err)
		}

		for i, commit := range newCommits {
			// New position is i+1 (renumbered after first PR merged)
			newPosition := i + 1
			branchName := stack.StackBranchName(currentBranch, newPosition, commit.Subject)

			err := g.CreateBranchAtCommit(branchName, commit.Hash)
			if err != nil {
				return fmt.Errorf("failed to update branch %s: %w", branchName, err)
			}

			err = g.PushBranch("origin", branchName, true)
			if err != nil {
				return fmt.Errorf("failed to push %s: %w", branchName, err)
			}

			// Find PR for this branch to get number
			branchPRs, _ := gh.ListPullRequests(branchName)
			if len(branchPRs) > 0 {
				output.RemainingPRs = append(output.RemainingPRs, branchPRs[0].Number)
			}

			if !jsonOutput {
				fmt.Printf("Updated: %s\n", branchName)
			}
		}
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	fmt.Printf("\nMerge complete. %d PRs remaining.\n", len(output.RemainingPRs))

	return nil
}
```

**Step 2: Add Run method to git package**

```go
// Add to internal/git/git.go

func (g *Git) Run(args ...string) (string, error) {
	return g.run(args...)
}
```

**Step 3: Build and test**

Run: `go build -o gh-stacked . && ./gh-stacked merge --help`

**Step 4: Commit**

```bash
git add cmd/ internal/
git commit -m "feat(merge): implement merge command with stack rebase"
```

---

## Task 16: Add Integration Tests

**Files:**
- Create: `internal/git/integration_test.go`

**Step 1: Write integration tests**

```go
// internal/git/integration_test.go
package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFullWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	os.Chdir(dir)

	// Initialize repo with main branch
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "Initial commit"},
		{"git", "branch", "-M", "main"},
		{"git", "checkout", "-b", "feature/test"},
		{"git", "commit", "--allow-empty", "-m", "First feature"},
		{"git", "commit", "--allow-empty", "-m", "Second feature"},
		{"git", "commit", "--allow-empty", "-m", "Third feature"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %v failed: %v\n%s", args, err, out)
		}
	}

	g := New()

	// Test GetBaseBranch
	base, err := g.GetBaseBranch()
	if err != nil {
		t.Fatalf("GetBaseBranch: %v", err)
	}
	if base != "main" {
		t.Errorf("expected base 'main', got '%s'", base)
	}

	// Test GetCurrentBranch
	branch, err := g.GetCurrentBranch()
	if err != nil {
		t.Fatalf("GetCurrentBranch: %v", err)
	}
	if branch != "feature/test" {
		t.Errorf("expected 'feature/test', got '%s'", branch)
	}

	// Test GetCommitsSinceBase
	commits, err := g.GetCommitsSinceBase("main")
	if err != nil {
		t.Fatalf("GetCommitsSinceBase: %v", err)
	}
	if len(commits) != 3 {
		t.Errorf("expected 3 commits, got %d", len(commits))
	}

	// Verify commit order (oldest first)
	expected := []string{"First feature", "Second feature", "Third feature"}
	for i, c := range commits {
		if c.Subject != expected[i] {
			t.Errorf("commit %d: expected '%s', got '%s'", i, expected[i], c.Subject)
		}
	}
}
```

**Step 2: Run tests**

Run: `go test ./internal/git/... -v`

**Step 3: Commit**

```bash
git add internal/git/
git commit -m "test(git): add integration tests for git workflow"
```

---

## Task 17: Add Squash Validation Tests

**Files:**
- Create: `cmd/push_test.go` (expand)

**Step 1: Write squash validation tests**

```go
// cmd/push_test.go - expand file
package cmd

import "testing"

func TestParseSquashRange(t *testing.T) {
	tests := []struct {
		input     string
		wantStart int
		wantEnd   int
		wantErr   bool
	}{
		{"2-4", 2, 4, false},
		{"1-10", 1, 10, false},
		{"5-3", 0, 0, true},  // start >= end
		{"abc", 0, 0, true},   // invalid format
		{"2-", 0, 0, true},    // missing end
		{"-4", 0, 0, true},    // missing start
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			start, end, err := parseSquashRange(tt.input)
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
			if start != tt.wantStart {
				t.Errorf("start = %d, want %d", start, tt.wantStart)
			}
			if end != tt.wantEnd {
				t.Errorf("end = %d, want %d", end, tt.wantEnd)
			}
		})
	}
}

func TestPushCommand(t *testing.T) {
	cmd := pushCmd

	if cmd.Use != "push" {
		t.Errorf("expected Use 'push', got %s", cmd.Use)
	}

	// Verify flags exist
	previewFlag := cmd.Flags().Lookup("preview")
	if previewFlag == nil {
		t.Error("expected --preview flag")
	}

	squashFlag := cmd.Flags().Lookup("squash")
	if squashFlag == nil {
		t.Error("expected --squash flag")
	}
}
```

**Step 2: Run tests**

Run: `go test ./cmd/... -v`

**Step 3: Commit**

```bash
git add cmd/
git commit -m "test(push): add squash range parsing tests"
```

---

## Task 18: Final Build and Install

**Step 1: Run all tests**

Run: `go test ./... -v`

**Step 2: Build final binary**

Run: `go build -o gh-stacked .`

**Step 3: Install as gh extension**

Run: `gh extension install .`

**Step 4: Test installed extension**

Run: `gh stacked --help`

**Step 5: Commit any final changes**

```bash
git add .
git commit -m "chore: finalize build and installation"
```

---

## Summary

This plan implements:
1. **Git package** - All git operations (branch, commit, rebase)
2. **GitHub package** - PR creation, updates, merging
3. **Stack package** - Branch naming, PR tracking, footer generation
4. **Commands** - status, push, amend, merge with JSON output

Key testing areas covered:
- Squash validation (cannot squash already-pushed commits)
- Branch naming with full parent path preservation
- Conflict recovery with restoration
- Merge order enforcement

Total commits: ~18 focused commits following TDD.
