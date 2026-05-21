---
id: 004
title: cardano-testnet tools image and init fragment
date: 2026-05-20
status: complete
repos_touched: [yacd]
related_sessions: [003]
---

## Goal
Continue the localnet runtime slice from session 003 by turning
`localnet.Plan` into the first Kubernetes init-container fragment for
`cardano-testnet create-env`.

## Outcome
The goal was met, with one versioning correction along the way. The session
landed a YACD-owned `cardano-testnet` tools image, integrated it into the
Release Please and GHCR publication flow, reset the first image release to the
packaging-aware `11.0.1-yacd.1` tag, and added the controller helper that
converts a `localnet.Plan` into the init-container fragment.

## Key Decisions
- Package `cardano-testnet` in a narrow YACD tools image, because the published
  Cardano node image did not provide the `cardano-testnet` binary contract
  needed by the Kubernetes init path.
- Keep the image sourced from official IntersectMBO `cardano-node` release
  artifacts instead of building Cardano from source or maintaining a broad
  custom runtime image.
- Version the image as `<upstream-cardano-version>-yacd.N`, because the base
  version should track the bundled upstream binary while `yacd.N` tracks
  wrapper and packaging changes.
- Keep Release Please as the only release mechanism for the image. The
  component release train now emits tags such as
  `cardano-testnet/v11.0.1-yacd.1`.
- Move the restart-safe idempotency wrapper into the image rather than
  injecting a long shell script into Kubernetes manifests.
- Keep `internal/cardano/localnet` Kubernetes-free. The new init-container
  construction lives in the `cardanonetwork` controller package.

## Changes
- `containers/cardano-testnet/Dockerfile` - added the pinned, multi-arch tools
  image that downloads and verifies official Cardano release artifacts.
- `containers/cardano-testnet/yacd-cardano-testnet-init` - added the
  image-owned restart-safe wrapper for `cardano-testnet create-env`.
- `release-please-config.json` and `.release-please-manifest.json` - added the
  independent `containers/cardano-testnet` component and reset it to the
  prerelease-style `11.0.1-yacd.N` train.
- `.github/workflows/release-cardano-testnet.yml` - added the dedicated
  multi-arch GHCR image release, smoke-test, provenance/SBOM, and GitHub
  attestation workflow.
- `.github/workflows/release-dry-run.yml` - added a cardano-testnet image
  rehearsal path that builds and smoke-tests without publishing.
- `.github/dependabot.yml` and `.github/repository-settings.toml` - included
  the new container path and dry-run check in repository maintenance.
- `internal/controller/cardanonetwork/localnet_init_container.go` - added the
  unexported helper that renders the init-container fragment from
  `localnet.Plan`.
- `internal/controller/cardanonetwork/localnet_init_container_test.go` - added
  focused tests for image/tag selection, args/env propagation, manifest JSON,
  mounts, security context, and validation errors.

## Open Threads
- Build the next Kubernetes workload layer around this fragment: PVC/shared
  state, node container shape, StatefulSet, and Services.
- Make the image packaging revision less hard-coded once there is a concrete
  controller configuration or dependency-management story for packaged tool
  images.
- Root Release Please PR #7 remains open separately for `yacd` `1.0.0`; it was
  not part of this image/component release.
- Decide later whether the root Release Please PR should be resolved, closed,
  or rebased after the root release strategy is clearer.

## References
- PR #5: <https://github.com/meigma/yacd/pull/5>
- PR #6: <https://github.com/meigma/yacd/pull/6>
- PR #8: <https://github.com/meigma/yacd/pull/8>
- PR #9: <https://github.com/meigma/yacd/pull/9>
- Release: <https://github.com/meigma/yacd/releases/tag/cardano-testnet/v11.0.1-yacd.1>
- Release workflow: <https://github.com/meigma/yacd/actions/runs/26238667694>
- `.journal/003/SUMMARY.md`
