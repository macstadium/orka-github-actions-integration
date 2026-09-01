package env

// maxUserdataEncodedSize is the maximum size of the base64-encoded userdata
// script accepted by the Orka API (64KiB).
const maxUserdataEncodedSize = 65536

const (
	GitHubAppIDEnvName             = "GITHUB_APP_ID"
	GitHubAppInstallationIDEnvName = "GITHUB_APP_INSTALLATION_ID"
	GitHubAppPrivateKeyPathEnvName = "GITHUB_APP_PRIVATE_KEY_PATH"
	GitHubAppPrivateKeyEnvName     = "GITHUB_APP_PRIVATE_KEY"
	GitHubURLEnvName               = "GITHUB_URL"
	GitHubAPIURLEnvName            = "GITHUB_API_URL"
	GitHubRunnerVersionEnvName     = "GITHUB_RUNNER_VERSION"
	GitHubTokenEnvName             = "GITHUB_TOKEN" // Token for public GitHub API authentication
	GitHubPATEnvName               = "GITHUB_PAT"

	OrkaURLEnvName   = "ORKA_URL"
	OrkaTokenEnvName = "ORKA_TOKEN"

	OrkaNamespaceEnvName          = "ORKA_NAMESPACE"
	OrkaVMConfigEnvName           = "ORKA_VM_CONFIG"
	OrkaVMUsernameEnvName         = "ORKA_VM_USERNAME"
	OrkaVMPasswordEnvName         = "ORKA_VM_PASSWORD"
	OrkaVMMetadataEnvName         = "ORKA_VM_METADATA"
	OrkaVMUserdataEnvName         = "ORKA_VM_USERDATA"
	OrkaVMUserdataFilePathEnvName = "ORKA_VM_USERDATA_FILE_PATH"

	OrkaEnableNodeIPMappingEnvName = "ORKA_ENABLE_NODE_IP_MAPPING"
	OrkaNodeIPMappingEnvName       = "ORKA_NODE_IP_MAPPING"

	RunnersEnvName = "RUNNERS"

	RunnerDeregistrationTimeoutEnvName      = "RUNNER_DEREGISTRATION_TIMEOUT"
	RunnerDeregistrationPollIntervalEnvName = "RUNNER_DEREGISTRATION_POLL_INTERVAL"

	VMTrackerIntervalEnvName = "VM_TRACKER_INTERVAL"

	MaxRunnersEnvName = "MAX_RUNNERS"

	LogLevelEnvName = "LOG_LEVEL"

	// Prometheus metrics
	EnableMetricsEnvName       = "ENABLE_METRICS"
	MetricsAddrEnvName         = "METRICS_ADDR"
	MetricsPollIntervalEnvName = "METRICS_POLL_INTERVAL"

	ManageRunnerScaleSetsEnvName = "MANAGE_RUNNER_SCALE_SETS"

	EnableReconciliationEnvName = "ENABLE_RECONCILIATION"
)
