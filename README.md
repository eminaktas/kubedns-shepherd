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

## Configuration

```yaml
apiVersion: config.kubedns-shepherd.io/v1alpha1
kind: DNSClass
metadata:
  name: example
spec:
  disabledNamespaces:
  - kube-system
  allowedDNSPolicies:
  - None
  - ClusterFirst
  - ClusterFirstWithHostNet
  dnsPolicy: None
  dnsConfig:
    nameservers:
      - 10.96.0.10
    searches:
      - "svc.{{ .clusterDomain }}"
      - "{{ .podNamespace }}.svc.{{ .clusterDomain }}"
    options:
      - name: ndots
        value: "2"
      - name: edns0 
```

- `disabledNamespaces`: Specifies the namespaces where the DNSClass rule should not be applied.
- `allowedDNSPolicies`: Specifies the DNS policies allowed for the namespaces.
- `dnsPolicy`: Specifies the DNS policy for Pods. Refer to the [Kubernetes documentation](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#pod-s-dns-policy) for more details.
- `dnsConfig`: Specifies the DNS configuration for Pods. Refer to the [Kubernetes documentation](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#pod-dns-config) for more details.

### Dynamic Configuration in DNSClass

KubeDNS Shepherd supports dynamic parameters. Here are the supported keys, which should be used within `{{ }}`:

#### Supported Keys

- `.podNamespace`: Adds the configured pod's namespace.
- `.clusterDomain`: Adds the discovered cluster domain from the `kubelet-config` ConfigMap.
- `.dnsDomain`: Adds the discovered dns domain from the `kubeadm-config` ConfigMap.
- `.clusterName`: Adds the discovered cluster name from the `kubeadm-config` ConfigMap.

nameservers can also be configured if it is not defined by users in the `DNSClass`. It will extract the value from `kubelet-config` ConfigMap.

**Note:** It may fail to discover these parameters if the resources does not exists in your cluster. You can ignore if you don't use dynamic configuration.

## Use Cases

### Improved DNS Resolution
- **Issue**: DNS resolutions in some services were failing intermittently.
- **Solution**: Optimize the environment by using this controller to adjust `ndots` and/or `searches` option for pods or add a dot `.` at the end of the FQDN.

### DNS Query Optimization
- **Issue**: Kubernetes Pods experienced failures in DNS queries for the first attempts due to the `ndots` option set to 5 in `resolv.conf`.
- **Solution**: Optimize the environment by using this controller to adjust `ndots` and `searches` option in `resolv.conf`.

## Contributing

Please read our [Contributing Guidelines](CONTRIBUTING.md) before contributing.

More information can be found at our [Development Guide](DEVELOPMENT.md)

## Code of Conduct

Please read our [Code of Conduct](CODE_OF_CONDUCT.md) before engaging with our community.

## License

This project is licensed under the [Apache-2.0 License](LICENSE).
