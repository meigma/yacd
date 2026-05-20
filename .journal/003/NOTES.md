---
id: 003
title: First YACD environment prototype
started: 2026-05-20
---

## 2026-05-20 16:03 — Kickoff
Goal for the session: Start a new YACD journal session, refresh on `DESIGN.md`
and `.journal/PLAN.md`, then wait for the next implementation request.

Current state of the world: Session 001 added the initial YACD design and
component plan. Session 002 completed the repository branding/foundation slice:
the template `NginxDeployment` API and controller are gone, the manager-only
operator shell still builds/tests/deploys, and the repo intentionally has no
custom APIs or reconcilers yet. The next product slice is expected to introduce
the first real YACD primary environment CRD and controller rather than a
placeholder API.

Plan: Prime this session, reread the current design and rough prototype plan,
then proceed with the next small implementation slice once requested.

## 2026-05-20 16:10 — CRD shape research
Investigated `cardano-foundation/cardano-ignite` main at `7a3c6ac`,
`bloxbean/yaci-devkit` main at `d42fe55`, and local
`~/work/phoenix/test/cli`.

Findings: Ignite exposes broad testnet/topology and raw genesis/node config
knobs, including pool count/cost/margin/pledge, delegated supply, system start,
network magic, slot/epoch/security parameters, protocol version, tracing, peer
targets, and optional db-sync/Blockfrost/Yaci Store/observability services.
Yaci DevKit is more developer-oriented: `create-node` centers name, ports,
slot length, block time, epoch length, era, genesis profile, key generation,
multi-node rollback mode, and service toggles for Yaci Store/Ogmios/Kupo; its
`node.properties` contains a much larger genesis/protocol override universe.
Phoenix is the closest local schema precedent: `network.enabled`, local/public
chain mode, node version/port, magic, one-pool default, epoch/slot length,
Ogmios/Kupo, optional db-sync, and wallet/walletSet startup flows.

Proposal direction: make the first YACD CRD a namespaced `CardanoNetwork` under
the repo domain, keep it local-devnet-only for the first slice, include only the
small runtime/genesis/topology/storage/API knobs needed to reconcile one node
plus default Ogmios, and defer db-sync, wallet generation, faucet/topup, Yaci
Store, Kupo, Blockfrost, raw node-config overrides, and multi-node rollback
mode until their own slices have working evidence.
