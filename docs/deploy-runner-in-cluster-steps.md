# Steps to deploy Orka GitHub Runner into the cluster

This guide provides the steps to deploy the Orka GitHub runner inside your Kubernetes cluster.

## Prerequisites

- Access to your Kubernetes cluster
- A private key for your GitHub App
- Proper configuration of Orka nodes and images

## Steps

### 1. Create the Orka GitHub runner namespace

First, create a dedicated namespace in your Kubernetes cluster for the Orka GitHub runner. This namespace will allow for custom pods:

```bash
orka3 namespace create orka-github-runner --enable-custom-pods
```

### 2. Create a Kubernetes secret for your GitHub App private key

```bash
kubectl create secret generic my-private-key --from-file=<local-file-path-to-your-private-key.pem> -n orka-github-runner
```

### 3. Assign Orka node to the runner namespace

Next, reserve an Orka node for the newly created namespace where the runner will operate:

```bash
orka3 node namespace <intel-node-name> orka-github-runner
```

### 4. Create a VM configuration for the runner

Now, create the VM configuration for the GitHub runner, specifying the Orka image you wish to use:

```bash
orka3 vmc create orka-runner --image <your-image-name> # ghcr.io/macstadium/orka-images/sonoma:latest
```

### 5. Apply the runner deployment configuration

The final step is to apply the Kubernetes deployment configuration that defines the Orka GitHub runner. Make sure to replace the necessary placeholders in the YAML configuration:

```bash
kubectl apply -f deploy-runner-in-cluster.yml
```
