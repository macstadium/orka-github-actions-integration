package github

import (
	"fmt"
	"net/url"
	"strings"
)

type GitHubConfig struct {
	Scope        GitHubScope
	Organization string
	Repository   string
	URL          string
}

type GitHubScope int

const (
	GitHubScopeUnknown GitHubScope = iota
	GitHubScopeOrganization
	GitHubScopeRepository
)

var ErrInvalidGitHubConfigURL = fmt.Errorf("invalid config URL, should point to an enterprise, org, or repository")

func NewGitHubConfig(gitHubURL string) (*GitHubConfig, error) {
	u, err := url.Parse(strings.Trim(gitHubURL, "/"))
	if err != nil {
		return nil, err
	}

	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")

	invalidURLError := fmt.Errorf("%q: %w", u.String(), ErrInvalidGitHubConfigURL)

	config := &GitHubConfig{
		URL: gitHubURL,
	}

	switch len(pathParts) {
	case 1: // Organization
		if pathParts[0] == "" {
			return nil, invalidURLError
		}

		config.Scope = GitHubScopeOrganization
		config.Organization = pathParts[0]

	case 2: // Repository
		config.Scope = GitHubScopeRepository
		config.Organization = pathParts[0]
		config.Repository = pathParts[1]
	default:
		return nil, invalidURLError
	}

	return config, nil
}
