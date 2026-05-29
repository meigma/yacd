---
id: 035
title: B6 storage expansion status
date: 2026-05-29
status: complete
repos_touched: [yacd]
related_sessions: [029, 031, 032, 033, 034]
---

## Goal
Continue TEST_REPORT follow-through by fixing B6: PVC expansion failures on
non-expandable StorageClasses were visible only in controller logs while CR
status stayed stale and misleading.

## Outcome
Goal met. PR #53 was squash-merged as `dea708e`, local `master` was
fast-forwarded, the dev stack was shut down, and the implementation Worktrunk
worktree was removed.

B6 now surfaces rejected PVC expansion through CR status for both
`CardanoNetwork` and `CardanoDBSync` PVC apply paths.

## Key Decisions
- Classify failed existing-object persistence in `ctrlkit/apply` -> keeps the
  create/read/owner/validate/mutate/persist skeleton shared while letting
  controllers map API-server rejections into their own status contract.
- Map only Forbidden/Invalid expansion rejections -> preserves unexpected
  errors as controller-runtime errors while publishing a useful condition for
  the Kubernetes resize rejection B6 found.
- Skip StorageClass preflight for this slice -> the update-error path covers
  StorageClass limits, admission, and CSI-specific rejection without adding new
  RBAC.

## Changes
- `internal/ctrlkit/apply` - added optional `UpdateError` mapping for failed
  patch/update of existing owned objects.
- `internal/controller/storage` - added shared PVC update-error mapping to
  `StorageExpansionRejected`.
- `internal/controller/cardanonetwork` and `internal/controller/cardanodbsync`
  - wired rejected PVC expansion into typed status conditions and added
  focused reconciler tests.

## Open Threads
- Remaining TEST_REPORT findings still need follow-through: D1, D2, D6, F0,
  and F2/F4.

## References
- PR #53: https://github.com/meigma/yacd/pull/53
- Merge commit: `dea708e`
- TEST_REPORT B6: `.journal/TEST_REPORT.md`
- Session 029: `.journal/029/SUMMARY.md`
- Session 034: `.journal/034/SUMMARY.md`

## Lessons
PVC expansion support is ultimately adjudicated by Kubernetes at update time,
not just by YACD's desired-state planner. Shared apply helpers need a narrow
way to classify persistence failures so user-actionable API-server messages can
reach status without turning every raw API error into a degraded condition.
