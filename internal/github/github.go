// internal/github/github.go
package github

import (
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
