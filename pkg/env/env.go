package env

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"log"

	"github.com/joho/godotenv"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/macstadium/orka-github-actions-integration/pkg/version"
)

type Runner struct {
	Name string
}

type Data struct {
	GitHubAppID             int64
	GitHubAppInstallationID int64
	GitHubAppPrivateKey     string
	GitHubURL               string
	GitHubRunnerVersion     string

	OrkaURL   string
	OrkaToken string

	OrkaNamespace  string
	OrkaVMConfig   string
	OrkaVMUsername string
	OrkaVMPassword string
	OrkaVMMetadata string

	OrkaEnableNodeIPMapping bool
	OrkaNodeIPMapping       map[string]string

	Runners []Runner

	LogLevel string
}

func ParseEnv() *Data {
	if err := godotenv.Load(); err != nil {
		log.Println("Error loading .env file", err)
	}

	envData := &Data{
		GitHubAppPrivateKey: os.Getenv(GitHubAppPrivateKeyEnvName),
		GitHubURL:           os.Getenv(GitHubURLEnvName),
		GitHubRunnerVersion: os.Getenv(GitHubRunnerVersionEnvName),

		OrkaURL:   os.Getenv(OrkaURLEnvName),
		OrkaToken: os.Getenv(OrkaTokenEnvName),

		OrkaNamespace:  getEnvWithDefault(OrkaNamespaceEnvName, "orka-default"),
		OrkaVMConfig:   os.Getenv(OrkaVMConfigEnvName),
		OrkaVMUsername: getEnvWithDefault(OrkaVMUsernameEnvName, "admin"),
		OrkaVMPassword: getEnvWithDefault(OrkaVMPasswordEnvName, "admin"),
		OrkaVMMetadata: getEnvWithDefault(OrkaVMMetadataEnvName, ""),

		OrkaEnableNodeIPMapping: getBoolEnv(OrkaEnableNodeIPMappingEnvName, false),

		LogLevel: getEnvWithDefault(LogLevelEnvName, logging.LogLevelInfo),
	}

	envData.OrkaURL = strings.TrimSuffix(envData.OrkaURL, "/")

	errors := []string{}

	if appID, err := strconv.ParseInt(os.Getenv(GitHubAppIDEnvName), 10, 64); err != nil {
		errors = append(errors, fmt.Sprintf("%s is not set to a valid number: %s", GitHubAppIDEnvName, err))
	} else {
		envData.GitHubAppID = appID
	}

	if installationID, err := strconv.ParseInt(os.Getenv(GitHubAppInstallationIDEnvName), 10, 64); err != nil {
		errors = append(errors, fmt.Sprintf("%s is not set to a valid number: %s", GitHubAppInstallationIDEnvName, err))
	} else {
		envData.GitHubAppInstallationID = installationID
	}

	if envData.GitHubAppPrivateKey == "" {
		gitHubAppPrivateKeyPath := os.Getenv(GitHubAppPrivateKeyPathEnvName)
		if gitHubAppPrivateKeyPath == "" {
			errors = append(errors, fmt.Sprintf("GitHub App private key is required. Please provide either a file path to the private key using %s env or the private key directly using %s env variable", GitHubAppPrivateKeyPathEnvName, GitHubAppPrivateKeyEnvName))
		} else {
			privateKeyContent, err := os.ReadFile(gitHubAppPrivateKeyPath)
			if err != nil {
				errors = append(errors, err.Error())
			}

			envData.GitHubAppPrivateKey = string(privateKeyContent)
		}
	}

	if envData.OrkaEnableNodeIPMapping {
		err := json.Unmarshal([]byte(os.Getenv(OrkaNodeIPMappingEnvName)), &envData.OrkaNodeIPMapping)
		if err != nil {
			errors = append(errors, err.Error())
		}

		if len(envData.OrkaNodeIPMapping) == 0 {
			errors = append(errors, "please provide at least one node IP mapping in order to use public IPs functionality")
		}
	}

	if envData.GitHubRunnerVersion == "" {
		if latestVersion, err := version.GetLatestRunnerVersion(); err != nil {
			errors = append(errors, err.Error())
		} else {
			envData.GitHubRunnerVersion = latestVersion.String()
		}
	}

	if runners, err := getRunnersFromEnv(); err != nil {
		errors = append(errors, err.Error())
	} else {
		envData.Runners = runners
	}

	if errs := validateEnv(envData); len(errs) > 0 {
		errors = append(errors, errs...)
	}

	if len(errors) > 0 {
		panic(fmt.Sprintf("Invalid environment configuration. Please fix the errors below:\n%s", strings.Join(errors, "\n")))
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

func getBoolEnv(key string, fallback bool) bool {
	value := os.Getenv(key)

	if len(value) == 0 {
		return fallback
	}

	return strings.ToLower(value) == "true"
}

func getRunnersFromEnv() ([]Runner, error) {
	values := os.Getenv(RunnersEnvName)

	var runners []Runner
	if err := json.Unmarshal([]byte(values), &runners); err != nil {
		return nil, fmt.Errorf(`unable to parse the %s environment variable as a JSON array of runners. Make sure the variable is correctly set with a valid JSON array, for example, '[{"name":"my-test-runner"}]'`, RunnersEnvName)
	}

	return runners, nil
}

func validateEnv(envData *Data) []string {
	errors := []string{}

	if !regexp.MustCompile(`^https?://github.com/.+`).MatchString(envData.GitHubURL) {
		errors = append(errors, fmt.Sprintf("%s env is required and must be set to the GitHub repository or organization URL, for example, 'https://github.com/your-username/your-repository'", GitHubURLEnvName))
	}

	if !regexp.MustCompile(`^https?://.+`).MatchString(envData.OrkaURL) {
		errors = append(errors, fmt.Sprintf("%s env is required and must be set to the Orka API URL of the Orka cluster, for example, `http://10.221.188.20`", OrkaURLEnvName))
	}

	if envData.OrkaToken == "" {
		errors = append(errors, fmt.Sprintf("%s env is required and must be set to a valid JWT token from the Orka cluster", OrkaTokenEnvName))
	}

	if envData.OrkaVMConfig == "" {
		errors = append(errors, fmt.Sprintf("%s env is required and must be set to a valid and existing VM config in the Orka cluster", OrkaVMConfigEnvName))
	}

	if envData.OrkaVMMetadata != "" && !validateMetadata(envData.OrkaVMMetadata) {
		errors = append(errors, fmt.Sprintf("%s must be formatted as key=value comma separated string", OrkaVMMetadataEnvName))
	}

	return errors
}

func validateMetadata(metadata string) bool {
	r, _ := regexp.Compile(`^(\w+=\w+)(,\s*\w+=\w+)*$`)
	return r.MatchString(metadata)
}
