# Orka GitHub runner

Orka GitHub runner is a tool that integrates with GitHub to provide demand-based solution for customer workflows.
It leverages ephemeral runners to ensure real-time execution and dynamic scaling, eliminating the need for manual provisioning and maintenance of runners.

## Features

* **Real-time execution:** Ephemeral runners are spun up on demand, ensuring that customer workflows are executed instantly without delays.

* **Dynamic scaling:** Orka GitHub runner automatically scales the number of runners based on the demand for customer workflows, ensuring optimal resource utilization.

* **Secure integration:** Orka GitHub runner utilizes a dedicated GitHub app for authentication and authorization, ensuring secure access to customer GitHub resources.

## Building the Orka GitHub runner

Using the provided `Makefile`, building and running the project can be done as follows:

```shell
make run
```

## Running Tests

To run the tests locally, use the following command:

```bash
make test
```