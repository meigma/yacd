---
id: 015
title: CardanoDBSync progress probes
date: 2026-05-24
status: complete
repos_touched: [yacd]
related_sessions: [011, 012, 013, 014]
---

## Goal
Assess phase 6 progress, then complete the runtime proof for `CardanoDBSync`
by adding efficient db-sync progress probing and honest sync/ready status.

## Outcome
The goal was met. PR #31 merged the bounded Postgres/Ogmios runtime probe,
populated `status.sync`, made `PostgresReady`, `Synced`, and aggregate `Ready`
reflect runtime state, and extended the Kind smoke to prove db-sync inserts
blocks and publishes node-tip/lag status. The prerequisite `cardano-testnet`
tools image release `11.0.1-yacd.4` also landed during the session so real
installs use the hash-enriching publisher image rather than a locally rebuilt
old tag.

## Key Decisions
- Shell out to `cardano-cli` for genesis hashes -> it is the canonical release
  tool already present in the YACD tools image, and using it avoids vendoring or
  reimplementing Cardano genesis hash internals for this prototype.
- Keep publisher hash enrichment in `publisher/internal/artifacts` -> the
  Cobra command remains orchestration code while file mutation and command
  invocation are unit-testable package behavior.
- Probe Postgres before db-sync container readiness -> bad database wiring is a
  common reason db-sync never becomes ready, so the controller should surface
  that directly instead of hiding it behind workload readiness.
- Gate full sync/lag comparison on healthy workloads and an Ogmios endpoint ->
  database progress can be reported independently, while `Synced=True` requires
  both indexed progress and a node tip.
- Keep Chainsaw bounded -> the smoke asserts sync-status publication and at
  least one indexed block, while controller tests cover the strict near-tip
  `Synced=True` threshold.

## Changes
- `internal/controller/cardanodbsync/runtime_probe.go` - added bounded pgx
  Postgres probing and Ogmios tip comparison without pod execs, log scraping,
  goroutines, or broad SQL scans.
- `internal/controller/cardanodbsync/status.go` and controller tests - wired
  `status.sync`, early `PostgresReady`, `Synced`, aggregate `Ready`, runtime
  requeues, and no-op status patch behavior.
- `internal/controller/cardanodbsync/workload_builder.go` - tightened pgpass
  handling and pod security details needed by the live smoke.
- `containers/cardano-testnet/publisher/internal/artifacts` - added active
  publisher-side configuration hash enrichment using a narrow `cardano-cli`
  adapter.
- `internal/controller/cardanonetwork/init_container.go`,
  `containers/cardano-testnet/Dockerfile`, and `.dev/scripts/test-e2e.sh` -
  moved defaults and local e2e to `ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.4`.
- `test/chainsaw/manager-smoke/chainsaw-test.yaml` - extended the installed
  operator smoke to require db-sync progress, node-tip/lag publication, and a
  live in-cluster `psql` proof that `block` has rows.

## Open Threads
- The CLI/devconfig path for creating `CardanoDBSync` resources remains future
  phase-6 polish.
- The sync lag threshold is still a package constant for the prototype.
- The localnet smoke accepts `SyncLagging` because the tiny local chain can
  leave db-sync in a near-tip client-index migration after the first block;
  `Synced=True` remains covered at the controller/probe layer.

## References
- PR #25: https://github.com/meigma/yacd/pull/25
- PR #27: https://github.com/meigma/yacd/pull/27
- PR #29: https://github.com/meigma/yacd/pull/29
- PR #30: https://github.com/meigma/yacd/pull/30
- PR #31: https://github.com/meigma/yacd/pull/31
- Release tag: `cardano-testnet/v11.0.1-yacd.4`
