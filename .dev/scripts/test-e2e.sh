#!/usr/bin/env bash
set -euo pipefail

kind_bin="${KIND:-kind}"
command -v "$kind_bin" >/dev/null

cluster="${KIND_CLUSTER:-yacd-test-e2e}"
manager_image="${IMG:-example.com/yacd:v0.0.1}"
faucet_image="${FAUCET_IMG:-example.com/yacd-faucet:v0.0.1}"
cardano_testnet_image="ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.4"
cardano_tools_image="ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.0"
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
docker build -f services/faucet/Dockerfile -t "$faucet_image" .
docker build -f containers/cardano-testnet/Dockerfile -t "$cardano_testnet_image" containers/cardano-testnet
# cardano-tools builds from the repository ROOT context (its binary lives in the
# root Go module); the Dockerfile path is relative to that context.
docker build -f containers/cardano-tools/Dockerfile -t "$cardano_tools_image" .
"$kind_bin" load docker-image "$manager_image" --name "$cluster"
"$kind_bin" load docker-image "$faucet_image" --name "$cluster"
"$kind_bin" load docker-image "$cardano_testnet_image" --name "$cluster"
"$kind_bin" load docker-image "$cardano_tools_image" --name "$cluster"

KIND="$kind_bin" KIND_CLUSTER="$cluster" IMG="$manager_image" FAUCET_IMG="$faucet_image" KUBECTL_KUBERC="${KUBECTL_KUBERC:-false}" \
  chainsaw test --config test/chainsaw/chainsaw-config.yaml test/chainsaw
