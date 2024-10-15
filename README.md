# KubeDNS Shepherd

[![Release](https://github.com/eminaktas/kubedns-shepherd/actions/workflows/release.yaml/badge.svg)](https://github.com/eminaktas/kubedns-shepherd/actions/workflows/release.yaml)
[![Unit test](https://github.com/eminaktas/kubedns-shepherd/actions/workflows/unit-tests.yaml/badge.svg?branch=main)](https://github.com/eminaktas/kubedns-shepherd/actions/workflows/unit-tests.yaml)
[![Lint](https://github.com/eminaktas/kubedns-shepherd/actions/workflows/lint.yaml/badge.svg?branch=main)](https://github.com/eminaktas/kubedns-shepherd/actions/workflows/lint.yaml)
[![Go Report Card](https://goreportcard.com/badge/eminaktas/kubedns-shepherd)](https://goreportcard.com/report/eminaktas/kubedns-shepherd)
[![Coverage Status](https://coveralls.io/repos/github/eminaktas/kubedns-shepherd/badge.svg?branch=main)](https://coveralls.io/github/eminaktas/kubedns-shepherd?branch=main)
[![Latest release](https://badgen.net/github/release/eminaktas/kubedns-shepherd)](https://github.com/eminaktas/kubedns-shepherd)

KubeDNS Shepherd is a Kubernetes controller that manages the DNS configuration of workloads, ensuring an efficient and reliable way to configure DNS within your Kubernetes cluster. This project is essential for those looking to optimize DNS resolutions and configurations within their Kubernetes environments.

## Getting Started

### To Deploy on the Cluster

**Deploy the KubeDNS Shepherd to the cluster:**

```sh
helm repo add kubedns-shepherd https://eminaktas.github.io/kubedns-shepherd/
helm repo update
helm install kubedns-shepherd kubedns-shepherd/kubedns-shepherd --namespace kubedns-shepherd-system --create-namespace
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin privileges or be logged in as admin.

**Create instances of your solution:**
You can apply any example from the config/sample:

> **NOTE**: Ensure that the samples have default values to test them out.

### To Uninstall

**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Uninstall the KubeDNS Shepherd from the cluster:**

```sh
helm uninstall kubedns-shepherd --namespace kubedns-shepherd-system
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

> [!WARNING]
> **Compatibility with GKE and EKS**
>
> Please note that the templating for `.clusterDomain`, `.dnsDomain`, `.clusterName`, and nameservers does not work in GKE or EKS due to missing kubelet-config and kubeadm-config ConfigMaps in those environments. If you are using these platforms, these parameters will not be automatically discovered. You can either define these values manually or ignore them if dynamic configuration is not required.

#### Supported Keys

- `.podNamespace`: Adds the configured pod's namespace.
- `.clusterDomain`: Adds the discovered cluster domain from the `kubelet-config` ConfigMap.
- `.dnsDomain`: Adds the discovered DNS domain from the `kubeadm-config` ConfigMap.
- `.clusterName`: Adds the discovered cluster name from the `kubeadm-config` ConfigMap.

Nameservers can also be configured if they are not defined by users in the `DNSClass`. It will extract the value from the `kubelet-config` ConfigMap.

**Note:** It may fail to discover these parameters if the resources do not exist in your cluster. You can ignore them if you don't use dynamic configuration.

## Use Cases

### Improved DNS Resolution

- **Issue**: DNS resolutions in some services were failing intermittently.
- **Solution**: Optimize the environment by using this controller to adjust `ndots` and/or `searches` options for pods or add a dot `.` at the end of the FQDN.

### DNS Query Optimization

- **Issue**: Kubernetes Pods experienced failures in DNS queries for the first attempts due to the `ndots` option set to 5 in `resolv.conf`.
- **Solution**: Optimize the environment by using this controller to adjust `ndots` and `searches` options in `resolv.conf`.

## Contributing

Please read our [Contributing Guidelines](CONTRIBUTING.md) before contributing.

More information can be found in our [Development Guide](DEVELOPMENT.md).

## Code of Conduct

Please read our [Code of Conduct](CODE_OF_CONDUCT.md) before engaging with our community.

## License

This project is licensed under the [Apache-2.0 License](LICENSE).
