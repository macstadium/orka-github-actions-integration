# Orka GitHub runner

[Orka](https://www.macstadium.com/orka) GitHub runner is a tool that integrates with GitHub to provide demand-based solution for customer workflows.
It leverages ephemeral runners to ensure real-time execution and dynamic scaling, eliminating the need for manual provisioning and maintenance of runners.

> **NOTE**: This project is not affiliated with the original [Actions Runner Controller (ARC)](https://github.com/actions/actions-runner-controller) project but uses portions of its code under the terms of the Apache License 2.0.

## Features

* **Real-time execution:** Ephemeral runners are spun up on demand, ensuring that customer workflows are executed instantly without delays.

* **Dynamic scaling:** Orka GitHub runner automatically scales the number of runners based on the demand for customer workflows, ensuring optimal resource utilization.

* **Secure integration:** Orka GitHub runner utilizes a dedicated GitHub app for authentication and authorization, ensuring secure access to customer GitHub resources.

The Orka Runner application utilizes Runner scale sets in a manner similar to the [ARC project](https://github.com/actions/actions-runner-controller). Runner scale sets is a group of homogeneous runners that can be assigned jobs from GitHub Actions. More information about runner scale sets can be found [here](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners-with-actions-runner-controller/deploying-runner-scale-sets-with-actions-runner-controller).

## Prerequisites

Before using the Orka GitHub Runner, ensure that the following prerequisites are met:

* GitHub App: Having a GitHub App is a prerequisite for using the Orka GitHub Runner at the repository or organization level. You can find instructions on creating a GitHub App in the [Creating a GitHub app](docs/github-app-setup-steps.md) file. Registering runners at the enterprise level instead requires a personal access token — see [Enterprise-level runners](#enterprise-level-runners).
* Connectivity to Orka 3.0+ cluster: Ensure that the machine where the Orka Github Runner is started has connectivity to the Orka cluster. Additionally, ensure that the SSH ports are open to enable the runner to establish SSH connections with Orka VMs.
* The Orka GitHub runner has been tested with GitHub.com hosted environments. 

### GitHub Enterprise Server 

The Orka GitHub Runner supports GitHub Enterprise Server (GHES) environments.

When using GHES, make sure to:
1. Configure the `GITHUB_URL` to point to your organization or repository on your enterprise instance (e.g., `https://github.enterprise.com/my-org`)
2. Provide a valid `GITHUB_TOKEN` with appropriate permissions for pulling the runner from github.com, or pin `GITHUB_RUNNER_VERSION` to avoid contacting github.com at startup entirely

The API endpoint is derived from `GITHUB_URL` and is logged on startup, so it does not need to be configured. `GITHUB_API_URL` is accepted but ignored, and the runner logs a warning when it is set.

The runner will automatically detect if you're using GHES based on the provided URL and adjust its behavior accordingly.

### Enterprise-level runners

Runners can be registered at the enterprise level by pointing `GITHUB_URL` at an enterprise, for example `https://github.enterprise.com/enterprises/my-enterprise`.

Enterprise-level registration cannot use GitHub App authentication. GitHub does not grant the `manage_runners:enterprise` permission to App installations, so the registration-token request is rejected with `403 Resource not accessible by integration` regardless of where the App is installed. This is a GitHub limitation and applies equally to the Actions Runner Controller; see [Authenticating ARC to the GitHub API](https://docs.github.com/en/enterprise-cloud@latest/actions/hosting-your-own-runners/managing-self-hosted-runners-with-actions-runner-controller/authenticating-to-the-github-api).

To register at the enterprise level, set `GITHUB_PAT` to a classic personal access token with the `admin:enterprise` (`manage_runners:enterprise`) scope, owned by an enterprise owner. The GitHub App variables are then not required, and the runner will refuse to start if an enterprise `GITHUB_URL` is provided without `GITHUB_PAT`.

Fine-grained personal access tokens do not expose enterprise scopes and cannot be used. Because a classic token with this scope is long-lived and broadly privileged, use a dedicated service account and rotate the token on a schedule.

## Setting up the Orka GitHub runner

You can get the Orka GitHub runner by downloading it from [this link](https://github.com/macstadium/orka-github-actions-integration/pkgs/container/orka-github-runner). You will be able to execute the runner (via `docker run`) from any machine that has connectivity to the Orka cluster. If running within MacStadium, you can request 2 vCPU of Private Cloud x86 compute with 15GB of storage to run the container, with the corresponding compute billed as a part of your Virtual Private Cloud (VPC) services.

### Environment variables

The Orka GitHub runner requires the following environment variabales to be configured:
* `GITHUB_APP_ID`: The unique identifier for the GitHub App. Detailed instructions on setting up a GitHub app can be found [here](./docs/github-app-setup-steps.md). Not required when `GITHUB_PAT` is set.
* `GITHUB_APP_INSTALLATION_ID`: The installation identifier for the GitHub App. Not required when `GITHUB_PAT` is set.
* `GITHUB_APP_PRIVATE_KEY_PATH` or `GITHUB_APP_PRIVATE_KEY`: The private key associated with the GitHub App. You can either provide the file path to the private key using `GITHUB_APP_PRIVATE_KEY_PATH` or directly provide the private key string using `GITHUB_APP_PRIVATE_KEY`. At least one of these environment variables must be set. Not required when `GITHUB_PAT` is set.
* `GITHUB_PAT`: (Optional) A classic personal access token used instead of GitHub App authentication. Required for enterprise-level runners, where GitHub App authentication is not supported — see [Enterprise-level runners](#enterprise-level-runners). When set, it takes precedence over the GitHub App variables.
* `GITHUB_URL`: The URL of the GitHub repository, organization, or enterprise.
* `GITHUB_API_URL`: (Deprecated, ignored) The API URL is derived from `GITHUB_URL` and logged on startup. Setting this variable has no effect and logs a warning.
* `GITHUB_TOKEN`: (Optional) A github.com token used only to look up the latest runner release, to avoid rate limiting. This is always a github.com token, even on GHES, and is unrelated to `GITHUB_PAT`. Unauthenticated lookups are limited to 60 requests per hour per IP, and exceeding that fails startup with `403 Forbidden`. Not needed if `GITHUB_RUNNER_VERSION` is set.
* `GITHUB_RUNNER_VERSION`: (Optional) Pins the GitHub Actions runner version deployed to each VM, for example `2.336.0`. Must not include a leading `v`. If not provided, the latest release is looked up from github.com at startup, which requires outbound access to `api.github.com`.
* `ORKA_URL`: The URL of the Orka server.
* `ORKA_NAMESPACE`: (Optional) The Orka namespace used when deploying VMs. Defaults to `orka-default`.
* `ORKA_TOKEN`: The authentication token for accessing the Orka API. A token can be generated by an admin user with the command `orka3 sa token <service-account-name>`.
* `ORKA_VM_CONFIG`: The name of the VM config that will be used when deploying Orka virtual machines. A config can be created with the command `orka3 vmc create --image <image-name>`.
* `ORKA_VM_USERNAME`: Specifies the username for the deployed VMs. If no value is provided, it defaults to admin.
* `ORKA_VM_PASSWORD`: Specifies the password for the deployed VMs. If no value is provided, it defaults to admin.
* `ORKA_VM_METADATA`: Specifies custom VM metadata passed to the VM. Must be formatted as key=value comma separated pairs.
* `ORKA_EMULATOR_CONFIGS`: (Optional) A comma-separated list of Android emulator config names, for example `pixel8-api36,tablet-api35`. One emulator is paired with every runner VM per entry in the list. When unset, no emulators are deployed. See [Android emulators](#android-emulators).
* `ORKA_EMULATOR_DEPLOY_TIMEOUT`: (Optional) How many minutes to wait for each emulator to reach a running state. Defaults to `10`.
* `ORKA_ENABLE_NODE_IP_MAPPING`: Specifies whether to enable the mapping of Orka node IPs to external IPs.
* `ORKA_NODE_IP_MAPPING`: Defines the mapping of Orka node internal IPs to external host IPs.
* `RUNNERS`: A JSON array containing configuration details of the GitHub runner scale set that will be created. Currently only one runner is supported. See [here](#how-to-use-multiple-runners) for how to use multiple runners. Example usage: `RUNNERS='[{"name":"my-github-runner", "id": 1}]'`. The `name` field should match the value specified in the `runs-on` field in the Actions workflow. The `id` field should be used to differentiate runners with GitHub. We default to `1` if it is not defined. See an example [here](./examples/ci.yml).
* `LOG_LEVEL`: The logging level for the Orka GitHub Runner (e.g., debug, info, error). If not provided, it defaults to info.
* `ENABLE_METRICS`: (Optional) Enables Prometheus metrics exposure. When set to `true`, the service will expose metrics at the `/metrics` endpoint. Defaults to `false`.
* `METRICS_ADDR`: (Optional) The address where the Prometheus metrics endpoint will be exposed (e.g., `:8080`). Defaults to `:8080`.
* `METRICS_POLL_INTERVAL`: (Optional) Interval at which runner scale set statistics are polled and metrics are updated (e.g., `30s`, `1m`). Defaults to `30s`.
* `MANAGE_RUNNER_SCALE_SETS`: (Optional) When set to `true`, deletes any existing runner scale set with the same name on startup and deletes the scale set on exit. When set to `false`, reuses an existing scale set if found and skips deletion on exit. Defaults to `false`.
* `MAX_RUNNERS`: (Optional) The maximum number of runners the scale set will report to GitHub. Defaults to `9000`.
* `ENABLE_RECONCILIATION`: (Optional) When set to `true`, on startup the runner inspects existing Orka VMs for the scale set and adopts, cleans up, or deletes each one so it can recover from a prior process exit. Defaults to `true`.
* `VM_TRACKER_INTERVAL`: (Optional) Interval at which orphaned VMs are checked for. A VM is deleted after two consecutive checks without a corresponding GitHub runner. Defaults to `300s`.
* `RUNNER_DEREGISTRATION_TIMEOUT`: (Optional) How long to wait for a runner to cleanly de-register from GitHub before force-deleting it. Defaults to `30s`.
* `RUNNER_DEREGISTRATION_POLL_INTERVAL`: (Optional) How often to poll GitHub while waiting for a runner to de-register. Defaults to `2s`.

For a complete example of the required format, refer to the `.env` file located in the examples directory [here](./examples/.env).

To start the Orka GitHub runner using Docker, you have two options:

1. Provide all of the environment variables directly in the docker run command

```shell
docker run -e GITHUB_APP_ID=<value> \
    -e GITHUB_APP_INSTALLATION_ID=<value> \
    -e GITHUB_APP_PRIVATE_KEY_PATH=<value> \
    -e GITHUB_URL=<value> \
    -e ORKA_URL=<value> \
    -e ORKA_TOKEN=<value> \
    -e ORKA_VM_CONFIG=<value> \
    -e RUNNERS=<value> \
    ghcr.io/macstadium/orka-github-runner:<tag-name>
```

2. Provide a .env file containing all the environment variables and mount it as a volume when running the Docker container:

```shell
docker run -v /path/to/.env:/.env ghcr.io/macstadium/orka-github-runner:<tag-name>
```

Replace <tag-name> with the version or tag of the Orka GitHub runner you want to use.

> **NOTE**: The private key must be in PKCS#1 RSA private key format. See [here](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/managing-private-keys-for-github-apps#generating-private-keys) for more information. If needed, convert the private key to the correct format: `ssh-keygen -p -m pem -f /path/to/private-key.pem`

### Android emulators

The Orka GitHub runner can pair one or more Android emulators with every runner VM it deploys, so a workflow can run instrumented Android tests against a real emulator on the same host as its macOS runner.

Emulators are deployed from Android emulator configs that you create ahead of time, in the same way `ORKA_VM_CONFIG` names a VM config you created with `orka3 vmc create`. The config owns the Android platform, system image, device profile, and CPU and memory sizing; the runner only names it. Set `ORKA_EMULATOR_CONFIGS` to a comma-separated list of config names, and the runner deploys one emulator per entry:

```shell
ORKA_EMULATOR_CONFIGS="pixel8-api36,tablet-api35"
```

The runner refuses to start if any named config does not exist, listing the configs that do, so a typo surfaces immediately rather than failing every job.

#### Connecting from a workflow

Each job receives the coordinates of its emulators as environment variables. These are the only way a job can reach its emulator: the address is on a bridge that is routable from inside the paired VM and nowhere else, and the job cannot query the Orka cluster.

| Variable | Description |
|---|---|
| `ORKA_EMULATOR_COUNT` | How many emulators are paired with this runner |
| `ORKA_EMULATOR_<n>_ADB` | `host:port` to pass to `adb connect`, 1-indexed |
| `ORKA_EMULATOR_<n>_ADB_HOST`, `ORKA_EMULATOR_<n>_ADB_PORT` | The same address, split |
| `ORKA_EMULATOR_<n>_NAME`, `_CONFIG` | The emulator's name and the config it came from |
| `ORKA_EMULATOR_<n>_PLATFORM`, `_IMAGE_TYPE`, `_DEVICE_PROFILE` | Resolved from the config |
| `ORKA_EMULATOR_ADB`, `ORKA_EMULATOR_ADB_HOST`, `ORKA_EMULATOR_ADB_PORT` | Aliases for the first emulator |
| `ORKA_EMULATOR_ADB_TARGETS` | Every `host:port`, comma separated |

The runner does not connect to the emulator for you, so that a slow first boot never holds up the job being picked up. Connect and wait for the device in your workflow:

```yaml
jobs:
  android-tests:
    runs-on: my-github-runner
    steps:
      - uses: actions/checkout@v4

      - name: Connect to the Orka emulator
        run: |
          adb connect "$ORKA_EMULATOR_ADB"
          adb wait-for-device
          adb shell 'while [ "$(getprop sys.boot_completed)" != 1 ]; do sleep 2; done'

      - name: Run instrumented tests
        run: ./gradlew connectedAndroidTest
```

#### Requirements and limitations

* **Apple silicon only.** Emulators run as host processes on Apple silicon nodes. Intel nodes cannot run them.
* **`adb` must be available inside the VM.** Either bake the Android platform tools into your VM image or install them in the workflow, for example with `android-actions/setup-android`.
* **Emulator config is per runner, not per job.** A runner scale set is a homogeneous pool, so every job on a given runner gets the same emulators. To offer different device targets, run another instance of the Orka GitHub runner with its own runner name and its own `ORKA_EMULATOR_CONFIGS`.
* **The first deploy of a platform on a node is slow.** The Android system image is downloaded on demand the first time a given platform and image type is used on a node, which is why `ORKA_EMULATOR_DEPLOY_TIMEOUT` defaults to 10 minutes. Later deploys on the same node reuse it.
* **Node capacity applies.** Emulators consume CPU and memory on the node alongside the VMs. Sizing them too large for the node the paired VM landed on causes the deploy to fail and the runner to retry with a new VM.

#### How to use multiple runners

Currently, our setup supports only one runner scale set. However, if you need to have multiple runner scale sets, you can achieve this by running multiple instances of the Orka GitHub runner and providing the runner configuration for each of them. Each instance would have its own unique runner name specified in the RUNNERS environment variable.

## How to upgrade?

Upgrading the Orka GitHub plugin to the latest version ensures you have the latest features and bug fixes. Follow these steps to upgrade the plugin:
1. <b>Check for updates</b>: Visit [the Orka GitHub packages page](https://github.com/macstadium/orka-github-actions-integration/pkgs/container/orka-github-runner) to find the latest version of the plugin.
1. <b>Download latest release</b>: Use the command `docker pull ghcr.io/macstadium/orka-github-runner:<version>` to download the latest release of the Orka GitHub plugin.
1. <b>Check for running GitHub CI jobs</b>: Before proceeding, verify that there are no active CI jobs that are currently running.
1. <b>Stop previous instance(s)</b>: Ensure that any existing instances of the Orka GitHub plugin are stopped on your machine before proceeding with the upgrade.
1. <b>Review changelog</b>: Check the changelog for any additional requirements or changes in configuration that may be needed for the new version.
1. <b>Start new plugin version</b>: Execute the necessary docker run commands(mentioned in the previous section) to start the new docker image with the upgraded plugin.

## Changelog

Stay up-to-date with the latest changes, features, and fixes in the Orka GitHub plugin by checking the Changelog [here](https://github.com/macstadium/orka-github-actions-integration/releases).

## Contributing

We welcome contributions to this project! Here's how you can get involved:

1. <b>Reporting Bugs</b>: If you encounter any issues while using the Orka GitHub Runner, please open an issue on the repository. Be sure to include as much detail as possible to help us diagnose and address the problem.
1. <b>Requesting Features</b>: Have an idea for a new feature or enhancement? Feel free to submit a feature request on the repository. We value your feedback and ideas for improving the project.
1. <b>Submitting Pull Requests</b>: If you're interested in contributing code to the project, we encourage you to fork the repository, create a new branch, and submit a pull request with your changes. Make sure to follow our contribution guidelines to streamline the process.

## License

This project is licensed under the Apache License 2.0. Portions of the code are derived from the [ARC project](https://github.com/actions/actions-runner-controller), also licensed under Apache 2.0.

See the [NOTICE](./NOTICE) and [LICENSE](./LICENSE) files for more details.
