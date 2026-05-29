---
id: 033
title: TEST_REPORT finding fixes
date: 2026-05-29
status: complete
repos_touched: [yacd]
related_sessions: [029, 031, 032]
---

## Goal
Continue fixing issues from `.journal/TEST_REPORT.md`, specifically B1: forged
`CardanoNetwork.status.network.*Fingerprint` values should not persist as lying
status or permanently brick reconciliation.

## Outcome
Goal met. PR #51 was squash-merged as `855d9fa`, local `master` was
fast-forwarded, the dev stack was stopped, and the implementation Worktrunk
worktree was removed.

B1 now treats accepted `CardanoNetwork` identity as owned runtime state rather
than status input. The primary node PVC is authoritative, the primary
Deployment pod-template annotations are a fallback only when the PVC is absent,
and status fingerprints are repaired as derived display state.

## Key Decisions
- Read accepted identity from owned children, not status -> removes the
  two-sources-of-truth failure and follows the existing state-on-owned-material
  pattern.
- Keep the Deployment fallback when the PVC is absent -> preserves the existing
  guard that prevents unsafe localnet spec drift after PVC deletion.
- Add a focused parent predicate for identity-status fingerprint changes ->
  forged status self-heals quickly without reenabling all status-update churn.
- Avoid API/CRD description churn -> keep durable architectural guidance in
  package comments and `.journal/TECH_NOTES.md` rather than changing the public
  status schema wording for an internal behavior fix.

## Changes
- `internal/controller/cardanonetwork` - added accepted identity helpers,
  switched accepted-fingerprint validation and compatibility short-circuiting
  to owned runtime material, and repaired status from accepted identity.
- `internal/controller/cardanonetwork` tests - added fake-client coverage for
  status forgery repair, forged-status-plus-PVC-drift recovery, PVC-deletion
  fallback, and manager-backed watch wiring.
- `.journal/TECH_NOTES.md` - recorded that `status.network.*Fingerprint` is
  derived state and must not be used as an acceptance source.

## Open Threads
- Other `.journal/TEST_REPORT.md` findings remain open: B2, B6, D1, D2, D6,
  F0, and F2+F4.
- The unrelated active journal edit in `.journal/030/NOTES.md` was deliberately
  left untouched and unstaged during closeout.

## References
- PR #51: https://github.com/meigma/yacd/pull/51
- Merge commit: `855d9fa`
- TEST_REPORT B1: `.journal/TEST_REPORT.md`
- Session 029: `.journal/029/SUMMARY.md`
- Session 031: `.journal/031/SUMMARY.md`
- Session 032: `.journal/032/SUMMARY.md`

## Lessons
- Status subresources are not safe as identity authority when a controller also
  grants status patch access. Keep accepted identity on owned runtime material
  and publish status as a derived projection.
