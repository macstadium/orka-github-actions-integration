# Releasing Guide

This guide explains how to release the plugin.

## Releasing the Plugin

## Table of Contents

- [Releasing the Plugin](#releasing-the-plugin)

The plugin is released using GitHub Actions.

### Steps to Release the Plugin

1. Ensure the nightly integration tests pass successfully by reviewing the workflow configuration file [here](https://github.com/macstadium/monorepo-dev/blob/master/.github/workflows/orka-github-runner-tests.yml).
1. Prepare the Image tag:
   * Before triggering the workflow, identify the tag of the image you wish to release.
1. Initiate the Release Workflow:
   * Run the [Release CI Workflow](https://github.com/macstadium/orka-github-actions-integration-private/actions/workflows/release-ci.yml).
1. Tagging and Publishing:
   * The workflow will re-tag the image with the version you specify and push it to the public repository.
1. Verify the Release:
   * After the workflow completes, ensure the new version is correctly published and accessible [in the public repo](https://github.com/macstadium/orka-github-actions-integration).

