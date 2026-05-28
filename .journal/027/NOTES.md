---
id: 027
title: Session kickoff
started: 2026-05-27
---

## 2026-05-27 15:27 — Kickoff
Goal for the session: Start a fresh YACD journal session, load the current project context, and wait for the concrete implementation or review request.
Current state of the world: `master` is clean at `5b3185a` (`fix(cardanodbsync): reject accepted placement handoffs (#46)`). The personal journal branch `journal/jmgilman` is clean and up to date after session 026. Recent closed work completed the post-refactor manual functional test pass, landed db-sync `primarySidecar` placement, and then tightened the contract by rejecting post-acceptance placement changes after functional testing exposed unsafe topology migration.
Plan: Keep this session log updated at meaningful checkpoints. Once the user gives the actual task, select or create the appropriate implementation Worktrunk worktree, start the local dev stack for implementation work unless waived, and proceed in small verified slices.

## 2026-05-27 15:30 — Public network exploration
Goal for the checkpoint: Explore what it would take to support non-local CardanoNetwork resources for public networks such as preview, preprod, and mainnet.
Current state of the world: The CRD already exposes `spec.mode: public`, `spec.public.profile: preprod|preview|mainnet|custom`, and custom ConfigMap/Secret config sources, but `primaryWorkloadBuilder.localnetSpec` rejects non-local mode. The primary Deployment, artifact publisher, status identity, artifact schema, db-sync planner inputs, CLI developer config, and faucet path all still assume generated localnet artifacts and a localnet fingerprint. Upstream public profile files are published by the Cardano Operations Book; preview and mainnet are currently compatible with cardano-node 11.0.1, while preprod currently advertises 10.6.2 compatibility.
Plan: Recommend a small first slice: support preview public passive node with Ogmios and Kupo, no faucet, no custom profile, and no db-sync/public mainnet smoke yet. Then extend artifact/status contracts so CardanoDBSync can consume public profiles safely, followed by mainnet sizing/Mithril/bootstrap work.

## 2026-05-27 15:32 — Public network source correction
Correction: A direct re-open of the Cardano Operations Book environment pages before final assessment showed preview, preprod, and mainnet all advertising compatibility with cardano-node 11.0.1. Treat the earlier note that preprod advertised 10.6.2 as stale/conflicting source output; the implementation should still resolve profile compatibility from the checked-in profile data rather than from a hard-coded assumption.

## 2026-05-27 15:41 — Public network implementation slices
Goal for the checkpoint: Capture the four rough implementation slices for non-testnet/public CardanoNetwork support so the next work can stay agile and avoid over-specifying the final architecture up front.
Current state of the world: Public mode is an intended API shape but not a runtime path yet. The implementation should prove the smallest useful path first, then expand through real feedback.
Plan:
1. Slice 1: Preview passive node plus Ogmios only. Add a public-profile planner, render a passive public node without local pool credentials or `cardano-testnet create-env`, publish public profile identity/status/artifacts, and keep faucet disabled/rejected for public mode.
2. Slice 2: Curated preprod/mainnet profiles plus custom profile source. Extend profile resolution to all three public profiles and then ConfigMap/Secret-backed custom bundles, with profile/artifact fingerprint immutability on the primary PVC so profile drift degrades instead of silently mutating live chain state.
3. Slice 3: Public db-sync dedicated follower. Teach CardanoDBSync to consume public profile artifacts, set the right `NetworkName` and `RequiresNetworkMagic` values, keep `primarySidecar` local-only for now, and validate the dedicated follower path against a public profile.
4. Slice 4: Practical mainnet support. Add the operational pieces that make mainnet usable rather than merely accepted by the API: realistic storage/resource defaults, explicit mainnet warnings or opt-ins if needed, and a bootstrap/snapshot path such as Mithril before claiming mainnet is a good developer experience.

## 2026-05-27 16:02 — Implementation worktree ready
Goal for the checkpoint: Start implementation of slice 1 in an isolated Worktrunk worktree.
Current state of the world: Created `feat/public-preview-network` at `/Users/josh/code/meigma/yacd/.wt/feat-public-preview-network` from `master`. Ran `moon run root:dev-up` from that worktree; it completed successfully, leaving Tilt running at `http://localhost:10350/` with logs under `.run/yacd-dev/tilt.log`.
Plan: Implement the public preview planner/runtime path, keep the dev stack running through validation, and record later test/smoke results here.

## 2026-05-27 16:29 — Slice 1 implemented
Goal for the checkpoint: Record the implementation and proof for public preview CardanoNetwork support.
Current state of the world: Committed `e53c14b` on `feat/public-preview-network` (`feat(cardanonetwork): support public preview nodes`). The implementation adds an embedded official preview profile planner, renders public preview as a passive node plus Ogmios with no localnet init/pool credentials, publishes mode-neutral network artifacts/status, rejects public faucet/Kupo and unsupported profiles, and adds `examples/public-preview/yacd.yaml`.
Verification: `moon run root:generate`, `moon run root:test`, `moon run root:check`, `git diff --check`, and `git diff --cached --check` passed. Runtime smoke against the active Kind/Tilt stack passed with `go run ./cli/cmd/yacd deploy -f examples/public-preview/yacd.yaml --wait --timeout 10m`; observed `mode=public`, `profile=preview`, `networkMagic=2`, ready node/Ogmios conditions, verified artifacts, and published node/Ogmios endpoints. The temporary `yacd-smoke` namespace was deleted afterward; the dev stack remains running for the session.
Plan: Use this branch for review or follow-up slice 1 polish. Public db-sync, preprod/mainnet/custom profiles, snapshots/Mithril, and mainnet sizing remain out of scope for this slice.

## 2026-05-27 17:10 — Slice 2 implemented
Goal for the checkpoint: Record implementation and proof for curated preprod/mainnet profiles plus custom ConfigMap/Secret public profile sources.
Current state of the world: Committed and pushed `79d76ab` on `feat/public-preview-network` (`feat(cardanonetwork): support public profiles and custom sources`). The implementation extends the public profile planner to preview, preprod, mainnet, and custom bundles; vendors preprod/mainnet Operations Book assets; reads custom ConfigMap/Secret sources in the reconciler; watches/indexes referenced custom sources; preserves passive node plus Ogmios behavior; keeps public Kupo/faucet rejected; and adds examples for preprod and custom source configs.
Verification: `moon run root:generate`, `moon run root:test`, `moon run root:check`, `git diff --check`, and `git diff --cached --check` passed. CLI dry-runs passed for custom (`examples/public-custom/yacd.yaml`) and a temporary mainnet developer config. Runtime smoke against Kind/Tilt passed with `go run ./cli/cmd/yacd deploy -f examples/public-preprod/yacd.yaml --wait --timeout 10m`; observed `mode=public`, `profile=preprod`, `networkMagic=1`, fingerprint `142535876501fd7d3b40d940379c6df8ac7a9865f3fa0a4ce50fb3697276cb45`, verified artifacts, node/Ogmios endpoints, and a primary Deployment with only `cardano-node` and `ogmios`. The temporary `yacd-smoke` namespace was deleted afterward; the dev stack remains running.
Plan: Next public-network slice is CardanoDBSync dedicated follower consumption of public artifacts; practical mainnet sizing/bootstrap remains a later slice.

## 2026-05-27 17:59 — Slice 3 implemented
Goal for the checkpoint: Record implementation and proof for public CardanoDBSync dedicated-follower consumption of CardanoNetwork public artifacts.
Current state of the world: Committed and pushed `be7913a` on `feat/public-preview-network` (`feat(cardanodbsync): support public dedicated followers`). The implementation adds a typed `connection.json` consumer under the network-artifact boundary, validates artifact identity against CardanoNetwork status, feeds `NetworkName`, `RequiresNetworkMagic`, and node-to-node endpoint identity into the db-sync planner, keeps local compatibility, accepts public preview/preprod/custom only for `dedicatedFollower`, rejects public mainnet and public `primarySidecar`, and adds `examples/public-preprod/cardanodbsync-managed-postgres.yaml`.
Verification: `moon run root:generate`, `moon run root:test`, `moon run root:check`, and `git diff --check` passed. Runtime smoke against Kind/Tilt passed by deploying `examples/public-preprod/yacd.yaml` with `yacd deploy --wait`, applying the new public preprod managed-Postgres CardanoDBSync example, and observing managed Postgres available, DB Sync Deployment available, follower and db-sync containers ready, metrics Service published, `PostgresReady=True`, `DBSyncReady=True`, and `Synced=False/PostgresSchemaPending`. The temporary `yacd-smoke` namespace was deleted afterward; the dev stack remains running.
Plan: Slice 4 remains the practical mainnet work: sizing/resource defaults, stronger mainnet UX warnings or opt-ins if needed, and bootstrap/snapshot support such as Mithril before treating mainnet as operationally comfortable.

## 2026-05-27 18:38 — Resume
Goal for the checkpoint: Resume session 027 and start slice 4 implementation.
Current state of the world: The active implementation branch is `feat/public-preview-network` at `be7913a`, clean and pushed after slices 1-3. Slice 4 is now the practical mainnet support prototype: make mainnet explicit and opt-in, require Mithril bootstrap for CardanoNetwork mainnet, add realistic mainnet storage/resource defaults, keep CardanoDBSync mainnet rejected, and validate through generated CRDs plus unit/check tasks rather than a full live mainnet bootstrap.
Plan: Implement slice 4 in the existing implementation worktree, keep the work scoped to CardanoNetwork mainnet/Ogmios bootstrap and CLI opt-in behavior, and record verification results after the code and tests are complete.

## 2026-05-27 18:58 — Slice 4 implemented
Goal for the checkpoint: Record implementation and proof for practical mainnet CardanoNetwork support.
Current state of the world: The implementation branch `feat/public-preview-network` now has a validated Slice 4 diff staged in part for generated API/CRD verification. The slice adds `spec.public.bootstrap.mithril`, requires it for public mainnet, vendors release-mainnet Mithril verification keys, renders a Mithril bootstrap init container, defaults mainnet node storage/resources to `500Gi` and `cpu=2`/`memory=24Gi`, rejects explicit mainnet node storage below `300Gi`, adds `yacd deploy --allow-mainnet`, warns on mainnet dry-runs without the flag, and keeps public mainnet CardanoDBSync rejected until a later follower bootstrap or public sidecar slice.
Verification: `moon run root:generate`, `moon run root:test`, `moon run root:check`, and `git diff --check HEAD` passed. `root:check` was run after staging the API and generated CRD/deepcopy files because its generated-artifacts guard compares the working tree against the index for `api` and `charts/yacd/crds`.
Plan: Stage the full implementation diff, commit it on `feat/public-preview-network`, push the branch, and leave full live mainnet bootstrap as an explicit long-running manual proof rather than the default acceptance test.
