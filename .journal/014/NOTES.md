---
id: 014
title: New YACD session
started: 2026-05-24
---

## 2026-05-24 15:48 — Kickoff
Goal for the session: start a fresh journaled YACD working session; the concrete implementation goal has not been stated yet.
Current state of the world: session 013 closed with PR #23 merged, adding the first `CardanoDBSync` controller runtime for external Postgres, conservative runtime status, artifact validation, and owned db-sync/follower workloads. Session 012 completed `CardanoNetwork` artifact ConfigMap publishing, and session 011 introduced the `CardanoDBSync` API shape. The journal worktree for `journal/jmgilman` was clean and up to date before this session was created.
Plan: wait for the user's actual request, then select or create an implementation Worktrunk worktree, start the dev stack once if implementation work proceeds, keep notes at meaningful checkpoints, and preserve the agile prototype-first cadence.

## 2026-05-24 16:05 — Phase 6 assessment
Goal for this checkpoint: assess how far phase 6 is with the currently implemented db-sync-adjacent code.
What was reviewed: `.journal/PLAN.md`, `api/v1alpha1/cardanodbsync_types.go`, `internal/cardano/dbsync`, `internal/controller/cardanodbsync`, manager registration, chart RBAC, the external Postgres example, Chainsaw coverage, and README/design surfaces.
Current finding: phase 6 has a substantial controller-contract slice in place: `CardanoDBSync` exists as a namespaced CRD, the manager registers its controller, the reconciler consumes fresh verified `CardanoNetwork` artifacts, renders a follower/db-sync Deployment with owned config, pgpass Secret, two PVCs, and metrics Service, and publishes conservative conditions. The missing phase-6 proof is the live runtime loop: no managed Postgres workload, no Postgres connectivity probe, no db-sync sync-progress probe, no `status.sync` population, no aggregate `Ready=True`, no CLI/devconfig path, and no Kind/Chainsaw db-sync runtime smoke yet.
Verification: `KUBEBUILDER_ASSETS="$(setup-envtest use 1.35.x -p path)" go test ./api/v1alpha1 ./internal/cardano/dbsync ./internal/controller/cardanodbsync` passed. `moon run root:test` failed only in `test/chart` because `go test ./...` from the primary checkout walks the nested `.wt/journal-jmgilman` worktree and controller-gen sees its old `template-k8s` sample RBAC; db-sync packages themselves passed.
Next likely slice: add the smallest runnable phase-6 proof around external Postgres first, probably with a local smoke manifest/service and Postgres/db-sync progress probing before tackling managed Postgres.

## 2026-05-24 16:24 — Managed Postgres implementation start
Goal for this checkpoint: begin the approved `CardanoDBSync.spec.database.managed` implementation slice in an isolated Worktrunk branch.
Setup: created implementation branch/worktree `feat/cardanodbsync-managed-postgres` at `.wt/feat-cardanodbsync-managed-postgres` from current `master`.
Dev stack: ran `moon run root:dev-up` from the implementation worktree; Kind `kind-yacd-dev`, the local registry, and Tilt came up successfully, and the controller readiness gate passed. Tilt UI is available at `http://localhost:10350/`.
Implementation intent: keep the existing public API, add the managed Postgres owned Secret/PVC/Service/Deployment path, gate db-sync workload application on live Postgres readiness, and preserve conservative `Ready=False`/`SyncNotProbed` behavior until the later sync-progress slice.

## 2026-05-24 16:43 — Managed Postgres implementation validated
Goal for this checkpoint: complete and validate the approved managed Postgres slice for `CardanoDBSync`.
Implementation: branch `feat/cardanodbsync-managed-postgres` now has commit `ab14b3b` (`feat(cardanodbsync): add managed postgres support`). The controller resolves external vs managed database modes into one db-sync database endpoint, creates/preserves a generated managed auth Secret when `managed.authSecretRef` is omitted, validates provided managed auth Secrets without owning them, applies the owned Postgres PVC/Service/Deployment, gates the db-sync workload on live Postgres Deployment/container readiness, and keeps `Synced=False/SyncNotProbed` until the future sync-progress probe.
Status behavior: managed mode reports `status.endpoints.postgres.serviceName`, port, URL, and generated `status.database.authSecretName`; `PostgresReady=True` comes from the managed Postgres workload readiness path. External mode still reports `PostgresReady=False/ExternalDatabaseNotProbed`.
Verification: `moon run root:generate`, targeted `KUBEBUILDER_ASSETS="$(setup-envtest use 1.35.x -p path)" go test ./api/v1alpha1 ./internal/cardano/dbsync ./internal/controller/cardanodbsync ./test/chart`, `moon run root:check`, `moon run root:test`, `git diff --check`, and `moon run root:test-e2e` all passed. The Chainsaw smoke created a raw `CardanoDBSync` with `database.managed: {}`, observed the owned Postgres resources, and confirmed `PostgresReady=True` with `Synced=False/SyncNotProbed`.

## 2026-05-24 17:47 — Managed Postgres review fixes
Goal for this checkpoint: address the review findings on managed Postgres identity ordering, unsafe Secret rotation assumptions, status truth during Postgres regressions, and generated Secret resourceVersion churn.
Implementation: the controller now builds a managed Postgres identity from bootstrap-affecting inputs before applying owned Postgres resources and stores that identity on the managed Postgres PVC/template. Changes to managed Postgres image/database/user/auth identity are rejected before Deployment mutation. Generated auth Secrets get a password fingerprint annotation and password edits are rejected instead of rolling Postgres or db-sync; managed db-sync workload rendering uses a stable managed credential version instead of treating every generated Secret resourceVersion change as rollout-safe. Status while Postgres is temporarily unready now preserves accepted db-sync database identity and reports the live follower/db-sync workload conditions instead of resetting them to `WorkloadMissing`.
Verification: `go test ./internal/controller/cardanodbsync -run 'Test(CardanoDBSyncReconciler|DBSyncWorkloadBuilder)'`, `moon run root:generate`, `moon run root:test`, `moon run root:check`, `git diff --check`, and `moon run root:test-e2e` passed. After `root:test-e2e`, kube context was switched back to `kind-yacd-dev`.

## 2026-05-24 18:09 — Managed auth recovery hardening
Goal for this checkpoint: address follow-up review findings around missing generated auth Secret recovery and over-broad provided Secret identity.
Implementation: missing generated managed Postgres auth Secrets are no longer regenerated after a managed Postgres identity exists; the controller now degrades with `ManagedDatabaseSecretMissing` and leaves the Secret missing so a random wrong password is not introduced. Provided managed auth Secret identity now uses Secret name plus password material fingerprint, not resourceVersion or UID, so metadata churn and unrelated keys do not force `UnsupportedDatabaseIdentityChange`; actual password changes still block before child mutation.
Verification: `go test ./internal/controller/cardanodbsync -run 'Test(CardanoDBSyncReconciler|DBSyncWorkloadBuilder)'`, `moon run root:generate`, `moon run root:test`, `moon run root:check`, `git diff --check`, and `moon run root:test-e2e` passed. After `root:test-e2e`, kube context was switched back to `kind-yacd-dev`.

## 2026-05-24 18:31 — Close
Merged PR #24 (`feat(cardanodbsync): add managed postgres support`) with squash commit `879c0d7`, fast-forwarded local `master`, deleted the remote feature branch, removed the `feat/cardanodbsync-managed-postgres` Worktrunk worktree, and stopped the `kind-yacd-dev` dev stack with `moon run root:dev-down`.
Final handoff: `.journal/014/SUMMARY.md` records the session postmortem, `.journal/INDEX.md` marks session 014 complete, and `.journal/TECH_NOTES.md` now reflects the managed Postgres support and identity/auth constraints.
