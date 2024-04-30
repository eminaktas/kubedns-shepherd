# KubeDNS Shepherd

KubeDNS Shepherd is a Kubernetes controller that manages the DNS configuration of workloads, ensuring efficient and reliable way to configure DNS within your Kubernetes cluster.

## Getting Started

### To Deploy on the cluster

**Deploy the KubeDNS Shepherd to the cluster:**

```sh
make deploy
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply any example from the config/sample:

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall

**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Contributing

Please read our [Contributing Guidelines](CONTRIBUTING.md) before contributing.

More information can be found at our [Development Guide](DEVELOPMENT.md)

## Code of Conduct

Please read our [Code of Conduct](CODE_OF_CONDUCT.md) before engaging with our community.

## License

This project is licensed under the [Apache-2.0 License](LICENSE).
