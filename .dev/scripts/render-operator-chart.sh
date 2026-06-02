#!/usr/bin/env bash
# Renders the YACD operator Helm chart into the manifests the CLI embeds and
# installs by server-side apply (cli/internal/operator/ssa). The render is
# deterministic at default chart values (Kyverno off, metrics/webhook TLS off),
# so re-running it on a clean tree produces no diff.
#
# Manager and faucet images are digest-pinned to the published release so the
# embedded install is reproducible and tamper-evident. The operator VERSION the
# CLI reconciles against is NOT set here — it comes from the chart's appVersion,
# stamped onto every object as app.kubernetes.io/version. When cutting a new
# operator release, bump charts/yacd/Chart.yaml appVersion AND update the two
# digests below to the newly published manager/faucet images, then re-render.
set -euo pipefail

# Published v0.1.1 image digests (ghcr.io/meigma/yacd, .../faucet).
MANAGER_DIGEST="sha256:5d53ca824dacad39c482dc93edfd2db4a65d5803f43dce5b18b1a7482b0f8e21"
FAUCET_DIGEST="sha256:826f8d52f0a4b0f607e2293cf72a8217de27700b5e5f1b35e1af86ef18fd3f66"

chart="charts/yacd"
out="cli/internal/operator/ssa/manifests/operator.yaml"

mkdir -p "$(dirname "$out")"

helm template yacd "$chart" \
  --namespace yacd-system \
  --include-crds \
  --no-hooks \
  --set-string "image.digest=${MANAGER_DIGEST}" \
  --set-string "faucet.image.digest=${FAUCET_DIGEST}" \
  > "$out"

echo "rendered operator manifests -> $out"
