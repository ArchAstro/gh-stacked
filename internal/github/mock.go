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
	CreatedPRs []CreatePullRequestInput
	UpdatedPRs []UpdatedPR
	MergedPRs  []int
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
		State:  "open",
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
					URL:        pr.URL,
					Branch:     pr.Head.Ref,
					BaseBranch: pr.Base.Ref,
					State:      pr.State,
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
	return m.Errors["ClosePullRequest"]
}

// AddExistingPR sets up an existing PR for a branch
func (m *MockGitHub) AddExistingPR(branch string, number int, title string) {
	m.AddExistingPRWithState(branch, number, title, "open")
}

// AddExistingPRWithState sets up an existing PR for a branch with a specific state
func (m *MockGitHub) AddExistingPRWithState(branch string, number int, title string, state string) {
	pr := PullRequest{
		Number: number,
		URL:    fmt.Sprintf("https://github.com/%s/%s/pull/%d", m.Repo.Owner, m.Repo.Name, number),
		Title:  title,
		State:  state,
	}
	pr.Head.Ref = branch
	m.PRs[branch] = append(m.PRs[branch], pr)
}

// Verify MockGitHub implements GitHubOperations at compile time
var _ GitHubOperations = (*MockGitHub)(nil)
