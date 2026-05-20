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

## 2026-05-20 16:40 — CardanoNetwork CRD draft
Created Worktrunk branch `feat/cardano-network-crd` and drafted the first
`CardanoNetwork` API package. The CRD uses `spec.mode: local|public`, requires
exactly one matching `spec.local` or `spec.public` block, models
`public.profile: preprod|preview|mainnet|custom`, and gives custom public
profiles a typed `configSource` union for ConfigMap, Secret, OCI, or HTTP
bundles. The local block captures the first practical network knobs:
network magic, era, timing, pool topology/default economics, genesis profile,
security/max-supply/delegated-supply/protocol-version, shared node settings,
and default Ogmios chain API settings.

Generated deepcopy code and the Helm CRD under `charts/yacd/crds`, registered
the API type with the manager scheme, updated the foundation envtest to assert
the type registration, and refreshed README current state. Verification passed:
`moon run root:generate`, `moon run root:check`, `moon run root:test`, and
`git diff --check`.

## 2026-05-20 16:54 — Custom profile source narrowed
Applied review feedback to keep custom public profile sources in-cluster only
for the first CRD pass. Removed OCI and HTTP source variants, removed the
custom `LocalObjectReference` wrapper, and changed `configSource` to accept
exactly one same-namespace `corev1.LocalObjectReference` for either
`configMapRef` or `secretRef`. Because Kubernetes' built-in
`LocalObjectReference` allows an empty name for compatibility, the CRD now adds
a CEL rule requiring the selected reference name to be non-empty.

Committed the adjustment on `feat/cardano-network-crd` as `9cabf93`
(`refactor(api): narrow custom profile sources`). Verification after the
commit passed: `moon run root:check`, `moon run root:test`, and
`git diff --check`.

## 2026-05-20 16:57 — CRD PR opened and CI verified
Pushed `feat/cardano-network-crd` to origin and opened PR #3:
<https://github.com/meigma/yacd/pull/3>. PR title is
`feat(api): add CardanoNetwork CRD draft` and the head SHA is
`9cabf9303d02670481fcb4ca0db1c0f63b1c4c6c`.

Verified GitHub checks with `gh pr checks 3 --watch --fail-fast` and a final
`gh pr view 3 --json ...` read. Active checks passed: `ci` succeeded in about
1m2s and Kusari Inspector succeeded in about 21s. Release dry-run jobs were
reported as skipped for this PR event. GitHub reports the PR merge state as
`CLEAN` and mergeable.
