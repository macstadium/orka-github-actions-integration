package version

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/go-version"
)

const defaultMajorVersion = 2

func GetLatestRunnerVersion(token *string) (*version.Version, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/actions/runner/releases/latest", nil)
	if err != nil {
		return nil, err
	}

	// Add GitHub token if available
	if *token != "" {
		req.Header.Set("Authorization", "Bearer "+*token)
	}

	// Add User-Agent header to comply with GitHub API requirements
	req.Header.Set("User-Agent", "orka-github-actions-integration")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch latest release version: %s", resp.Status)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	verObject, err := version.NewVersion(release.TagName)
	if err != nil {
		return nil, err
	}

	if err := validateRunnerVersion(verObject); err != nil {
		return nil, err
	}

	return verObject, nil
}

func validateRunnerVersion(ver *version.Version) error {
	majorVersion := ver.Segments()[0]
	if majorVersion > defaultMajorVersion {
		return fmt.Errorf("we've identified the latest GitHub runner version as %d.x.x. However, we currently do not support this version. Please provide GITHUB_RUNNER_VERSION=\"<your version>\" environment variable to proceed", majorVersion)
	}

	return nil
}
