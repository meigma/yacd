---
id: 022
title: cardanodbsync controller refactor
started: 2026-05-26
---

## 2026-05-26 12:15 — Kickoff
Goal for the session: not yet stated; waiting on the user's request.
Current state of the world: `master` at `777ead0` (PR #39 merged). Sessions 018-021 completed the multi-package readability/maintainability sweep across `internal/cardano/dbsync`, `internal/cardano/localnet`, `internal/ctrlkit`, and `internal/controller/cardanonetwork`. Open follow-up from session 021: `internal/controller/cardanodbsync` peer pass is the next sibling target in the sweep, and the deferred storage-default bleed (`LedgerBackend: "lsm"`, `NearTipEpoch: 580` in `workload_builder.go`) is still open from session 018. Journal worktree clean and synced. Dev stack not started.
Plan: confirm session goal with the user, then proceed.

## 2026-05-26 12:56 — Plan approved, worktree cut
Goal locked: refactor `internal/controller/cardanodbsync` to the cardanonetwork bar in a single PR. Plan file at `/Users/josh/.claude/plans/sorry-use-plan-mode-piped-sonnet.md`.

Three Explore agents ran in parallel (audit, precedent, hexagonal/test). Synthesis surfaced three large gaps: (1) 928-line `workload_builder.go` needs splitting into builder/containers/resources/settings/defaults/validate/names/labels/annotations, (2) untyped condition vocabulary needs typed `conditionType`/`conditionReason` + extraction to conditions.go, (3) `status.go` is a grab-bag and needs a three-way split into status/readiness/conditions.

User-locked decisions:
- Terminology canon: "dbsync workload" (two-container Deployment) + "managed Postgres" (tech-specific) + "managed Postgres workload" (owned Deployment).
- All five rejected architectural changes stand (no K8s client port, no readiness prober port, no RuntimeProber split, no subpackage adapter, no mockery).
- Fold the session-018 deferred storage-default bleed (`LedgerBackend: "lsm"`, `NearTipEpoch: 580`) into this PR via `defaults.go` + a settings resolver.

Cut worktree `refactor/controller-cardanodbsync` from master; started `moon run root:dev-up` in background.

## 2026-05-26 13:40 — PR #40 opened
Implementation complete. Committed in 8 logically separated commits on `refactor/controller-cardanodbsync` (will squash on merge):

1. `d14f7be` — doc.go + typed conditionType/conditionReason vocabulary in conditions.go + errors.go.
2. `b4fa351` — extract names.go, labels.go, annotations.go, defaults.go (also picked up the deferred-from-018 storage-default bleed: `LedgerBackend: "lsm"`, `NearTipEpoch: 580` now live in defaults.go).
3. `226a509` — split 928-line workload_builder.go into builder.go + containers.go + resources.go + settings.go; renamed workload_builder.go → builder.go and workload_builder_test.go → builder_test.go.
4. `7dec48a` — three-way status split: extract readiness.go from status.go (live Deployment/Pod reads); status.go is now patches + endpoint setters only.
5. `4c14ae0` — extract callbacks.go from apply.go (all Validate/Mutate callbacks + shared validateControllerOwner / controlledBy / defaultObject).
6. `1b4b258` — extract postgres_identity.go from managed_postgres.go (pure identity types + fingerprint compute).
7. `b3eb61e` — tighten runtime_probe.go: rename cardanoDBSyncRuntimeProber → runtimeProber (drop redundant package prefix), godoc the port + adapter + types + function-field test seams.
8. `2c94c77` — godoc bar pass across controller.go and database.go; simplify readyCondition to drop the status parameter (every caller passed ConditionFalse).

Test posture: `moon run root:check` green, `moon run root:test` green (cardanodbsync envtest + unit, all other packages unchanged). dev stack still running.

PR: https://github.com/meigma/yacd/pull/40

Open follow-ups (none new from this session — all locked at plan time):
- The five rejected architectural changes (K8s client port, readiness prober port, RuntimeProber split, subpackage adapter, mockery) — separable cross-cutting decisions the user explicitly excluded from this pass.
