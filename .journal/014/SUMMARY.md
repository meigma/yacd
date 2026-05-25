---
id: 014
title: Managed Postgres for CardanoDBSync
date: 2026-05-24
status: complete
repos_touched: [yacd]
related_sessions: [011, 012, 013]
---

## Goal
Assess the current phase-6 db-sync state, then implement the first YACD-owned Postgres path for `CardanoDBSync` through `spec.database.managed`.

## Outcome
The goal was met. PR #24 merged `spec.database.managed` support with an owned managed Postgres Secret/PVC/Service/Deployment, readiness-gated db-sync workload application, conservative status behavior, and follow-up hardening for managed database identity and auth recovery.

## Key Decisions
- Keep `database.managed: {}` within the existing public API -> this proved the first local/dev managed database path without expanding the CRD surface.
- Run managed Postgres as a separate Deployment rather than a db-sync sidecar -> this keeps database lifecycle and readiness explicit and avoids tying DB restarts to db-sync container changes.
- Treat managed Postgres bootstrap inputs and password material as immutable after acceptance -> this avoids pretending the operator can safely rotate or recreate credentials for an initialized data directory.
- Keep aggregate `Ready=False` until sync progress probing exists -> `PostgresReady=True` can now be honest for managed Postgres, but db-sync indexing health is still intentionally unprobed.

## Changes
- `api/v1alpha1/cardanodbsync_types.go` - accepted and defaulted `spec.database.managed` while preserving exactly-one database mode validation.
- `internal/controller/cardanodbsync/database.go` - resolved external vs managed database runtime, generated or validated managed auth Secrets, and blocked unsafe generated Secret regeneration.
- `internal/controller/cardanodbsync/managed_postgres.go` - rendered owned managed Postgres PVC, Service, Deployment, probes, security context, and bootstrap identity.
- `internal/controller/cardanodbsync/controller.go` - applied managed Postgres before db-sync workloads, gated db-sync on live Postgres readiness, and preserved external database behavior.
- `internal/controller/cardanodbsync/status.go` - published managed Postgres endpoints and conditions while keeping sync progress conservative.
- `internal/controller/cardanodbsync/*_test.go` - added controller, envtest, and builder coverage for managed resources, auth Secret references, watches, readiness gating, drift repair, identity rejection, and Secret churn behavior.
- `examples/cardanodbsync-managed-postgres.yaml` and `test/chainsaw/manager-smoke/chainsaw-test.yaml` - added a raw managed Postgres smoke path that proves owned Postgres resources and `PostgresReady=True`.

## Open Threads
- Add real Postgres connectivity and db-sync sync-progress probes, then populate `status.sync` and eventually allow aggregate `Ready=True`.
- Add CLI/devconfig support for creating `CardanoDBSync`; this session only added raw YAML coverage.
- Managed Postgres remains a local/dev path only: HA, backups, restore, password rotation, and major-version upgrades are intentionally out of scope.

## References
- PR #24: https://github.com/meigma/yacd/pull/24
- Prior db-sync controller runtime: `.journal/013/SUMMARY.md`
- First db-sync CRD slice: `.journal/011/SUMMARY.md`
