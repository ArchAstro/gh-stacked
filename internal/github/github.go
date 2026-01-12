// internal/github/github.go
package github

import (
	"bytes"
	"encoding/json"
	"fmt"

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

	// Get current repo
	repo, err := repository.Current()
	if err != nil {
		return nil, err
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

func (c *Client) Repository() Repository {
	return c.repo
}

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
