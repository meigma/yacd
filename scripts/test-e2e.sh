#!/usr/bin/env bash
set -euo pipefail

kind_bin="${KIND:-kind}"
command -v "$kind_bin" >/dev/null

cluster="${KIND_CLUSTER:-template-k8s-test-e2e}"
manager_image="${IMG:-example.com/template-k8s:v0.0.1}"
kubeconfig_dir="$(mktemp -d)"
kubeconfig="$kubeconfig_dir/kubeconfig"
created=0

cleanup() {
  status="$?"
  if [ "$created" -eq 1 ]; then
    "$kind_bin" delete cluster --name "$cluster"
  fi
  rm -rf "$kubeconfig_dir"
  exit "$status"
}
trap cleanup EXIT

if "$kind_bin" get clusters | grep -qx "$cluster"; then
  echo "Using existing Kind cluster $cluster"
else
  created=1
  "$kind_bin" create cluster --name "$cluster"
fi

"$kind_bin" export kubeconfig --name "$cluster" --kubeconfig "$kubeconfig"
export KUBECONFIG="$kubeconfig"

docker build -t "$manager_image" .
"$kind_bin" load docker-image "$manager_image" --name "$cluster"

KIND="$kind_bin" KIND_CLUSTER="$cluster" IMG="$manager_image" KUBECTL_KUBERC="${KUBECTL_KUBERC:-false}" \
  chainsaw test --config test/chainsaw/chainsaw-config.yaml test/chainsaw
