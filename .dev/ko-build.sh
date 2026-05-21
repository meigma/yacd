#!/usr/bin/env bash
set -euo pipefail

: "${EXPECTED_REF:?Tilt must set EXPECTED_REF for the image build}"

export CGO_ENABLED="${CGO_ENABLED:-0}"

built_ref="$(ko build --local ./cmd | tail -n 1)"
docker tag "$built_ref" "$EXPECTED_REF"
