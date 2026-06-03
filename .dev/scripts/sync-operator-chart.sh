#!/usr/bin/env bash
# Syncs the YACD operator Helm chart into the in-package copy the CLI embeds and
# renders at install time (cli/internal/operator/ssa/chart). `//go:embed` cannot
# traverse `..`, and the chart lives at the repo root while the embedding package
# lives under cli/internal/operator/ssa, so the chart is copied in-tree and
# drift-guarded by root:check (git diff of the copy vs charts/yacd).
#
# The copy is the single source of truth for the runtime render: the embedded
# chart's appVersion (stamped onto every object as app.kubernetes.io/version) is
# the operator VERSION the CLI reconciles against, and the image digests the
# default install pins live in operator.Default() as Go consts. When cutting a
# new operator release, bump charts/yacd/Chart.yaml appVersion AND the two
# digests in operator.Default(), then re-sync.
#
# The copy is deterministic: it is a verbatim, delete-semantics mirror of
# charts/yacd, so re-running it on a clean tree produces no diff.
set -euo pipefail

chart="charts/yacd"
out="cli/internal/operator/ssa/chart"

rm -rf "$out"
cp -R "$chart" "$out"

echo "synced operator chart $chart -> $out"
