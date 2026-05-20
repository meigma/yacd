#!/usr/bin/env bash
set -euo pipefail

helm uninstall "${HELM_RELEASE:-template-k8s}" \
  --namespace "${HELM_NAMESPACE:-template-k8s-system}" \
  --ignore-not-found

kubectl delete --ignore-not-found=true -f charts/template-k8s/crds
