# Orka GitHub runner

[Orka](https://www.macstadium.com/orka) GitHub runner is a tool that integrates with GitHub to provide demand-based solution for customer workflows.
It leverages ephemeral runners to ensure real-time execution and dynamic scaling, eliminating the need for manual provisioning and maintenance of runners.

## Features

* **Real-time execution:** Ephemeral runners are spun up on demand, ensuring that customer workflows are executed instantly without delays.

* **Dynamic scaling:** Orka GitHub runner automatically scales the number of runners based on the demand for customer workflows, ensuring optimal resource utilization.

* **Secure integration:** Orka GitHub runner utilizes a dedicated GitHub app for authentication and authorization, ensuring secure access to customer GitHub resources.

The Orka Runner application utilizes Runner scale sets in a manner similar to the [ARC project](https://github.com/actions/actions-runner-controller). Runner scale sets is a group of homogeneous runners that can be assigned jobs from GitHub Actions. More information about runner scale sets can be found [here](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners-with-actions-runner-controller/deploying-runner-scale-sets-with-actions-runner-controller).

## Prerequisites

Before using the Orka GitHub Runner, ensure that the following prerequisites are met:

* GitHub App: Having a GitHub App is a prerequisite for using the Orka GitHub Runner. You can find instructions on creating a GitHub App in the [Creating a GitHub app](docs/github-app-setup-steps.md) file.
* Connectivity to Orka 3.0+ cluster: Ensure that the machine where the Orka Github Runner is started has connectivity to the Orka cluster. Additionally, ensure that the SSH ports are open to enable the runner to establish SSH connections with Orka VMs.
* The Orka GitHub runner supports GitHub.com hosted environments. GitHub Enterprise Server was not available to the development team for testing, but given the runner was heavily inspired by the [ARC project](https://github.com/actions/actions-runner-controller), it is expected to work on GitHub Enterprise Server 3.6+

## Setting up the Orka GitHub runner

You can get the Orka GitHub runner by downloading it from [this link](https://github.com/macstadium/orka-github-actions-integration/pkgs/container/orka-github-runner). You will be able to execute the runner (via `docker run`) from any machine that has connectivity to the Orka cluster. If running within MacStadium, you can request 2 vCPU of Private Cloud x86 compute to run the container. 

### Environment variables

The Orka GitHub runner requires the following environment variabales to be configured:
* `GITHUB_APP_ID`: The unique identifier for the GitHub App.
* `GITHUB_APP_INSTALLATION_ID`: The installation identifier for the GitHub App.
* `GITHUB_APP_PRIVATE_KEY_PATH` or `GITHUB_APP_PRIVATE_KEY`: The private key associated with the GitHub App. You can either provide the file path to the private key using `GITHUB_APP_PRIVATE_KEY_PATH` or directly provide the private key string using `GITHUB_APP_PRIVATE_KEY`. At least one of these environment variables must be set.
* `GITHUB_URL`: The URL of the GitHub repository or organization.
* `ORKA_URL`: The URL of the Orka server.
* `ORKA_TOKEN`: The authentication token for accessing the Orka API.
* `ORKA_VM_CONFIG`: The name of the VM config that will be used when deploying Orka virtual machines.
* `ORKA_VM_USERNAME`: Specifies the username for the deployed VMs. If no value is provided, it defaults to admin.
* `ORKA_VM_PASSWORD`: Specifies the password for the deployed VMs. If no value is provided, it defaults to admin.
* `RUNNERS`: A JSON array containing configuration details of the runners.
* `ORKA_ENABLE_NODE_IP_MAPPING`: Specifies whether to enable the mapping of Orka node IPs to external IPs.
* `ORKA_NODE_IP_MAPPING`: Defines the mapping of Orka node internal IPs to external host IPs.
* `LOG_LEVEL`: The logging level for the Orka GitHub Runner (e.g., debug, info, error).

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
