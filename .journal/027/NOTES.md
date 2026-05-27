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
