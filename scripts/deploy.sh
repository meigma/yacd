#!/usr/bin/env bash
set -euo pipefail

release="${HELM_RELEASE:-template-k8s}"
namespace="${HELM_NAMESPACE:-template-k8s-system}"
image="${IMG:-}"

kubectl apply -f charts/template-k8s/crds

args=(
  upgrade
  --install
  "$release"
  charts/template-k8s
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
