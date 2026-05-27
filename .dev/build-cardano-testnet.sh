#!/usr/bin/env bash
set -euo pipefail

# Build the cardano-testnet tools image from local source so the dev stack
# picks up publisher changes (e.g. genesis hash enrichment) that the
# published cardano-testnet tag does not yet contain. Tilt must set
# EXPECTED_REF.

: "${EXPECTED_REF:?Tilt must set EXPECTED_REF for the cardano-testnet image build}"

repo_root="$(cd "$(dirname "$0")/.." && pwd)"

docker build \
  -f "${repo_root}/containers/cardano-testnet/Dockerfile" \
  -t "$EXPECTED_REF" \
  "${repo_root}/containers/cardano-testnet"
