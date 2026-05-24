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
