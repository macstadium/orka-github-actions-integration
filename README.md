# Orka GitHub runner

Orka GitHub runner is a tool that integrates with GitHub to provide demand-based solution for customer workflows.
It leverages ephemeral runners to ensure real-time execution and dynamic scaling, eliminating the need for manual provisioning and maintenance of runners.

## Features

* **Real-time execution:** Ephemeral runners are spun up on demand, ensuring that customer workflows are executed instantly without delays.

* **Dynamic scaling:** Orka GitHub runner automatically scales the number of runners based on the demand for customer workflows, ensuring optimal resource utilization.

* **Secure integration:** Orka GitHub runner utilizes a dedicated GitHub app for authentication and authorization, ensuring secure access to customer GitHub resources.

The Orka Runner application utilizes Runner scale sets in a manner similar to the [ARC project](https://github.com/actions/actions-runner-controller). Runner scale sets is a group of homogeneous runners that can be assigned jobs from GitHub Actions. More information about runner scale sets can be found [here](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners-with-actions-runner-controller/deploying-runner-scale-sets-with-actions-runner-controller).

## Prerequisites

Before using the Orka GitHub Runner, ensure that the following prerequisites are met:

* GitHub App: Having a GitHub App is a prerequisite for using the Orka GitHub Runner. You can find instructions on creating a GitHub App in the [Creating a GitHub app](#creating-a-github-app) section below.
* Connectivity to Orka cluster: Ensure that the machine where the Orka Github Runner is started has connectivity to an Orka cluster.

## Creating a GitHub app

### Setup steps

* Choose App Creation Method: Decide whether to create the app for your user account or an organization.
* Create GitHub App:
    * User Account: Click the [following link](https://github.com/settings/apps/new?url=https://github.com/macstadium/orka-github-actions-integration&webhook_active=false&public=false&actions=read&administration=write), which pre-fills the required permissions:

        **Repository Permissions**
        * Actions (read)
        * Administration (read/write)
        * Metadata (read)
    * Organization: Replace `:org` with your organization name in the [following link](https://github.com/organizations/:org/settings/apps/new?url=https://github.com/macstadium/orka-github-actions-integration&webhook_active=false&public=false&administration=write&organization_self_hosted_runners=write&actions=read&checks=read), which pre-fills the required permissions:

        **Repository Permissions**
        * Actions (read)
        * Metadata (read)

        **Organization Permissions**
        * Self-hosted runners (read/write)
* Retrieve App ID: Locate the displayed App ID on the app's page.
* Download Private Key: Click `Generate a private key` and download the file securely.
* Install App: Navigate to the `Install App` tab and complete the installation for your user account or organization.
* Retrieve Installation ID: Once you've successfully installed the app, locate the installation URL. It will look something like this: https://github.com/installations/1234567. Your Installation ID is the last number in the URL (in this case, 1234567).

## Building the Orka GitHub runner

### Environment variables

The Orka GitHub runner requires the following environment variabales to be configured:
* `GITHUB_APP_ID`: The unique identifier for the GitHub App.
* `GITHUB_APP_INSTALLATION_ID`: The installation identifier for the GitHub App.
* `GITHUB_APP_PRIVATE_KEY_PATH`: The file path to the private key associated with the GitHub App.
* `GITHUB_URL`: The URL of the GitHub repository or organization.
* `ORKA_URL`: The URL of the Orka server.
* `ORKA_TOKEN`: The authentication token for accessing the Orka API.
* `ORKA_VM_CONFIG`: The name of the VM config that will be used when deploying Orka virtual machines.
* `ORKA_VM_USERNAME`: Specifies the username for the deployed VMs. If no value is provided, it defaults to admin.
* `ORKA_VM_PASSWORD`: Specifies the password for the deployed VMs. If no value is provided, it defaults to admin.
* `RUNNERS`: A JSON array containing configuration details of the runners.
* `LOG_LEVEL`: The logging level for the Orka GitHub Runner (e.g., debug, info, error).

Using the provided `Makefile`, building and running the project can be done as follows:

```shell
make run
```

## Running Tests

To run the tests locally, use the following command:

```bash
make test
```