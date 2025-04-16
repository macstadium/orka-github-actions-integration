package version

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/go-version"
)

const defaultMajorVersion = 2

func GetLatestRunnerVersion() (*version.Version, error) {
	resp, err := http.Get("https://api.github.com/repos/actions/runner/releases/latest")
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
