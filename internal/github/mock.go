// internal/github/mock.go
package github

import "fmt"

// MockGitHub is a test double for GitHubOperations
type MockGitHub struct {
	// State
	Repo Repository
	PRs  map[string][]PullRequest // branch -> PRs

	// Track calls
	Calls []MockCall

	// Control PR number generation
	NextPRNumber int

	// Error injection
	Errors map[string]error

	// Capture created/updated PRs for assertions
	CreatedPRs    []CreatePullRequestInput
	UpdatedPRs    []UpdatedPR
	MergedPRs     []int
	ClosedPRs     []int
	MarkedReadyPRs []int
}

type MockCall struct {
	Method string
	Args   []interface{}
}

type UpdatedPR struct {
	Number int
	Title  string
	Body   string
}

func NewMockGitHub() *MockGitHub {
	return &MockGitHub{
		Repo:         Repository{Owner: "test", Name: "repo"},
		PRs:          make(map[string][]PullRequest),
		NextPRNumber: 100,
		Errors:       make(map[string]error),
		CreatedPRs:   []CreatePullRequestInput{},
		UpdatedPRs:   []UpdatedPR{},
		MergedPRs:    []int{},
	}
}

func (m *MockGitHub) Repository() Repository {
	m.Calls = append(m.Calls, MockCall{Method: "Repository"})
	return m.Repo
}

func (m *MockGitHub) CreatePullRequest(input CreatePullRequestInput) (*PullRequest, error) {
	m.Calls = append(m.Calls, MockCall{Method: "CreatePullRequest", Args: []interface{}{input}})
	if err := m.Errors["CreatePullRequest"]; err != nil {
		return nil, err
	}

	m.CreatedPRs = append(m.CreatedPRs, input)

	pr := &PullRequest{
		Number: m.NextPRNumber,
		URL:    fmt.Sprintf("https://github.com/%s/%s/pull/%d", m.Repo.Owner, m.Repo.Name, m.NextPRNumber),
		Title:  input.Title,
		Body:   input.Body,
		State:  "open",
		Draft:  input.Draft,
	}
	pr.Head.Ref = input.Head
	pr.Base.Ref = input.Base
	m.NextPRNumber++

	// Store for ListPullRequests
	m.PRs[input.Head] = append(m.PRs[input.Head], *pr)

	return pr, nil
}

func (m *MockGitHub) UpdatePullRequest(number int, title, body string) error {
	m.Calls = append(m.Calls, MockCall{Method: "UpdatePullRequest", Args: []interface{}{number, title, body}})
	if err := m.Errors["UpdatePullRequest"]; err != nil {
		return err
	}
	m.UpdatedPRs = append(m.UpdatedPRs, UpdatedPR{Number: number, Title: title, Body: body})
	return nil
}

func (m *MockGitHub) UpdatePullRequestBase(number int, base string) error {
	m.Calls = append(m.Calls, MockCall{Method: "UpdatePullRequestBase", Args: []interface{}{number, base}})
	return m.Errors["UpdatePullRequestBase"]
}

func (m *MockGitHub) GetPullRequest(number int) (*PullRequestStatus, error) {
	m.Calls = append(m.Calls, MockCall{Method: "GetPullRequest", Args: []interface{}{number}})
	if err := m.Errors["GetPullRequest"]; err != nil {
		return nil, err
	}
	// Find PR by number in our stored PRs
	for _, prs := range m.PRs {
		for _, pr := range prs {
			if pr.Number == number {
				return &PullRequestStatus{
					Number:     pr.Number,
					Title:      pr.Title,
					Body:       pr.Body,
					URL:        pr.URL,
					Branch:     pr.Head.Ref,
					BaseBranch: pr.Base.Ref,
					State:      pr.State,
					Draft:      pr.Draft,
					Mergeable:  pr.Mergeable,
					Additions:  pr.Additions,
					Deletions:  pr.Deletions,
				}, nil
			}
		}
	}
	return nil, fmt.Errorf("PR #%d not found", number)
}

func (m *MockGitHub) ListPullRequests(head string) ([]PullRequest, error) {
	m.Calls = append(m.Calls, MockCall{Method: "ListPullRequests", Args: []interface{}{head}})
	if err := m.Errors["ListPullRequests"]; err != nil {
		return nil, err
	}
	// Only return open PRs
	var openPRs []PullRequest
	for _, pr := range m.PRs[head] {
		if pr.State == "open" {
			openPRs = append(openPRs, pr)
		}
	}
	return openPRs, nil
}

func (m *MockGitHub) ListAllPullRequests(head string) ([]PullRequest, error) {
	m.Calls = append(m.Calls, MockCall{Method: "ListAllPullRequests", Args: []interface{}{head}})
	if err := m.Errors["ListAllPullRequests"]; err != nil {
		return nil, err
	}
	// Return all PRs regardless of state
	return m.PRs[head], nil
}

func (m *MockGitHub) MergePullRequest(number int) error {
	m.Calls = append(m.Calls, MockCall{Method: "MergePullRequest", Args: []interface{}{number}})
	if err := m.Errors["MergePullRequest"]; err != nil {
		return err
	}
	m.MergedPRs = append(m.MergedPRs, number)
	return nil
}

func (m *MockGitHub) ClosePullRequest(number int) error {
	m.Calls = append(m.Calls, MockCall{Method: "ClosePullRequest", Args: []interface{}{number}})
	if err := m.Errors["ClosePullRequest"]; err != nil {
		return err
	}
	m.ClosedPRs = append(m.ClosedPRs, number)
	return nil
}

func (m *MockGitHub) MarkPullRequestReadyForReview(number int) error {
	m.Calls = append(m.Calls, MockCall{Method: "MarkPullRequestReadyForReview", Args: []interface{}{number}})
	if err := m.Errors["MarkPullRequestReadyForReview"]; err != nil {
		return err
	}
	m.MarkedReadyPRs = append(m.MarkedReadyPRs, number)
	// Update the PR's draft status in our stored PRs
	for branch, prs := range m.PRs {
		for i, pr := range prs {
			if pr.Number == number {
				m.PRs[branch][i].Draft = false
			}
		}
	}
	return nil
}

// AddExistingPR sets up an existing PR for a branch
func (m *MockGitHub) AddExistingPR(branch string, number int, title string) {
	m.AddExistingPRWithState(branch, number, title, "open")
}

// AddExistingPRWithState sets up an existing PR for a branch with a specific state
func (m *MockGitHub) AddExistingPRWithState(branch string, number int, title string, state string) {
	m.AddExistingPRFull(branch, number, title, state, false)
}

// AddMergedPR sets up a merged PR for a branch
func (m *MockGitHub) AddMergedPR(branch string, number int, title string) {
	m.AddExistingPRFull(branch, number, title, "closed", true)
}

// AddExistingPRFull sets up a PR with full control over state and merged flag
func (m *MockGitHub) AddExistingPRFull(branch string, number int, title string, state string, merged bool) {
	m.AddExistingPRWithBase(branch, number, title, state, merged, "main")
}

// AddExistingPRWithBase sets up a PR with full control including base branch
func (m *MockGitHub) AddExistingPRWithBase(branch string, number int, title string, state string, merged bool, base string) {
	m.AddExistingPRWithDraft(branch, number, title, state, merged, base, false)
}

// AddDraftPR sets up a draft PR for a branch
func (m *MockGitHub) AddDraftPR(branch string, number int, title string) {
	m.AddExistingPRWithDraft(branch, number, title, "open", false, "main", true)
}

// AddExistingPRWithDraft sets up a PR with full control including draft status
func (m *MockGitHub) AddExistingPRWithDraft(branch string, number int, title string, state string, merged bool, base string, draft bool) {
	pr := PullRequest{
		Number: number,
		URL:    fmt.Sprintf("https://github.com/%s/%s/pull/%d", m.Repo.Owner, m.Repo.Name, number),
		Title:  title,
		State:  state,
		Merged: merged,
		Draft:  draft,
	}
	pr.Head.Ref = branch
	pr.Base.Ref = base
	m.PRs[branch] = append(m.PRs[branch], pr)
}

// Verify MockGitHub implements GitHubOperations at compile time
var _ GitHubOperations = (*MockGitHub)(nil)
