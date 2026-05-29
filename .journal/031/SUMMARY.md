---
id: 031
title: A3 artifact recovery rollout throttle
date: 2026-05-28
status: complete
repos_touched: [yacd]
related_sessions: [012, 029]
---

## Goal
Fix `.journal/TEST_REPORT.md` finding A3 with the smallest safe behavior
change: preserve local artifact recovery, but stop sustained external
ConfigMap corruption from rolling the primary Pod once per corruption.

## Outcome
Goal met. PR #49 was squash-merged as `11b6ee7`, local `master` was
fast-forwarded, the session dev stack was stopped, and the implementation
worktree was removed. The A3 manual corruption burst now produces bounded
rollout churn instead of 1:1 Deployment churn.

## Key Decisions
- Store the recovery cooldown timestamp on Deployment metadata, not the Pod
  template -> the cooldown state itself does not roll the primary Pod.
- Keep ConfigMap UID stamping as the republish trigger -> local artifact data
  is still published from the primary PVC by the init container, so a hash-only
  rollout gate would risk leaving recreated ConfigMaps empty.
- Suppress recovery during cooldown by leaving the corrupted ConfigMap in
  place -> `ArtifactsReady=False` remains honest while avoiding repeated Pod
  rolls under sustained external mutation.
- Recreate immediately when recovery is allowed unless deletion is held by a
  finalizer -> the timestamp and Pod-template UID update land in one Deployment
  patch, avoiding an extra generation bump.

## Changes
- `internal/controller/cardanonetwork/annotations.go` - added the internal
  `yacd.meigma.io/network-artifacts-recovery-rollout-at` Deployment metadata
  annotation.
- `internal/controller/cardanonetwork/apply.go` and `artifacts.go` - added the
  60s recovery cooldown, suppression requeue behavior, timestamp parsing, and
  preservation of the previous pod-template artifact ConfigMap UID during
  cooldown.
- `internal/controller/cardanonetwork/controller.go` and `callbacks.go` -
  added deterministic clock injection, recovery/suppression logging, requeue
  handling, and preservation of the metadata annotation across ordinary
  Deployment reconciliation.
- `internal/controller/cardanonetwork/controller_test.go` - covered first
  recovery, suppressed repeat corruption inside cooldown, and post-cooldown
  recovery with a fake client.
- `internal/controller/cardanonetwork/controller_envtest_test.go` - extended
  manager-backed envtest coverage so owned ConfigMap mutation enqueues the
  parent and cooldown suppresses a second recovery rollout.

## Open Threads
- A no-roll repair path using a separate publisher Job remains out of scope.
  This slice bounds rollout churn; it does not make artifact repair
  zero-roll.
- `.journal/TEST_REPORT.md` still contains other unfixed findings from session
  029.

## References
- PR #49: https://github.com/meigma/yacd/pull/49
- Merge commit: `11b6ee7`
- Finding source: `.journal/TEST_REPORT.md` entry A3
- Manual evidence: `.run/manual-a3-20260528-192734`
- Session 012: `.journal/012/SUMMARY.md`
- Session 029: `.journal/029/SUMMARY.md`

## Lessons
- For recovery paths that intentionally republish through a Pod init
  container, throttle the rollout trigger itself instead of trying to gate only
  the artifact hash.
- Deployment metadata changes can still advance `.metadata.generation`; when
  measuring churn, combine metadata timestamp updates with the actual
  pod-template change.
