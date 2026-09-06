#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cluster_name="${CLUSTER_NAME:-kubedns-shepherd-dns-demo}"
context="kind-${cluster_name}"
queries="${QUERIES:-250}"
loadgen_image="kubedns-shepherd-dns-loadgen:demo"

if ! [[ "${queries}" =~ ^[1-9][0-9]*$ ]]; then
  echo "QUERIES must be a positive integer, got ${queries}" >&2
  exit 2
fi

for command_name in kubectl sed awk; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Required command not found: ${command_name}" >&2
    exit 1
  fi
done

export KUBECTL_KUBERC=false
kubectl --context "${context}" cluster-info >/dev/null

scenarios=()
target_names=()
use_cases=()
ndots_values=()
dns_request_counts=()
nxdomain_counts=()

run_scenario() {
  local scenario namespace job manifest ndots target use_case
  local pod_name pod_dns pod_logs result_line dns_requests nxdomain_responses

  scenario="$1"
  namespace="$2"
  job="$3"
  manifest="$4"
  ndots="$5"
  target="$6"
  use_case="$7"

  echo
  echo "Running ${scenario} scenario for ${target} (${use_case})"
  kubectl --context "${context}" delete job "${job}" \
    --namespace "${namespace}" \
    --ignore-not-found \
    --wait >/dev/null

  sed \
    -e "s/__QUERIES__/${queries}/g" \
    -e "s/__TARGET__/${target}/g" \
    -e "s|DNS_LOADGEN_IMAGE|${loadgen_image}|g" \
    "${manifest}" \
    | kubectl --context "${context}" apply -f - >/dev/null

  if ! kubectl --context "${context}" wait \
    --for=condition=complete \
    --namespace "${namespace}" \
    "job/${job}" \
    --timeout=5m >/dev/null; then
    kubectl --context "${context}" describe job "${job}" --namespace "${namespace}" >&2 || true
    kubectl --context "${context}" logs "job/${job}" --namespace "${namespace}" >&2 || true
    exit 1
  fi

  pod_name="$(kubectl --context "${context}" \
    get pod \
    --namespace "${namespace}" \
    --selector "job-name=${job}" \
    -o jsonpath='{.items[0].metadata.name}')"
  pod_dns="$(kubectl --context "${context}" \
    get pod "${pod_name}" \
    --namespace "${namespace}" \
    -o jsonpath='{.spec.dnsPolicy}{" | options="}{.spec.dnsConfig.options}{" | dns-class="}{.metadata.annotations.kubedns-shepherd\.io/dns-class-name}')"

  pod_logs="$(kubectl --context "${context}" logs "${pod_name}" --namespace "${namespace}")"
  result_line="$(printf '%s\n' "${pod_logs}" | awk '/^\{"target":/ { result = $0 } END { print result }')"
  dns_requests="$(printf '%s\n' "${result_line}" \
    | sed -n 's/.*"dns_requests":\([0-9][0-9]*\).*/\1/p')"
  nxdomain_responses="$(printf '%s\n' "${result_line}" \
    | sed -n 's/.*"nxdomain_responses":\([0-9][0-9]*\).*/\1/p')"
  if [[ -z "${dns_requests}" || -z "${nxdomain_responses}" ]]; then
    echo "Could not read client-side DNS counters from ${pod_name}" >&2
    printf '%s\n' "${pod_logs}" >&2
    exit 1
  fi

  echo "Admitted Pod: ${pod_dns}"
  printf '%s\n' "${pod_logs}"

  scenarios+=("${scenario}")
  target_names+=("${target}")
  use_cases+=("${use_case}")
  ndots_values+=("${ndots}")
  dns_request_counts+=("${dns_requests}")
  nxdomain_counts+=("${nxdomain_responses}")
}

lookup_targets=(api api.prod api.prod.external.test)
lookup_use_cases=(same-namespace other-namespace outside-cluster)

for target_index in "${!lookup_targets[@]}"; do
  target="${lookup_targets[$target_index]}"
  use_case="${lookup_use_cases[$target_index]}"

  run_scenario \
    baseline \
    default \
    dns-loadgen-baseline \
    "${script_dir}/manifests/job-baseline.yaml" \
    5 \
    "${target}" \
    "${use_case}"

  run_scenario \
    kubedns-shepherd \
    dns-ndots-shepherd \
    dns-loadgen-shepherd \
    "${script_dir}/manifests/job-shepherd.yaml" \
    2 \
    "${target}" \
    "${use_case}"

  run_scenario \
    native-policy \
    default \
    dns-loadgen-native-policy \
    "${script_dir}/manifests/job-native-policy.yaml" \
    2 \
    "${target}" \
    "${use_case}"
done

echo
printf '%-23s %-17s %-19s %7s %13s %14s %11s\n' \
  Target 'Use case' Scenario ndots 'App lookups' 'Client DNS req' NXDOMAIN
printf '%-23s %-17s %-19s %7s %13s %14s %11s\n' \
  ----------------------- ----------------- ------------------- ------- ------------- -------------- -----------

for index in "${!scenarios[@]}"; do
  printf '%-23s %-17s %-19s %7s %13s %14s %11s\n' \
    "${target_names[$index]}" \
    "${use_cases[$index]}" \
    "${scenarios[$index]}" \
    "${ndots_values[$index]}" \
    "${queries}" \
    "${dns_request_counts[$index]}" \
    "${nxdomain_counts[$index]}"
done

echo
echo "Observed DNS request change (KubeDNS Shepherd vs baseline):"
for target_index in "${!lookup_targets[@]}"; do
  result_index=$((target_index * 3))
  baseline_requests="${dns_request_counts[$result_index]}"
  optimized_requests="${dns_request_counts[$((result_index + 1))]}"
  change="$(awk -v baseline="${baseline_requests}" -v optimized="${optimized_requests}" '
    BEGIN {
      if (baseline == 0) { print "n/a" }
      else if (optimized < baseline) { printf "%.1f%% fewer", (1 - optimized / baseline) * 100 }
      else if (optimized > baseline) { printf "%.1f%% more", (optimized / baseline - 1) * 100 }
      else { print "no change" }
    }
  ')"
  printf '  %-23s %s\n' "${lookup_targets[$target_index]}" "${change}"
done
