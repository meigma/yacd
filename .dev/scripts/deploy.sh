#!/usr/bin/env bash
set -euo pipefail

release="${HELM_RELEASE:-yacd}"
namespace="${HELM_NAMESPACE:-yacd-system}"
image="${IMG:-}"

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

if [ "${LOCAL_IMAGE:-false}" = "true" ]; then
  args+=(--set "image.pullPolicy=IfNotPresent")
fi

helm "${args[@]}"
