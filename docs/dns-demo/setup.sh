#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
cluster_name="${CLUSTER_NAME:-kubedns-shepherd-dns-demo}"
context="kind-${cluster_name}"
cert_manager_version="${CERT_MANAGER_VERSION:-v1.20.2}"
kubedns_shepherd_version="${KUBEDNS_SHEPHERD_VERSION:-0.4.4}"
loadgen_image="kubedns-shepherd-dns-loadgen:demo"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Required command not found: $1" >&2
    exit 1
  fi
}

for command_name in kind kubectl helm sed; do
  require_command "${command_name}"
done

container_tool="${CONTAINER_TOOL:-${KIND_EXPERIMENTAL_PROVIDER:-}}"
if [[ -z "${container_tool}" ]]; then
  for candidate in docker podman nerdctl; do
    if command -v "${candidate}" >/dev/null 2>&1; then
      container_tool="${candidate}"
      break
    fi
  done
fi

if [[ -z "${container_tool}" ]]; then
  echo "No supported container tool found; install Docker, Podman, or nerdctl" >&2
  exit 1
fi
require_command "${container_tool}"

export KUBECTL_KUBERC=false

cluster_created=false
if kind get clusters | grep -Fxq "${cluster_name}"; then
  echo "Using existing Kind cluster ${cluster_name}"
else
  echo "Creating isolated Kind cluster ${cluster_name}"
  kind create cluster \
    --name "${cluster_name}" \
    --config "${script_dir}/kind-config.yaml" \
    --wait 5m
  cluster_created=true
fi

kubectl --context "${context}" cluster-info >/dev/null

if [[ "${cluster_created}" == true ]]; then
  kubectl --context "${context}" label nodes --all \
    kubedns-shepherd.io/dns-demo-cluster-dns=10.96.0.53 \
    kubedns-shepherd.io/dns-demo-cluster-domain=example.test \
    --overwrite >/dev/null
else
  cluster_dns_marker="$(kubectl --context "${context}" get nodes \
    -o jsonpath='{.items[0].metadata.labels.kubedns-shepherd\.io/dns-demo-cluster-dns}')"
  cluster_domain_marker="$(kubectl --context "${context}" get nodes \
    -o jsonpath='{.items[0].metadata.labels.kubedns-shepherd\.io/dns-demo-cluster-domain}')"
  if [[ "${cluster_dns_marker}" != "10.96.0.53" || \
    "${cluster_domain_marker}" != "example.test" ]]; then
    echo "Existing cluster ${cluster_name} does not use the current demo DNS configuration" >&2
    echo "Run ./docs/dns-demo/cleanup.sh, then run setup.sh again" >&2
    exit 1
  fi
fi

echo "Building and loading the DNS load-generator image with ${container_tool}"
"${container_tool}" build -t "${loadgen_image}" "${script_dir}/loadgen"
kind load docker-image "${loadgen_image}" --name "${cluster_name}"

echo "Creating isolated demo namespaces and DNS observer"
kubectl --context "${context}" apply -f "${script_dir}/manifests/namespaces.yaml"
kubectl --context "${context}" apply -f "${script_dir}/manifests/test-services.yaml"

observer_image="$(kubectl --context "${context}" \
  get deployment coredns \
  --namespace kube-system \
  -o jsonpath='{.spec.template.spec.containers[0].image}')"

sed "s|DNS_OBSERVER_IMAGE|${observer_image}|g" \
  "${script_dir}/manifests/dns-observer.yaml" \
  | kubectl --context "${context}" apply -f -

kubectl --context "${context}" rollout restart \
  --namespace dns-ndots-demo \
  deployment/dns-observer
kubectl --context "${context}" rollout status \
  --namespace dns-ndots-demo \
  deployment/dns-observer \
  --timeout=2m

cert_manager_url="https://github.com/cert-manager/cert-manager/releases/download/${cert_manager_version}/cert-manager.yaml"
echo "Installing cert-manager ${cert_manager_version}"
kubectl --context "${context}" apply -f "${cert_manager_url}"
kubectl --context "${context}" wait \
  --for=condition=Available \
  --namespace cert-manager \
  deployment/cert-manager \
  deployment/cert-manager-cainjector \
  deployment/cert-manager-webhook \
  --timeout=5m

echo "Installing KubeDNS Shepherd from the Helm repository"
helm repo add kubedns-shepherd https://eminaktas.github.io/kubedns-shepherd/
helm repo update kubedns-shepherd
helm upgrade --install kubedns-shepherd kubedns-shepherd/kubedns-shepherd \
  --kube-context "${context}" \
  --namespace kubedns-shepherd-system \
  --create-namespace \
  --version "${kubedns_shepherd_version}" \
  --values "${script_dir}/helm-values.yaml" \
  --wait \
  --timeout 5m

kubectl --context "${context}" rollout status \
  --namespace kubedns-shepherd-system \
  deployment/kubedns-shepherd-controller-manager \
  --timeout=2m

echo "Waiting for the KubeDNS Shepherd admission webhook certificate"
for attempt in $(seq 1 60); do
  ca_bundle="$(kubectl --context "${context}" \
    get mutatingwebhookconfiguration \
    kubedns-shepherd-mutating-webhook-configuration \
    -o jsonpath='{.webhooks[0].clientConfig.caBundle}' 2>/dev/null || true)"
  if [[ -n "${ca_bundle}" ]]; then
    break
  fi
  if [[ "${attempt}" -eq 60 ]]; then
    echo "Timed out waiting for the webhook CA bundle" >&2
    exit 1
  fi
  sleep 2
done

echo "Waiting for the Helm-deployed DNSClass"
kubectl --context "${context}" wait \
  --for=jsonpath='{.status.state}'=ready \
  dnsclass/kubedns-shepherd-dnsclass-config \
  --timeout=2m

admission_resources="$(kubectl --context "${context}" api-resources \
  --api-group admissionregistration.k8s.io \
  -o name)"
if ! grep -E '^mutatingadmissionpolicies([.]|$)' \
  <<<"${admission_resources}" >/dev/null; then
  echo "The cluster does not serve MutatingAdmissionPolicy; Kubernetes 1.36+ is required" >&2
  exit 1
fi

echo "Applying the native MutatingAdmissionPolicy alternative"
kubectl --context "${context}" apply \
  -f "${repo_root}/docs/examples/mutating-admission-policy.yaml"

echo
echo "Demo environment is ready. Run:"
echo "  ./docs/dns-demo/measure.sh"
