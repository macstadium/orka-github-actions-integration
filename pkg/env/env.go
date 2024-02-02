package env

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/joho/godotenv"
)

type Runner struct {
	Name string
}

type Data struct {
	GitHubAppID             int64
	GitHubAppInstallationID int64
	GitHubAppPrivateKeyPath string
	GitHubURL               string

	OrkaURL   string
	OrkaToken string

	OrkaVMConfig   string
	OrkaVMUsername string
	OrkaVMPassword string

	Runners []Runner
}

func ParseEnv() *Data {
	if err := godotenv.Load(); err != nil {
		fmt.Println("Error loading .env file")
	}

	envData := &Data{
		GitHubAppPrivateKeyPath: os.Getenv(GitHubAppPrivateKeyPathEnvName),
		GitHubURL:               os.Getenv(GitHubURLEnvName),

		OrkaURL:   os.Getenv(OrkaURLEnvName),
		OrkaToken: os.Getenv(OrkaTokenEnvName),

		OrkaVMConfig:   os.Getenv(OrkaVMConfigEnvName),
		OrkaVMUsername: getEnvWithDefault(OrkaVMUsernameEnvName, "admin"),
		OrkaVMPassword: getEnvWithDefault(OrkaVMPasswordEnvName, "admin"),
	}

	if appID, err := strconv.ParseInt(os.Getenv(GitHubAppIDEnvName), 10, 64); err != nil {
		panic(fmt.Errorf("%s is not set to a valid number: %w", GitHubAppIDEnvName, err))
	} else {
		envData.GitHubAppID = appID
	}

	if installationID, err := strconv.ParseInt(os.Getenv(GitHubAppInstallationIDEnvName), 10, 64); err != nil {
		panic(fmt.Errorf("%s is not set to a valid number: %w", GitHubAppInstallationIDEnvName, err))
	} else {
		envData.GitHubAppInstallationID = installationID
	}

	if runners, err := getRunnersFromEnv(); err != nil {
		panic(err)
	} else {
		envData.Runners = runners
	}

	if err := validateEnv(envData); err != nil {
		panic(err)
	}

	return envData
}

func getEnvWithDefault(envName string, defaultValue string) string {
	if val, exists := os.LookupEnv(envName); exists {
		return val
	} else {
		return defaultValue
	}
}

func getRunnersFromEnv() ([]Runner, error) {
	values := os.Getenv(RunnersEnvName)

	var runners []Runner
	if err := json.Unmarshal([]byte(values), &runners); err != nil {
		return nil, fmt.Errorf(`unable to parse the %s environment variable as a JSON array of runners. Make sure the variable is correctly set with a valid JSON array, for example, '[{"name":"my-test-runner"}]'`, RunnersEnvName)
	}

	return runners, nil
}

func validateEnv(envData *Data) error {
	if envData.GitHubAppPrivateKeyPath == "" {
		return fmt.Errorf("%s env is required and must be set to the local file path of the private key obtainer from the GitHub UI after installing Orka GitHub app", GitHubAppPrivateKeyPathEnvName)
	}

	if !regexp.MustCompile(`^https?://github.com/.+`).MatchString(envData.GitHubURL) {
		return fmt.Errorf("%s env is required and must be set to the GitHub repository or organization URL, for example, 'https://github.com/your-username/your-repository'", GitHubURLEnvName)
	}

	if !regexp.MustCompile(`^http?://.+`).MatchString(envData.OrkaURL) {
		return fmt.Errorf("%s env is required and must be set to the Orka API URL of the Orka cluster, for example, `http://10.221.188.20`", OrkaURLEnvName)
	}

	if envData.OrkaToken == "" {
		return fmt.Errorf("%s env is required and must be set to a valid JWT token from the Orka cluster", OrkaTokenEnvName)
	}

	if envData.OrkaVMConfig == "" {
		return fmt.Errorf("%s env is required and must be set to a valid and existing VM config in the Orka cluster", OrkaVMConfigEnvName)
	}

	return nil
}
