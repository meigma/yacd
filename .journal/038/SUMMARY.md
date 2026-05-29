---
id: 038
title: TEST_REPORT continuation
date: 2026-05-29
status: complete
repos_touched: [yacd]
related_sessions: [029, 031, 032, 033, 034, 035, 037]
---

## Goal
Continue the `.journal/TEST_REPORT.md` follow-through by fixing D2, the primary
PVC deletion honesty issue. The required behavior was to fail closed when an
owned primary state PVC is terminating or missing after accepted localnet state,
rather than treating deletion as ordinary readiness churn or silently recreating
lost state.

## Outcome
The goal was met. PR #56 was reviewed, CI passed, and it was squash-merged into
`master` as `a28cc401`. Local `master` was fast-forwarded and includes PR #56
as an ancestor; PR #55 landed separately during closeout, so final local
`master` ended at `0bb852d`. The D2 dev stack was shut down, and the
`feat/d2-primary-pvc-deletion` Worktrunk worktree and remote branch were
removed.

## Key Decisions
- Primary PVC loss after accepted identity is not auto-recovered -> localnet
  state is durable for the `CardanoNetwork` lifetime, so safe recovery requires
  an explicit CR delete/recreate.
- Runtime truth remains the acceptance source -> the refusal gate uses existing
  PVC or Deployment annotations through `acceptedNetworkIdentity`, never CR
  status.
- Terminating owned children use a shared controller helper -> both controllers
  now surface `ChildBeingDeleted` with object/finalizer detail instead of each
  controller inventing one-off status text.
- `ctrlkit/apply` grew explicit hooks -> `ValidateCreate` and
  `ObjectDeleting` make create-path refusal and deletion-path fail-closed
  behavior first-class owned-child mechanics instead of special-case patches.

## Changes
- `internal/ctrlkit/apply` - added `ValidateCreate` and `ObjectDeleting` hooks,
  documented their ordering, and covered deletion/create blocking behavior.
- `internal/controller/children` - added shared status mapping for owned
  children that are already being deleted, including blocking finalizers.
- `internal/controller/cardanonetwork` - wired terminating-child detection,
  added `ChildBeingDeleted` and `PrimaryStateLost`, and refused primary PVC
  recreation after accepted runtime identity.
- `internal/controller/cardanodbsync` - wired terminating-child detection into
  owned child reconciliation and exposed `ChildBeingDeleted`.
- `internal/controller/cardanonetwork/controller_envtest_test.go` and
  `controller_test.go` - updated the old manager envtest recreation assertion
  into degraded-status proof and added unit coverage for terminating and lost
  primary PVC paths.

## Open Threads
- TEST_REPORT D6, F0, and F2/F4 remain for future sessions.
- Session 036 remains listed as in progress in the journal and was not closed
  here. Its PR #55 merged separately during this closeout window.

## References
- PR #56: https://github.com/meigma/yacd/pull/56
- Merge commit: `a28cc401ff48ed1b3801ee91e97fc48fbe339395`
- Implementation branch commit: `a763c1b871e1379650ecedeec7fa6c24ab752376`
- `.journal/TEST_REPORT.md` finding D2
