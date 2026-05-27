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
