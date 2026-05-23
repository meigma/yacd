#!/usr/bin/env bash
set -euo pipefail

release="${HELM_RELEASE:-yacd}"
namespace="${HELM_NAMESPACE:-yacd-system}"
image="${IMG:-}"
faucet_image="${FAUCET_IMG:-}"

shopt -s nullglob
crds=(charts/yacd/crds/*.yaml)
if [ "${#crds[@]}" -gt 0 ]; then
  kubectl apply -f charts/yacd/crds
fi

args=(
  upgrade
  --install
  "$release"
  charts/yacd
  --namespace
  "$namespace"
  --create-namespace
)

if [ -n "$image" ]; then
  if [[ "$image" == *@* ]]; then
    args+=(--set-string "image.repository=${image%@*}")
    args+=(--set-string "image.digest=${image#*@}")
    args+=(--set-string "image.tag=")
  else
    last_segment="${image##*/}"
    if [[ "$last_segment" == *:* ]]; then
      args+=(--set-string "image.repository=${image%:*}")
      args+=(--set-string "image.tag=${image##*:}")
    else
      args+=(--set-string "image.repository=$image")
      args+=(--set-string "image.tag=")
    fi
    args+=(--set-string "image.digest=")
  fi
fi

if [ -n "$faucet_image" ]; then
  if [[ "$faucet_image" == *@* ]]; then
    args+=(--set-string "faucet.image.repository=${faucet_image%@*}")
    args+=(--set-string "faucet.image.digest=${faucet_image#*@}")
    args+=(--set-string "faucet.image.tag=")
  else
    last_segment="${faucet_image##*/}"
    if [[ "$last_segment" == *:* ]]; then
      args+=(--set-string "faucet.image.repository=${faucet_image%:*}")
      args+=(--set-string "faucet.image.tag=${faucet_image##*:}")
    else
      args+=(--set-string "faucet.image.repository=$faucet_image")
      args+=(--set-string "faucet.image.tag=")
    fi
    args+=(--set-string "faucet.image.digest=")
  fi
fi

if [ "${LOCAL_IMAGE:-false}" = "true" ]; then
  args+=(--set "image.pullPolicy=IfNotPresent")
fi

helm "${args[@]}"
