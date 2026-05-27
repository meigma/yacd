---
id: 026
title: Primary sidecar manual functional testing
started: 2026-05-27
---

## 2026-05-27 13:25 — Kickoff
Goal for the session: continue from session 025 by manually function-testing the new `CardanoDBSync.spec.placement.primarySidecar` feature.
Current state of the world: `master` is at `8e77d3d` (`feat(cardanodbsync): support primary sidecar placement (#45)`). Session 025 closed with the implementation merged, the implementation worktree removed, and the dev stack stopped. Recent context from sessions 023-025 covers the CLI refactor, the post-refactor manual validation pass, and the primary-sidecar implementation.
Plan: prime the session first, then select or create an implementation worktree, start the local dev stack with `moon run root:dev-up`, and run focused manual tests around primary-sidecar attach, readiness, conflict, and handoff behavior.

## 2026-05-27 13:32 — Dev stack ready
Created Worktrunk branch/worktree `test/primary-sidecar-functional` from `origin/master` at `8e77d3d`. Ran `direnv allow && moon run root:dev-up` from `/Users/josh/code/meigma/yacd/.wt/test-primary-sidecar-functional`; it created `kind-yacd-dev`, started Tilt, and reported the YACD dev stack ready. Tilt UI is `http://localhost:10350/`; logs are in `/Users/josh/code/meigma/yacd/.run/yacd-dev/tilt.log`.

## 2026-05-27 13:34 — Baseline network ready
Created namespace `yacd-sidecar`, deployed local `CardanoNetwork/sidecar-net` with faucet enabled on port 8080, and waited for `Ready`, `NodeReady`, `ArtifactsReady`, `OgmiosReady`, `KupoReady`, and `FaucetReady` to become `True`. `DBSyncAttachmentReady` reports `DBSyncAttachmentNotRequested`, as expected before any `primarySidecar` claim exists.

## 2026-05-27 13:37 — Managed Postgres primary sidecar passed
Applied `CardanoDBSync/sidecar-managed` with `placement.mode=primarySidecar`, managed Postgres, Kind-safe db-sync config, and metrics port 9090. Verified managed Postgres children, db-sync ConfigMap/pgpass/state PVC/metrics Service, primary Deployment sidecar/init-container attachment, no dedicated `sidecar-managed-dbsync` Deployment, no follower PVC, and runtime status: `DBSyncAttachmentReady=True`, `NodeSocketReady=True`, `DBSyncReady=True`, `PostgresReady=True`, `Synced=True`. Independent `psql` probe against `sidecar-managed-postgres` returned `block rows: 80`. Evidence captured under `/tmp/yacd-sidecar-functional-20260527/managed`.

## 2026-05-27 13:40 — External Postgres primary sidecar passed
Deleted `sidecar-managed` and waited for the primary Deployment template and live Pods to remove `cardano-db-sync` before starting the next path. Created unmanaged `external-postgres` Secret/PVC/Service/Deployment, then applied `CardanoDBSync/sidecar-external` with `placement.mode=primarySidecar`, external DB reference, Kind-safe db-sync config, and metrics port 9090. Verified sidecar material, primary Deployment sidecar/init-container attachment, no dedicated `sidecar-external-dbsync` Deployment, no follower PVC, no YACD-managed Postgres resources, and runtime status: `DBSyncAttachmentReady=True`, `NodeSocketReady=True`, `DBSyncReady=True`, `PostgresReady=True`, `Synced=True`. Independent `psql` probe against `external-postgres` returned `block rows: 103`. Evidence captured under `/tmp/yacd-sidecar-functional-20260527/external`.

## 2026-05-27 13:49 — Negative and handoff scenarios
Validation and negative checks passed: the API server rejected `primarySidecar` plus `followerNode`, metrics port 8080 with faucet enabled degraded as `UnsupportedSpec` without attaching material, two primary-sidecar claims produced `PlacementConflict` on both DB Syncs and `DBSyncAttachmentReady=False/PlacementConflict` on the network, deleting one claim allowed the survivor to attach and reach `Synced=True`, and a public-mode `CardanoNetwork` plus primary-sidecar DB Sync degraded as unsupported. Handoff back from a failing dedicated placement to `primarySidecar` also recovered: the dedicated Deployment was scaled to zero, live dedicated Pods were gone, the primary sidecar reattached, and the DB Sync returned to `Synced=True`. Evidence captured under `/tmp/yacd-sidecar-functional-20260527/negative-recovery`.

Defect found during sidecar-to-dedicated handoff: patching the recovered sidecar claim to `dedicatedFollower` removed `cardano-db-sync` from the primary Deployment and waited until the live primary sidecar Pod was gone before creating `sidecar-conflict-a-dbsync`, but the dedicated `cardano-db-sync` container crash-looped against the same external database with `Shelley.validateGenesisDistribution: Expected initial block to have 7 but got 1`. This means the handoff ordering is safe, but the live runtime path does not work 100% when moving an already-synced primary-sidecar DB Sync to a dedicated follower. Evidence captured under `/tmp/yacd-sidecar-functional-20260527/dedicated-handoff-failure`.

Cleaned up by deleting namespace `yacd-sidecar`. Left `kind-yacd-dev` and Tilt running per active-session policy.
