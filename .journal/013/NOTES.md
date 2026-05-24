---
id: 013
title: Pending session request
started: 2026-05-24
---

## 2026-05-24 09:12 — Kickoff
Goal for the session: Start a fresh YACD journal session and wait for the user's actual implementation, review, or research request.
Current state of the world: The journal branch `journal/jmgilman` is clean and up to date. Recent closed work completed the faucet dev-image fix, the API-only `CardanoDBSync` CRD slice, and the `CardanoNetwork` localnet artifact ConfigMap path. Local `master` is at `9ac60de` from the artifact ConfigMap merge, and the prior session stopped the dev stack.
Plan: Wait for the user's request. For implementation work, select or create the implementation Worktrunk worktree first, then run `moon run root:dev-up` there before changing runtime code unless the user explicitly waives that session startup step.

## 2026-05-24 09:39 — Implementation branch and dev stack
Goal for the session is now the phase 6 `CardanoDBSync` controller initialization slice. Created implementation branch `feat/dbsync-controller-init` at `/Users/josh/code/meigma/yacd/.wt/feat-dbsync-controller-init` from current `master` (`9ac60de`). Ran `direnv allow` and `moon run root:dev-up` from that worktree; the command completed successfully and reported the YACD dev stack ready with Tilt at `http://localhost:10350/`.

## 2026-05-24 09:46 — CardanoDBSync controller initialized
Implemented the first `CardanoDBSync` controller slice on `feat/dbsync-controller-init` and committed it as `f39e41b` (`feat(cardanodbsync): initialize controller`). The slice registers a controller, indexes `spec.networkRef.name`, watches referenced `CardanoNetwork` status updates, validates fresh artifact status plus the live artifact ConfigMap metadata, and reports dependency/workload-pending conditions without creating runtime children. Validation passed: `moon run root:generate`, targeted impacted package tests, `moon run root:test`, `moon run root:check`, and `git diff --check`.

## 2026-05-24 10:44 — External Postgres contract added
Implemented the external Postgres prerequisite slice on `feat/dbsync-controller-init` and committed it as `2b1d8a1` (`feat(cardanodbsync): support external postgres references`). The API now requires exactly one of `spec.database.external` or `spec.database.managed`, defaults external connection fields, and preserves managed database settings under the reserved `managed` branch. The controller currently accepts only external mode, validates the same-namespace password Secret/key, watches referenced Secrets, and keeps reporting `WorkloadsPending` after database and network prerequisites are accepted. Validation passed: `moon run root:generate`, `moon run root:test`, `moon run root:check`, `git diff --check`, and `git diff --cached --check`.

## 2026-05-24 11:27 — External db-sync workloads generated
Implemented the first runtime workload-generation slice for `CardanoDBSync` on `feat/dbsync-controller-init` and committed it as `9e400b3` (`feat(cardanodbsync): generate external dbsync workloads`). Added a pure `internal/cardano/dbsync` planner for normalized config/topology/command/fingerprint generation, plus controller builders and apply helpers for the owned config ConfigMap, shared state PVC, two-container follower/db-sync Deployment, and metrics Service. The controller now applies those resources after fresh network artifacts and an external Postgres Secret are accepted, watches owned children, repairs owned ConfigMap drift, and reports `WorkloadsApplied` while keeping `Ready`, `FollowerNodeReady`, `PostgresReady`, `DBSyncReady`, and `Synced` false until runtime probes are added. Validation passed: `moon run root:generate`, `moon run root:test`, `moon run root:check`, `git diff --check`, and `git diff --cached --check`.

## 2026-05-24 13:23 — Review hardening for db-sync workloads
Addressed review feedback on `feat/dbsync-controller-init` and committed it as `85661f6` (`fix(cardanodbsync): harden generated workloads`). The controller now renders an owned pgpass Secret, mounts it with `PGPASSFILE`, passes `--pg-pass-env`, removes `PGPASSWORD`, rejects newline-bearing password Secret values, creates a separate follower-node PVC from `spec.followerNode.storage`, renders `spec.config.ipfsGateways` into `ipfs_gateway`, and scales the owned db-sync Deployment to zero when prerequisites become invalid or blocked. Validation passed: `moon run root:generate`, `moon run root:test`, `moon run root:check`, `git diff --check`, and `git diff --cached --check`.
