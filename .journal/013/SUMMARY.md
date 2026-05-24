---
id: 013
title: CardanoDBSync controller runtime
date: 2026-05-24
status: complete
repos_touched: [yacd]
related_sessions: ["011", "012"]
---

## Goal
Start the real `CardanoDBSync` controller path for phase 6. Prove that a
db-sync supporting-service resource can consume fresh `CardanoNetwork`
artifacts, accept an externally supplied Postgres database, generate the first
runtime Kubernetes workload set, and publish honest status without taking on
managed Postgres yet.

## Outcome
The goal was met. PR #23 landed the controller and runtime workload slice,
local `master` was fast-forwarded to `6cfe700`, the session dev stack was shut
down, and the implementation Worktrunk branch/worktree was removed.

## Key Decisions
- Require fresh network artifacts before any workload apply -> db-sync depends
  on exact same-namespace `CardanoNetwork` output and should not consume stale
  status or annotation-only claims.
- Support external Postgres first -> this gets a runnable db-sync instance
  without committing to managed database ownership, backup, upgrade, or storage
  policy too early.
- Keep db-sync planning in `internal/cardano/dbsync` -> the Kubernetes
  controller consumes a pure normalized plan, fingerprint, config file,
  topology file, and command shape instead of mixing db-sync semantics into
  object builders.
- Use an accepted database identity guard -> network artifacts, database
  address/user, db-sync image, ledger backend, and insert options are treated as
  database-affecting until there is an explicit migration/recreate story.
- Publish runtime status conservatively -> Deployment availability and
  container readiness are reported now, while Postgres probing and sync-lag
  probing remain `RuntimeProbesPending`.

## Changes
- `api/v1alpha1/cardanodbsync_types.go` and generated CRDs - added external
  database mode, optional config/storage defaults, database identity status,
  and insert-option pointer fields so presets are not defeated by CRD
  defaulting.
- `cmd/setup.go` and `charts/yacd/templates/rbac-manager.yaml` - registered the
  `CardanoDBSync` controller and granted the manager the reads/writes it needs
  for db-sync resources, referenced networks, Secrets, ConfigMaps, workloads,
  PVCs, Services, and Pods.
- `internal/cardano/dbsync` - added the pure db-sync planner for normalized
  config rendering, topology rendering, pgpass environment, plan fingerprinting,
  and accepted database identity fingerprinting.
- `internal/controller/cardanodbsync` - added reconcile, dependency validation,
  artifact ConfigMap data/hash verification, workload apply, drift repair,
  invalid-prerequisite suspension, status/condition helpers, runtime readiness
  checks, and manager-backed watch/index coverage.
- `examples/cardanodbsync-external-postgres.yaml` - added a minimal external
  Postgres example with the password Secret and `CardanoDBSync` resource.
- `api/v1alpha1/*_test.go`, `internal/cardano/dbsync/*_test.go`, and
  `internal/controller/cardanodbsync/*_test.go` - covered defaulting,
  validation, planner rendering, identity behavior, direct reconciles, owned
  workload behavior, and manager-backed watch wiring.

## Open Threads
- Add explicit Postgres connectivity/schema probing and db-sync sync progress
  checks so `PostgresReady`, `DBSyncReady`, `Synced`, and aggregate `Ready`
  can become true from live runtime state.
- Add managed Postgres in a later slice once the external database path is
  stable.
- Add Chainsaw coverage only after there is an installed runtime behavior worth
  proving in a real Kind cluster beyond the envtest/controller contract.
- Define a real upgrade/recreate flow for database-affecting db-sync plan
  changes before allowing image, insert-option, ledger, or network-artifact
  mutations against an accepted database identity.

## Lessons
- Upstream db-sync configuration has sharp runtime details: `PGPASSFILE` /
  `--pg-pass-env`, string-valued config toggles, image entrypoint/schema paths,
  and LSM insert-option constraints all needed direct grounding before the
  generated Kubernetes objects were trustworthy.
- Artifact annotations are not enough for a dependent controller. Recomputing
  the live ConfigMap data hash and required-key set is the right contract at the
  `CardanoNetwork` to `CardanoDBSync` boundary.
- The review-driven loop worked well here: keep the slice runnable but bounded,
  then tighten correctness around the concrete startup failures the prototype
  exposed.

## References
- PR #23: https://github.com/meigma/yacd/pull/23
- Merged commit: `6cfe70083362e4b9438b119fe059cdac97df5b2b`
- Prior db-sync API session: `.journal/011/SUMMARY.md`
- Prior network artifact session: `.journal/012/SUMMARY.md`
