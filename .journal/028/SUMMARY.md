---
id: 028
title: Public db-sync primary sidecar
date: 2026-05-27
status: complete
repos_touched: [yacd]
related_sessions: [025, 026, 027]
---

## Goal
Continue the public-network db-sync work from sessions 025-027 by enabling
`CardanoDBSync.spec.placement.mode: primarySidecar` for public non-mainnet
networks, proving it against a public test network, and assessing what remains
for public mainnet db-sync support.

## Outcome
The goal was met. PR #48 was squash-merged, local `master` was fast-forwarded
to the merge commit, the implementation worktree and remote feature branch were
removed, and the local YACD dev stack was stopped.

## Key Decisions
- Allow public `primarySidecar` only for preview, preprod, and custom profiles
  because those profiles can use the existing public node path without a
  mainnet-scale db-sync bootstrap story.
- Keep public mainnet db-sync rejected because mainnet needs a db-sync database
  bootstrap/restore path, not only the existing Cardano node Mithril bootstrap.
- Favor a future mainnet v1 around `primarySidecar` plus managed Postgres
  snapshot restore because it reuses the already bootstrapped primary node
  socket and avoids adding a second mainnet follower node bootstrap path.
- Keep external Postgres restore out of the first mainnet slice because YACD
  does not own database emptiness, permissions, or destructive restore safety.

## Changes
- `internal/controller/cardanodbsync/public_network.go` - accepts
  `dedicatedFollower` and `primarySidecar` for non-mainnet public networks and
  keeps mainnet rejected before workload apply.
- `internal/controller/cardanodbsync/primary_sidecar.go` and call sites -
  replaced the local-only primary-sidecar network gate with a local plus
  public non-mainnet gate.
- `internal/controller/cardanodbsync/controller_test.go` - covers public
  preview primary-sidecar material application, absence of dedicated follower
  resources, and mainnet rejection.
- `internal/controller/cardanonetwork/controller_test.go` - covers public
  preview sidecar attachment into the primary Pod.
- `examples/public-preview/cardanodbsync-primary-sidecar-managed-postgres.yaml`
  - adds a preview primary-sidecar example with managed Postgres and lightweight
  db-sync settings.
- `DESIGN.md` - documents the public non-mainnet primary-sidecar support and
  the continuing mainnet db-sync gap.

## Validation
- `KUBEBUILDER_ASSETS="$(setup-envtest use 1.35.x -p path)" go test ./internal/controller/cardanodbsync ./internal/controller/cardanonetwork`
- `moon run root:test`
- `moon run root:check`
- `git diff --check`
- Manual Kind/Tilt public preview smoke: deployed
  `examples/public-preview/yacd.yaml`, applied the new primary-sidecar
  `CardanoDBSync`, verified the primary Pod included `cardano-db-sync`,
  verified no dedicated db-sync Deployment or follower PVC existed, queried
  Ogmios tip successfully, and observed `66162` db-sync `block` rows through a
  temporary psql probe.

## Open Threads
- Public mainnet db-sync remains unsupported. The likely next slice is a
  managed-Postgres-only db-sync snapshot restore path for public mainnet
  `primarySidecar`.
- A mainnet restore prototype should be imperative first: restore one official
  upstream db-sync snapshot into YACD-owned Postgres/state PVCs, attach db-sync
  to a Mithril-bootstrapped mainnet primary node, and verify it continues from
  the restored block.
- Current YACD db-sync storage defaults are intentionally small for local/dev
  usage and are not suitable for mainnet snapshot restore.

## References
- PR #48: https://github.com/meigma/yacd/pull/48
- Upstream db-sync 13.7.1.0 release notes:
  https://github.com/IntersectMBO/cardano-db-sync/releases/tag/13.7.1.0
- Upstream db-sync state snapshot docs:
  https://github.com/IntersectMBO/cardano-db-sync/blob/13.7.1.0/doc/state-snapshot.md
- Mainnet db-sync snapshot index:
  https://update-cardano-mainnet.iohk.io/cardano-db-sync/index.html#13.7/
