# Development Guide

This guide provides information about how to build, package, and run the plugin locally.

## Table of Contents

- [Build Requirements](#build-requirements)
- [Building, Packaging, and Testing](#building-packaging-and-testing)
- [Running the Plugin Locally](#running-the-plugin-locally)

## Build Requirements

- [Go 1.23.0][golang]

Ensure you have Go installed before proceeding with the build steps.

## Building, Packaging, and Testing

To **build** the plugin:

```bash
make build
```

To check the code for **linting issues**, use:

```bash
make lint
```

To **run** unit tests:

```bash
make test
```

## Dependencies

The plugin uses Go modules to manage dependencies. To install them:

```bash
go mod tidy
```

## Running the Plugin Locally

To run the plugin locally:

1. Create a .env file:

* In your project directory, create a .env file with all the required environment variables.
* The required environment variables are described in the README.md at the root of the project.
* You can also refer to the examples/.env file for a sample configuration.

2. Use the following command to **run** the plugin:

```bash
make run
```
