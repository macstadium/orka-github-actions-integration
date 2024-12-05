package github_test

import (
	"fmt"
	"strings"

	"github.com/macstadium/orka-github-actions-integration/pkg/github"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GitHub Config", func() {
	Context("Create GitHub Config", func() {

		tests := []struct {
			configURL string
			expected  *github.GitHubConfig
		}{
			{
				configURL: "https://github.com/org/repo",
				expected: &github.GitHubConfig{
					Scope:        github.GitHubScopeRepository,
					Organization: "org",
					Repository:   "repo",
				},
			},
			{
				configURL: "https://github.com/org/repo/",
				expected: &github.GitHubConfig{
					Scope:        github.GitHubScopeRepository,
					Organization: "org",
					Repository:   "repo",
				},
			},
			{
				configURL: "https://github.com/org",
				expected: &github.GitHubConfig{
					Scope:        github.GitHubScopeOrganization,
					Organization: "org",
					Repository:   "",
				},
			},
			{
				configURL: "https://www.github.com/org",
				expected: &github.GitHubConfig{
					Scope:        github.GitHubScopeOrganization,
					Organization: "org",
					Repository:   "",
				},
			},
			{
				configURL: "https://www.github.com/org/",
				expected: &github.GitHubConfig{
					Scope:        github.GitHubScopeOrganization,
					Organization: "org",
					Repository:   "",
				},
			},
			{
				configURL: "https://github.localhost/org",
				expected: &github.GitHubConfig{
					Scope:        github.GitHubScopeOrganization,
					Organization: "org",
					Repository:   "",
				},
			},
		}

		for _, test := range tests {
			It(fmt.Sprintf("Should create GitHub config with url %s", test.configURL), func() {
				config, err := github.NewGitHubConfig(test.configURL)

				Expect(err).To(BeNil())

				Expect(config.Scope).To(Equal(test.expected.Scope))
				Expect(config.Organization).To(Equal(test.expected.Organization))
				Expect(config.Repository).To(Equal(test.expected.Repository))
			})
		}

		invalidURLs := []string{
			"https://github.com/",
			"https://github.com",
			"https://github.com/some/random/path",
		}

		for _, invalidURL := range invalidURLs {
			It(fmt.Sprintf("Should fail to create GitHub config with url %s", invalidURL), func() {
				config, err := github.NewGitHubConfig(invalidURL)

				Expect(config).To(BeNil())
				Expect(err.Error()).To(Equal(fmt.Sprintf("%q: invalid config URL, should point to an organization or repository", strings.Trim(invalidURL, "/"))))
			})
		}
	})
})
