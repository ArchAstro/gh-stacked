// internal/github/github.go
package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
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

	// Get current repo - try standard method first
	repo, err := repository.Current()
	if err != nil {
		// Fallback: parse git remote URL directly
		// This handles SSH aliases like github-work
		owner, name, parseErr := parseGitRemote()
		if parseErr != nil {
			return nil, fmt.Errorf("unable to determine current repository: %w", err)
		}
		return &Client{
			rest:    rest,
			graphql: graphql,
			repo:    Repository{Owner: owner, Name: name},
		}, nil
	}

	return &Client{
		rest:    rest,
		graphql: graphql,
		repo: Repository{
			Owner: repo.Owner,
			Name:  repo.Name,
		},
	}, nil
}

// parseGitRemote gets the origin remote URL and parses it
func parseGitRemote() (owner, name string, err error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to get git remote: %w", err)
	}

	url := strings.TrimSpace(string(out))
	return parseRemoteURL(url)
}

// parseRemoteURL extracts owner/repo from a git remote URL
// Supports formats:
//   - git@github.com:owner/repo.git
//   - git@github.com-alias:owner/repo.git
//   - https://github.com/owner/repo.git
func parseRemoteURL(url string) (owner, name string, err error) {
	// SSH format: git@github.com:owner/repo.git or git@github.com-alias:owner/repo.git
	sshRe := regexp.MustCompile(`git@[^:]+:([^/]+)/([^/]+?)(?:\.git)?$`)
	if matches := sshRe.FindStringSubmatch(url); matches != nil {
		return matches[1], matches[2], nil
	}

	// HTTPS format: https://github.com/owner/repo.git
	httpsRe := regexp.MustCompile(`https://[^/]+/([^/]+)/([^/]+?)(?:\.git)?$`)
	if matches := httpsRe.FindStringSubmatch(url); matches != nil {
		return matches[1], matches[2], nil
	}

	return "", "", fmt.Errorf("unable to parse remote URL: %s", url)
}

func (c *Client) Repository() Repository {
	return c.repo
}

type CreatePullRequestInput struct {
	Title string
	Body  string
	Head  string
	Base  string
	Draft bool
}

type PullRequest struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	URL       string `json:"html_url"`
	State     string `json:"state"`
	Draft     bool   `json:"draft"`
	Merged    bool   `json:"merged"`
	Mergeable bool   `json:"mergeable"`
	Head      struct {
		Ref  string `json:"ref"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

func (c *Client) CreatePullRequest(input CreatePullRequestInput) (*PullRequest, error) {
	path := fmt.Sprintf("repos/%s/%s/pulls", c.repo.Owner, c.repo.Name)

	body := map[string]interface{}{
		"title": input.Title,
		"body":  input.Body,
		"head":  input.Head,
		"base":  input.Base,
		"draft": input.Draft,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	var pr PullRequest
	err = c.rest.Post(path, bytes.NewReader(jsonBody), &pr)
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

	jsonBody, err := json.Marshal(update)
	if err != nil {
		return err
	}

	return c.rest.Patch(path, bytes.NewReader(jsonBody), nil)
}

func (c *Client) UpdatePullRequestBase(number int, base string) error {
	path := fmt.Sprintf("repos/%s/%s/pulls/%d", c.repo.Owner, c.repo.Name, number)

	update := map[string]string{
		"base": base,
	}

	jsonBody, err := json.Marshal(update)
	if err != nil {
		return err
	}

	return c.rest.Patch(path, bytes.NewReader(jsonBody), nil)
}

type PullRequestStatus struct {
	Number           int
	Title            string
	Body             string
	URL              string
	Branch           string
	BaseBranch       string
	State            string
	Draft            bool
	Mergeable        bool
	ChecksPassing    bool
	Approvals        int
	ChangesRequested bool
	Additions        int
	Deletions        int
	HeadRepository   string
}

func (c *Client) GetPullRequest(number int) (*PullRequestStatus, error) {
	path := fmt.Sprintf("repos/%s/%s/pulls/%d", c.repo.Owner, c.repo.Name, number)

	var pr PullRequest
	err := c.rest.Get(path, &pr)
	if err != nil {
		return nil, err
	}

	return &PullRequestStatus{
		Number:         pr.Number,
		Title:          pr.Title,
		Body:           pr.Body,
		URL:            pr.URL,
		Branch:         pr.Head.Ref,
		BaseBranch:     pr.Base.Ref,
		State:          pr.State,
		Draft:          pr.Draft,
		Mergeable:      pr.Mergeable,
		Additions:      pr.Additions,
		Deletions:      pr.Deletions,
		HeadRepository: pr.Head.Repo.FullName,
	}, nil
}

func (c *Client) ListPullRequests(head string) ([]PullRequest, error) {
	query := url.Values{}
	query.Set("head", c.repo.Owner+":"+head)
	query.Set("state", "open")
	path := fmt.Sprintf("repos/%s/%s/pulls?%s", c.repo.Owner, c.repo.Name, query.Encode())

	var prs []PullRequest
	err := c.rest.Get(path, &prs)
	return prs, err
}

func (c *Client) ListAllPullRequests(head string) ([]PullRequest, error) {
	query := url.Values{}
	query.Set("head", c.repo.Owner+":"+head)
	query.Set("state", "all")
	path := fmt.Sprintf("repos/%s/%s/pulls?%s", c.repo.Owner, c.repo.Name, query.Encode())

	var prs []PullRequest
	err := c.rest.Get(path, &prs)
	return prs, err
}

func (c *Client) MergePullRequest(number int) error {
	path := fmt.Sprintf("repos/%s/%s/pulls/%d/merge", c.repo.Owner, c.repo.Name, number)

	body := map[string]string{
		"merge_method": "squash",
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	return c.rest.Put(path, bytes.NewReader(jsonBody), nil)
}

func (c *Client) ClosePullRequest(number int) error {
	path := fmt.Sprintf("repos/%s/%s/pulls/%d", c.repo.Owner, c.repo.Name, number)

	update := map[string]string{
		"state": "closed",
	}

	jsonBody, err := json.Marshal(update)
	if err != nil {
		return err
	}

	return c.rest.Patch(path, bytes.NewReader(jsonBody), nil)
}

// MarkPullRequestReadyForReview converts a draft PR to ready for review.
// This requires GraphQL as GitHub's REST API doesn't support this operation.
func (c *Client) MarkPullRequestReadyForReview(number int) error {
	// First, get the PR's node ID via REST API
	path := fmt.Sprintf("repos/%s/%s/pulls/%d", c.repo.Owner, c.repo.Name, number)

	var pr struct {
		NodeID string `json:"node_id"`
	}
	err := c.rest.Get(path, &pr)
	if err != nil {
		return fmt.Errorf("failed to get PR node ID: %w", err)
	}

	// Use GraphQL mutation to mark as ready for review
	var mutation struct {
		MarkPullRequestReadyForReview struct {
			PullRequest struct {
				ID string
			}
		} `graphql:"markPullRequestReadyForReview(input: $input)"`
	}

	type MarkPullRequestReadyForReviewInput struct {
		PullRequestID string `json:"pullRequestId"`
	}

	variables := map[string]interface{}{
		"input": MarkPullRequestReadyForReviewInput{
			PullRequestID: pr.NodeID,
		},
	}

	err = c.graphql.Mutate("MarkPullRequestReadyForReview", &mutation, variables)
	if err != nil {
		return fmt.Errorf("failed to mark PR ready for review: %w", err)
	}

	return nil
}

// AddLabelToIssue adds a label to an issue or pull request.
// Creates the label if it doesn't exist.
func (c *Client) AddLabelToIssue(number int, label string) error {
	path := fmt.Sprintf("repos/%s/%s/issues/%d/labels", c.repo.Owner, c.repo.Name, number)

	body := map[string]interface{}{
		"labels": []string{label},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	return c.rest.Post(path, bytes.NewReader(jsonBody), nil)
}

// RemoveLabelFromIssue removes a label from an issue or pull request.
// Returns nil if the label was not present.
func (c *Client) RemoveLabelFromIssue(number int, label string) error {
	path := fmt.Sprintf("repos/%s/%s/issues/%d/labels/%s", c.repo.Owner, c.repo.Name, number, label)

	err := c.rest.Delete(path, nil)
	if err != nil {
		// Label not present is not an error
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "Label does not exist") {
			return nil
		}
		return err
	}
	return nil
}

func (c *Client) GetRepositoryPermission(username string) (string, error) {
	path := fmt.Sprintf("repos/%s/%s/collaborators/%s/permission", c.repo.Owner, c.repo.Name, username)
	var response struct {
		Permission string `json:"permission"`
	}
	if err := c.rest.Get(path, &response); err != nil {
		return "", err
	}
	return response.Permission, nil
}
