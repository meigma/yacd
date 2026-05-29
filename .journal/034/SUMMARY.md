---
id: 034
title: TEST_REPORT follow-through
date: 2026-05-29
status: complete
repos_touched: [yacd]
related_sessions: [029, 031, 032, 033]
---

## Goal
Continue fixing issues found in `.journal/TEST_REPORT.md`, starting with B2:
`CardanoDBSync` accepted database identity status forgery could be treated as
authoritative state.

## Outcome
The B2 goal was met. PR #52 was squash-merged into `master`, local `master` was
fast-forwarded to the merge commit, the implementation worktree was removed,
and the local Kind/Tilt dev stack was shut down.

## Key Decisions
- Make the owned db-sync state PVC annotation authoritative -> status is user
  writable through the status subresource, while the owned PVC is controller
  runtime material.
- Enqueue accepted-identity status-only updates -> forged or cleared
  `status.database.acceptedIdentityFingerprint` self-heals without a spec bump.
- Keep `internal/cardano/dbsync` unchanged -> accepted runtime material is a
  controller-owned Kubernetes concern, not planner/domain state.
- Defer persisted field-level identity diffs -> B2's root trust bug was fixed
  without widening the slice.

## Changes
- `internal/controller/cardanodbsync/apply.go` - reads accepted database
  identity from the owned state PVC annotation, repairs status from that source,
  and reports real identity drift with accepted/desired fingerprints plus the
  PVC and annotation key.
- `internal/controller/cardanodbsync/controller.go` - replaces the generation-
  only parent predicate with a local predicate that also catches accepted
  database identity status changes.
- `api/v1alpha1/cardanodbsync_types.go` and
  `charts/yacd/crds/yacd.meigma.io_cardanodbsyncs.yaml` - document
  `acceptedIdentityFingerprint` as controller-published derived status.
- `internal/controller/cardanodbsync/*_test.go` - adds focused reconciler tests
  and manager-backed envtest coverage for forged status repair, real identity
  drift, and pre-PVC status forgery.
- `internal/controller/cardanodbsync/doc.go` and `errors.go` - refresh package
  docs and hard-error wording around accepted identity.

## Open Threads
- Remaining TEST_REPORT findings still need follow-through: B6, D1, D2, D6,
  F0, and F2/F4.
- The primary checkout had unrelated dirty session-protocol edits and an
  untracked `.claude/scheduled_tasks.lock`; those were left untouched.

## References
- PR #52: https://github.com/meigma/yacd/pull/52
- `.journal/TEST_REPORT.md`
- `.journal/034/NOTES.md`
