---
id: 004
title: Session kickoff
started: 2026-05-20
---

## 2026-05-20 19:22 — Kickoff
Goal for the session: Start a new YACD journal session and wait for the developer's concrete implementation request.
Current state of the world: `master` is at `86da9a4` (`feat(localnet): build CardanoNetwork plan pipeline (#4)`). Session 003 closed after landing the `CardanoNetwork` API, `internal/cardano/localnet`, the read-only controller adapter, and the managed Kind/Tilt development stack lifecycle. The next known product thread is the Kubernetes workload layer: init-container construction from `localnet.Plan`, PVC/shared-state mounting, cardano-node container shape, then StatefulSet/Service assembly. Status, Events, Ogmios, and public profile behavior remain deferred.
Plan: Keep the session ready for the user's actual request. Before implementation work, select or create an implementation Worktrunk worktree and run `moon run root:dev-up` from that worktree per `.session.md`.

## 2026-05-20 20:33 — cardano-testnet tools container
Created implementation branch `feat/cardano-testnet-container` and started the required Kind/Tilt dev stack with `moon run root:dev-up`. Added `containers/cardano-testnet/Dockerfile`, a local-buildable tools image that downloads official IntersectMBO `cardano-node` release artifacts for `11.0.1`, verifies the release checksum, extracts `cardano-testnet`, `cardano-cli`, and `cardano-node`, and packages them on `debian:bookworm-slim` with the expected `CARDANO_CLI` and `CARDANO_NODE` environment defaults.

Verification passed: native `docker build -t yacd-cardano-testnet:11.0.1 containers/cardano-testnet`, `docker run --rm yacd-cardano-testnet:11.0.1 version`, `docker run --rm yacd-cardano-testnet:11.0.1 create-env --num-pool-nodes 1 --testnet-magic 42 --epoch-length 500 --slot-length 0.1 --output /tmp/cardano-env`, explicit `docker build --platform linux/amd64 -t yacd-cardano-testnet:11.0.1-amd64 containers/cardano-testnet`, label inspection, and `git diff --check`. Implementation commit: `ca9a71e` (`build(cardano-testnet): add tools container`). No CI, release, chart, or controller wiring was added.

## 2026-05-20 20:42 — container dependency pinning
Tightened `containers/cardano-testnet/Dockerfile` after review feedback. The Dockerfile now pins the BuildKit frontend to `docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6`, pins both Debian stages to `debian:bookworm-slim@sha256:0104b334637a5f19aa9c983a91b54c89887c0984081f2068983107a6f6c21eeb`, and installs the fetch-stage tools as `ca-certificates=20230311+deb12u1` and `curl=7.88.1-10+deb12u14` instead of floating apt candidates.

Verification passed on the final Dockerfile: native `docker build -t yacd-cardano-testnet:11.0.1 containers/cardano-testnet`, `docker run --rm yacd-cardano-testnet:11.0.1 version`, `docker run --rm yacd-cardano-testnet:11.0.1 create-env --num-pool-nodes 1 --testnet-magic 42 --epoch-length 500 --slot-length 0.1 --output /tmp/cardano-env`, explicit `docker build --platform linux/amd64 -t yacd-cardano-testnet:11.0.1-amd64 containers/cardano-testnet`, and `git diff --check`. Implementation commit: `b836271` (`build(cardano-testnet): pin container dependencies`).

## 2026-05-20 21:55 — release integration hard stop
Integrated the `cardano-testnet` tools container into the release flow on `feat/cardano-testnet-container`. Release Please now has a separate `containers/cardano-testnet` package configured for `cardano-testnet/vX.Y.Z` tags with `initial-version: 11.0.1`, and a dedicated tag-triggered workflow publishes `ghcr.io/meigma/yacd/cardano-testnet:<version>` with multi-arch BuildKit provenance/SBOM and GitHub-native attestation. The release dry-run workflow now includes a `Cardano Testnet Image Dry Run` required context, repository settings were updated for that check, and Dependabot now scans `containers/cardano-testnet`.

Kusari initially blocked PR #5 because the image ran as root, so the Dockerfile now creates fixed non-root UID/GID `10001`, owns `/state`, and runs `cardano-testnet` as that user. Verification passed: JSON/YAML parsing, `actionlint`, `git diff --check`, native and `linux/amd64` Docker builds, `version`, `create-env` writing to `/state`, explicit non-root/writable-state smoke, `moon run root:check`, and a Release Please dry run confirming a separate `cardano-testnet` `11.0.1` release PR candidate. Draft PR #5 (`build(cardano-testnet): add tools container`) is open at `https://github.com/meigma/yacd/pull/5`; CI and Kusari passed. This is hard stop 1 before merging the feature PR.

## 2026-05-20 22:01 — feature PR merged, release app settings blocker
After user approval, marked PR #5 ready and squash-merged it through GitHub into `master` as `5fc50dae32308ec1adb59f058e6d80fd6d20db6b`. The local `gh pr merge` command reported a local checkout conflict because `master` is already owned by the primary worktree, but GitHub confirms PR #5 is merged.

The post-merge Release Please workflow run `26206429171` failed before opening release PRs. Failure cause: the repository has no `MEIGMA_RELEASE_APP_ID` Actions variable and no `MEIGMA_RELEASE_APP_PRIVATE_KEY` Actions secret, so `actions/create-github-app-token` cannot mint the release app token. `meigma/template-k8s` has `MEIGMA_RELEASE_APP_ID=3342783`, which appears to be the matching app id, but the private key secret is not recoverable from another repository. Stop here before the generated Release Please PR because no release PR exists yet.

## 2026-05-20 22:17 — release app settings restored and Release Please PR opened
Used `op` to read the `meigma-release-please` item from the `Homelab` vault and set GitHub Actions settings on `meigma/yacd`: `MEIGMA_RELEASE_APP_ID`, `MEIGMA_RELEASE_APP_CLIENT_ID`, and `MEIGMA_RELEASE_APP_PRIVATE_KEY`. Reran failed Release Please run `26206429171`; it completed successfully.

Release Please opened separate PRs as expected: #6 (`chore(master): release cardano-testnet 11.0.1`) and #7 (`chore(master): release 1.0.0`). PR #6 modifies `.release-please-manifest.json` and adds `containers/cardano-testnet/CHANGELOG.md`. Checks on PR #6 passed, including `Cardano Testnet Image Dry Run`, both `cardano-testnet Image Platform Dry Run` jobs, CI, Kusari, and the existing release rehearsal jobs. This is hard stop 2 before merging the generated Release Please PR.

## 2026-05-20 22:24 — cardano-testnet image release verified
After user approval, squash-merged Release Please PR #6 into `master` as `071fa6fed78ea376315b9d08aaeb547022d336e4`. Release Please run `26207022838` completed successfully and created tag `cardano-testnet/v11.0.1` plus a draft GitHub release.

The tag-triggered `Release cardano-testnet Image` workflow run `26207027454` completed successfully. It built and pushed `linux/amd64` and `linux/arm64`, created the multi-platform manifest, smoke-tested the release image, and published the GitHub attestation. Verified GHCR image `ghcr.io/meigma/yacd/cardano-testnet:11.0.1` at digest `sha256:28043460b2c96878653530b53e832a949dec790f132ab70afb2a8adc137b0b9d` with `docker buildx imagetools inspect`, local `docker run --rm ghcr.io/meigma/yacd/cardano-testnet:11.0.1 version`, and `gh attestation verify oci://ghcr.io/meigma/yacd/cardano-testnet@sha256:28043460b2c96878653530b53e832a949dec790f132ab70afb2a8adc137b0b9d -R meigma/yacd --source-ref refs/tags/cardano-testnet/v11.0.1`. This is hard stop 3 before using the image in controller workload fragments. Root release PR #7 was not merged and is now conflicting on `.release-please-manifest.json` after the component release landed.

## 2026-05-20 22:42 — init-container fragment helper
Created implementation branch `feat/cardano-testnet-init-container` from updated `master` (`071fa6f`) and started the dev stack with `moon run root:dev-up`. Added controller-local helper `localnetCreateEnvInitContainer(plan localnet.Plan)` that converts a pure `localnet.Plan` into a `corev1.Container` init fragment for `ghcr.io/meigma/yacd/cardano-testnet:<version>`.

The fragment mounts `localnet-state` at `/state`, runs `/bin/sh -ec` as UID/GID `10001` with restricted security context and read-only root, passes the plan manifest as compact JSON, and wraps `cardano-testnet create-env` with restart-safe idempotency: matching manifest plus config exits 0, mismatched existing env refuses overwrite, and first run writes the manifest last. Verification passed: focused `go test ./internal/controller/cardanonetwork`, `moon run root:test`, `moon run root:check`, staged `git diff --check`, and a manual two-run Docker smoke against `ghcr.io/meigma/yacd/cardano-testnet:11.0.1` with a temp `/state` mount.
