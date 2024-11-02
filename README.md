# KubeDNS Shepherd GitHub Pages

This is the branch where we publish Helm charts to GitHub Pages.

## About KubeDNS Shepherd

KubeDNS Shepherd is a Kubernetes controller that centralizes and manages DNS configurations for workloads within a Kubernetes cluster. It provides an efficient, automated, and reliable method to handle DNS configurations, reducing complexity and improving the robustness of DNS management across different namespaces and workloads.

- Efficient DNS Configuration: Simplifies DNS management within your Kubernetes cluster by automating and consolidating configurations.
- Seamless Integration: Works with Kubernetes-native resources, making it easy to integrate with existing workflows.
- Scalability: Ideal for managing DNS configurations in large or complex Kubernetes environments.

## Installation

To install KubeDNS Shepherd using Helm, add the Helm repository:

```bash
helm repo add kubedns-shepherd https://eminaktas.github.io/kubedns-shepherd/
helm repo update
helm install kubedns-shepherd kubedns-shepherd/kubedns-shepherd --namespace kubedns-shepherd-system --create-namespace
```

## Documentation

For complete documentation on KubeDNS Shepherd, including configuration options, examples, and advanced usage, refer to the [KubeDNS Shepherd README](https://github.com/eminaktas/kubedns-shepherd?tab=readme-ov-file#kubedns-shepherd).

## Contributing

We welcome contributions to KubeDNS Shepherd! If you’d like to contribute, please fork the repository and submit a pull request. Ensure that your contributions adhere to the guidelines outlined in the [CONTRIBUTING.md](https://github.com/eminaktas/kubedns-shepherd/blob/main/CONTRIBUTING.md#contributing-to-kubedns-shepherd) file.

## License

This project is licensed under the [Apache-2.0 License](LICENSE).
