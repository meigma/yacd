#!/usr/bin/env bash
set -euo pipefail

# Build the cardano-tools utility image from local source so the dev stack can
# stage network artifacts with post-release tool changes that the published
# cardano-tools tag does not yet contain. Tilt must set EXPECTED_REF and is
# responsible for loading the built image into the kind cluster.
#
# Unlike build-cardano-testnet.sh, the cardano-tools Dockerfile builds the
# yacd-cardano-tools binary from the ROOT Go module, so the docker build context
# is the repository root (not the container subdirectory).

: "${EXPECTED_REF:?Tilt must set EXPECTED_REF for the cardano-tools image build}"

repo_root="$(cd "$(dirname "$0")/.." && pwd)"

docker build \
  -f "${repo_root}/containers/cardano-tools/Dockerfile" \
  -t "$EXPECTED_REF" \
  "${repo_root}"
