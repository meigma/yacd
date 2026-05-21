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

## 2026-05-20 17:01 — PR merged
Squash-merged PR #3 on GitHub after rechecking that the head SHA was still
`9cabf9303d02670481fcb4ca0db1c0f63b1c4c6c` and that PR checks were green. The
merge commit on `master` is `f918623376744ad4a8eba3f574019f887318014a`.

`gh pr merge --squash --delete-branch` completed the GitHub merge but could not
delete the local branch while it was checked out in the Worktrunk worktree.
Recovered by fast-forwarding local `master` with `git pull --ff-only origin
master`, removing the integrated `feat/cardano-network-crd` worktree via
`wt remove -y --foreground --format=json feat/cardano-network-crd`, and deleting
the stale remote branch with `git push origin --delete feat/cardano-network-crd`.

Post-merge `CI` on `master` passed. The `Release Please` workflow failed before
project code ran because `actions/create-github-app-token` received an empty
`client-id`/deprecated `app-id` input, which points at missing repository
release-app configuration rather than the CRD merge itself.

## 2026-05-20 17:27 — Local Cardano package layering
Agreed to keep the first local-mode implementation focused on
`cardano-testnet` and to ignore public mode for now. The raw Cardano boundary
should generate a typed Go `TestnetPlan` rather than Kubernetes resources. That
plan should describe the `cardano-testnet create-env` invocation, output/state
paths, expected node config/topology/key paths, socket conventions, and a plan
hash for idempotency/reset detection.

The Kubernetes side should stay layered. A node workload-fragment layer
converts the `TestnetPlan` into the init container, cardano-node container,
volume mounts, probes, and volume-name requirements. A separate Ogmios
fragment converts Ogmios settings plus node runtime paths into the sidecar,
port, and readiness shape. A final resource assembly layer consumes those
fragments to build the owned `StatefulSet`, service(s), selector labels,
volume claim template, owner refs, and status endpoint inputs.

First-pass package direction: keep the pure Cardano plan under something like
`internal/cardano/localnet`, and keep the Kubernetes fragments/resource assembly
controller-adjacent at first. Split the Kubernetes fragments into separate
packages only once the seams prove useful. The agreed runtime flow is:
`CardanoNetwork spec -> localnet.TestnetPlan -> node fragment -> Ogmios
fragment -> StatefulSet + Service`.

## 2026-05-20 17:52 — Localnet plan package implemented
Created Worktrunk branch `feat/localnet-plan-package` from `master` and added
the pure Go `internal/cardano/localnet` package. The package keeps the first
runtime boundary Kubernetes/API-free and builds a deterministic
`cardano-testnet create-env` plan from normalized inputs: network magic, pool
count, slot/epoch timing, output paths, optional tool version, expected config
and manifest paths, and a SHA-256 fingerprint/manifest for later init-container
idempotency checks.

Implementation intentionally models only the `cardano-testnet create-env`
input contract. It defers CRD mapping, init container construction, workload
resources, direct `cardano-node` key/runtime paths, and era/genesis tuning to
later slices. Verification passed on the feature branch:
`go test ./internal/cardano/localnet`, `moon run root:test`,
`moon run root:check`, and `git diff --check`.
