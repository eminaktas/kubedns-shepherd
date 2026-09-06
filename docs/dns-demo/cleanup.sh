#!/usr/bin/env bash

set -Eeuo pipefail

cluster_name="${CLUSTER_NAME:-kubedns-shepherd-dns-demo}"

if ! command -v kind >/dev/null 2>&1; then
  echo "Required command not found: kind" >&2
  exit 1
fi

if kind get clusters | grep -Fxq "${cluster_name}"; then
  echo "Deleting Kind cluster ${cluster_name}"
  kind delete cluster --name "${cluster_name}"
else
  echo "Kind cluster ${cluster_name} does not exist"
fi
