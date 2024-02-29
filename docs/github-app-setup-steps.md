# Creating a GitHub app

## Setup steps

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