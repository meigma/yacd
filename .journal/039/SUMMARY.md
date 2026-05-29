---
id: 039
title: D6 managed Postgres auth recovery
date: 2026-05-29
status: complete
repos_touched: [yacd]
related_sessions: [029, 036, 037, 038]
---

## Goal
Fix TEST_REPORT finding D6 so the documented recovery path for generated
managed Postgres auth Secrets is real: after managed database identity is
accepted, restoring the same-name Secret with the original `data.password`
should recover without a `CardanoDBSync.spec` bump.

## Outcome
The goal was met. PR #57 was merged with a focused controller change that
adopts only unowned same-name generated auth Secrets whose password material
re-derives the accepted managed Postgres identity. Wrong passwords are rejected
as identity drift, foreign-owned Secrets still conflict, and plain restores now
enqueue the owning `CardanoDBSync` through the field index/watch path.

## Key Decisions
- Keep the exception local to the generated managed-Postgres auth Secret
  lifecycle -> this preserved `ctrlkit/apply` and the generic owner-validation
  boundary instead of adding broad orphan adoption behavior.
- Recompute identity from restored password bytes -> this let the controller
  validate a plain restored Secret even when the controller-owned fingerprint
  annotation was absent.
- Continue degrading on missing generated Secrets after acceptance -> this
  avoids silently generating credentials that no longer match the initialized
  Postgres data directory.

## Changes
- `internal/controller/cardanodbsync/managed_postgres_auth.go` - added the
  generated/provided managed Postgres auth Secret lifecycle helpers, including
  validated adoption of restored generated Secrets.
- `internal/controller/cardanodbsync/database.go` - moved the auth Secret
  lifecycle out of the broader database resolver so the side-effecting managed
  auth behavior has a focused home.
- `internal/controller/cardanodbsync/postgres_identity.go` - added identity
  helper support for recomputing accepted managed Postgres identity from
  restored password material.
- `internal/controller/cardanodbsync/controller.go` - extended the managed
  database Secret index/watch fan-out to generated auth Secret names when
  `authSecretRef` is omitted.
- `internal/controller/cardanodbsync/doc.go` - documented the deliberate
  generated auth Secret adoption exception.
- `internal/controller/cardanodbsync/controller_test.go` and
  `internal/controller/cardanodbsync/controller_envtest_test.go` - covered
  deletion, valid restore adoption, wrong-password rejection, foreign-owner
  conflict, and manager-backed watch wiring.

## Open Threads
- TEST_REPORT F0 and F2/F4 remain open for future sessions.
- Session 036 still has separate CLI lifecycle work in progress; this session
  intentionally left `feat/cli-lifecycle` untouched.

## References
- PR #57: https://github.com/meigma/yacd/pull/57
- TEST_REPORT: `.journal/TEST_REPORT.md`
- Prior breakage inventory: `.journal/029/SUMMARY.md`

## Lessons
- Recovery paths that depend on user-restored Kubernetes objects need both the
  behavioral adoption logic and the watch/index path; one without the other can
  still require a spec bump and leave the advertised recovery incomplete.
