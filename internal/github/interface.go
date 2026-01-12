// internal/github/interface.go
package github

// GitHubOperations defines all GitHub API operations used by commands
type GitHubOperations interface {
	Repository() Repository
	CreatePullRequest(input CreatePullRequestInput) (*PullRequest, error)
	UpdatePullRequest(number int, title, body string) error
	UpdatePullRequestBase(number int, base string) error
	GetPullRequest(number int) (*PullRequestStatus, error)
	ListPullRequests(head string) ([]PullRequest, error)
	ListAllPullRequests(head string) ([]PullRequest, error)
	MergePullRequest(number int) error
	ClosePullRequest(number int) error
}

// Verify Client implements GitHubOperations at compile time
var _ GitHubOperations = (*Client)(nil)
