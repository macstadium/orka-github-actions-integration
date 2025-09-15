# Testing Guide

## Running Unit Tests

To execute unit tests, run the following command:

```bash
make test
```

## Integration Tests

Integration testing can be configured to run at a scheduled time, or when a certain action is performed (e.g., opening a pull request). To run tests using ```make test``` you will need to ensure you have a corresponding ```Makefile``` in your project's root directory that includes the test targets you would like to run. You should also:

- Create a ```tests.yml``` file in the ```.github/workflows``` directory, [defining the build steps] as needed to fit your environment
- After committing and pushing these changes, GitHub Actions will then automatically trigger the testing workflow defined in your ```tests.yml``` file on pushes or pull requests to your ```main``` branch, running the ```make test``` command.

[defining the build steps]: https://docs.github.com/en/actions/tutorials/create-an-example-workflow
