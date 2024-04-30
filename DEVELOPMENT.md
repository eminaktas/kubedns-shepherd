# Development Guide

To run the application against a cluster, you need to have a cluster in your local environment. You can either have access to your local environment or use Minikube or KinD in a Docker environment and use a Docker container for development, such as [VSCode's Dev Containers](https://code.visualstudio.com/docs/devcontainers/containers) feature.

If you set up the environment, the cluster should have Cert Manager installed to auto-generate certificates, which is required to test the webhook.

## Setting up the Environment

1. In `config/default/kustomization.yaml`, make the following change:

    ```yaml
    # [WEBHOOK] To enable webhook, uncomment all the sections with [WEBHOOK] prefix including the one in
    # crd/kustomization.yaml
    # - path: manager_webhook_patch.yaml
    # For local testing, use below and disable above patch
    - path: manager_local_webhook_patch.yaml
    ```

2. In `config/webkhook/kustomization.yaml`, Comment out the following section:

    ```yaml
    # For local testing, disable here
    # configurations:
    # - kustomizeconfig.yaml
    ```

## Create resource in your test cluster

Create required resources in cluster

```sh
make deploy
```

## Downloading Certificates

Run the following command to download the generated certificates for the webhook to your project folder:

```sh
make download-secrets
```

## .devcontainer Configuration

Here is an example configuration for .devcontainer:

```json
{
    "name": "kubedns-shepherd",
    "image": "mcr.microsoft.com/devcontainers/go:1-1.22-bookworm",
    "mounts": [
        "source=${localWorkspaceFolder}/tmp/k8s-webhook-server,target=/tmp/k8s-webhook-server,type=bind",
        "source=${localEnv:HOME}${localEnv:USERPROFILE}/.kube/minikube-config,target=/home/vscode/.kube/config,readonly,type=bind"
    ],
    "runArgs": [
        "--network=minikube"
    ]
}
```

This configuration binds two different paths and sets the network to make sure they are correct.

## Development Commands

- To generate code containing `DeepCopy`, `DeepCopyInto`, and `DeepCopyObject` method implementations, or `WebhookConfiguration`, `ClusterRole`, and `CustomResourceDefinition` objects, use:

    ```sh
    make generate
    ```

- To generate manifests, use:

    ```sh
    make manifest
    ```

*NOTE*: Run `make help` for more information on all potential `make` targets
