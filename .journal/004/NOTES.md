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
