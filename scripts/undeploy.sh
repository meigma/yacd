#!/usr/bin/env bash
set -euo pipefail

helm uninstall "${HELM_RELEASE:-yacd}" \
  --namespace "${HELM_NAMESPACE:-yacd-system}" \
  --ignore-not-found

shopt -s nullglob
crds=(charts/yacd/crds/*.yaml)
if [ "${#crds[@]}" -gt 0 ]; then
  kubectl delete --ignore-not-found=true -f charts/yacd/crds
fi
