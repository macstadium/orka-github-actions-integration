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

* GitHub App: Having a GitHub App is a prerequisite for using the Orka GitHub Runner. You can find instructions on creating a GitHub App in the [Creating a GitHub app](docs/github-app-setup-steps.md) file.
* Connectivity to Orka 3.0+ cluster: Ensure that the machine where the Orka Github Runner is started has connectivity to the Orka cluster. Additionally, ensure that the SSH ports are open to enable the runner to establish SSH connections with Orka VMs.
* The Orka GitHub runner has been tested with GitHub.com hosted environments. 

### GitHub Enterprise Server 

The Orka GitHub Runner now supports GitHub Enterprise Server (GHES) environments. To use the runner with GHES, you'll need to provide the following additional environment variables:

* `GITHUB_API_URL`: The URL of your GitHub Enterprise Server API endpoint (e.g., `https://github.enterprise.com/api/v3`)
* `GITHUB_TOKEN`: A GitHub token with appropriate permissions to avoid rate limiting

When using GHES, make sure to:
1. Configure the `GITHUB_URL` to point to your enterprise instance (e.g., `https://github.enterprise.com`)
2. Set the `GITHUB_API_URL` to your enterprise API endpoint
3. Provide a valid `GITHUB_TOKEN` with appropriate permissions for pulling the runner from github.com

The runner will automatically detect if you're using GHES based on the provided URLs and adjust its behavior accordingly.

> [!NOTE]
> GitHub Enterprise Server was not available to the development team for testing at the time this project was initially built, nor is it expected to be made available to our team. We welcome community contributions to this effort. If you would like to help us test the GitHub Enterprise Server integration with Orka, please [open a pull request](https://github.com/macstadium/orka-github-actions-integration/pulls) detailing your suggested code changes, tests, suggested documentation updates, and applicable next steps in the development process. We will then get you connected to a member of our field team to move the PR and testing process forward. 


## Setting up the Orka GitHub runner

You can get the Orka GitHub runner by downloading it from [this link](https://github.com/macstadium/orka-github-actions-integration/pkgs/container/orka-github-runner). You will be able to execute the runner (via `docker run`) from any machine that has connectivity to the Orka cluster. If running within MacStadium, you can request 2 vCPU of Private Cloud x86 compute to run the container.

### Environment variables

The Orka GitHub runner requires the following environment variabales to be configured:
* `GITHUB_APP_ID`: The unique identifier for the GitHub App. Detailed instructions on setting up a GitHub app can be found [here](./docs/github-app-setup-steps.md).
* `GITHUB_APP_INSTALLATION_ID`: The installation identifier for the GitHub App.
* `GITHUB_APP_PRIVATE_KEY_PATH` or `GITHUB_APP_PRIVATE_KEY`: The private key associated with the GitHub App. You can either provide the file path to the private key using `GITHUB_APP_PRIVATE_KEY_PATH` or directly provide the private key string using `GITHUB_APP_PRIVATE_KEY`. At least one of these environment variables must be set.
* `GITHUB_URL`: The URL of the GitHub repository or organization.
* `GITHUB_API_URL`: (Optional) The URL of the GitHub API endpoint. If not provided, it will default to github.com api endpoint if Github URL starts with "https://github.com" otherwise defaults to "<GITHUB_URL>/api/v3"
* `GITHUB_TOKEN`: (Optional) A GitHub token to avoid rate limiting. Required for GitHub Enterprise Server.
* `ORKA_URL`: The URL of the Orka server.
* `ORKA_TOKEN`: The authentication token for accessing the Orka API. A token can be generated by an admin user with the command `orka3 sa token <service-account-name>`.
* `ORKA_VM_CONFIG`: The name of the VM config that will be used when deploying Orka virtual machines. A config can be created with the command `orka3 vmc create --image <image-name>`.
* `ORKA_VM_USERNAME`: Specifies the username for the deployed VMs. If no value is provided, it defaults to admin.
* `ORKA_VM_PASSWORD`: Specifies the password for the deployed VMs. If no value is provided, it defaults to admin.
* `ORKA_VM_METADATA`: Specifies custom VM metadata passed to the VM. Must be formatted as key=value comma separated pairs.
* `ORKA_ENABLE_NODE_IP_MAPPING`: Specifies whether to enable the mapping of Orka node IPs to external IPs.
* `ORKA_NODE_IP_MAPPING`: Defines the mapping of Orka node internal IPs to external host IPs.
* `RUNNERS`: A JSON array containing configuration details of the GitHub runner scale set that will be created. Currently only one runner is supported. See [here](#how-to-use-multiple-runners) for how to use multiple runners. Example usage: `RUNNERS='[{"name":"my-github-runner"}]'`. The `name` field should match the value specified in the `runs-on` field in the Actions workflow. See an example [here](./examples/ci.yml).
* `LOG_LEVEL`: The logging level for the Orka GitHub Runner (e.g., debug, info, error). If not provided, it defaults to info.

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
