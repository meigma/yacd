---
id: 025
title: CardanoDBSync placement and primary sidecar
date: 2026-05-27
status: complete
repos_touched: [yacd]
related_sessions: [022, 024]
---

## Goal
Explore and implement the first db-sync placement path that can eventually avoid duplicate Cardano nodes for public networks, while keeping public-network support itself out of scope. The implementation target was to add placement semantics, conflict detection, and a local-network `primarySidecar` runtime path without letting both controllers mutate the same primary Deployment.

## Outcome
Met. PR #45 merged `CardanoDBSync.spec.placement`, conflict detection, local-network primary-sidecar runtime material/status, CardanoNetwork sidecar composition, placement handoff safety, and review-driven boundary cleanup. `master` was fast-forwarded to the merge commit, the implementation worktree was removed, and the dev stack was stopped.

## Key Decisions
- `CardanoDBSync` owns db-sync database/config/pgpass/state/metrics/status, while `CardanoNetwork` remains the only controller that mutates the primary Pod -> avoids shared Deployment ownership.
- `CardanoNetwork` consumes a narrow `CardanoDBSync.status.placement.primarySidecar` contract rather than reading DB Sync-owned child resources -> keeps material readiness and rollout revision calculation inside the DB Sync controller.
- Multiple primary-sidecar claims attach none -> deterministic conflict semantics avoid hidden winner selection.
- Placement flips use two-phase handoff -> prevents briefly running dedicated and sidecar db-sync processes against the same state PVC/database.
- `primarySidecar` is local-network only for now -> public preprod/preview/mainnet node support can be added after the db-sync placement model is stable.

## Changes
- `api/v1alpha1/cardanodbsync_types.go` - added placement mode/spec and status contract fields for db-sync sidecar material.
- `internal/controller/cardanodbsync` - added placement gating, conflict handling, primary-sidecar material reconciliation, status publication, safe placement handoff, and sidecar readiness/status behavior.
- `internal/controller/cardanonetwork` - added DB Sync sidecar claim resolution, primary Pod composition from DB Sync status, `DBSyncAttachmentReady`, rollout annotation handling, and sidecar attach/remove tests.
- `internal/cardano/primarypod` - added shared primary Pod naming, labels, port defaults, port names, and port ownership rules for both controllers.
- `internal/ctrlkit/readiness` - added named-container readiness support used to attribute attached db-sync sidecar readiness separately from primary node readiness.
- `DESIGN.md` - documented that dedicated follower remains the default no-primary-restart path, while `primarySidecar` explicitly trades isolation for socket sharing.

## Open Threads
- Public-network `CardanoNetwork` support remains intentionally unimplemented.
- `primarySidecar` currently supports local networks only; public preprod/preview/mainnet sidecar placement still needs a future public-node/artifact slice.
- Reconcile/status duplication between dedicated and primary-sidecar db-sync paths was left for a future cleanup after this runtime path settles.

## References
- PR #45: https://github.com/meigma/yacd/pull/45
- Session 022: `.journal/022/SUMMARY.md`
- Session 024: `.journal/024/SUMMARY.md`
