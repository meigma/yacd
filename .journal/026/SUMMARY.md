---
id: 026
title: Primary sidecar manual functional testing
date: 2026-05-27
status: complete
repos_touched: [yacd]
related_sessions: [024, 025]
---

## Goal
Run a manual Kind/Tilt functional pass for PR #45's `CardanoDBSync.spec.placement.mode: primarySidecar` path, covering managed Postgres, external Postgres, validation failures, conflict handling, and placement transitions. Fix any release-blocking failure surfaced by that pass.

## Outcome
Goal met. The manual pass proved the managed and external primary-sidecar paths, unsafe-input rejection, conflict behavior, public-mode rejection, and conflict recovery. It also found a real sidecar-to-dedicated runtime failure: after an already-synced primary-sidecar DB Sync was patched to `dedicatedFollower`, the dedicated db-sync process crash-looped against the same database with a Shelley genesis distribution mismatch. PR #46 fixed the contract by rejecting post-acceptance placement changes, then the updated behavior was manually regressed and merged.

## Key Decisions
- Treat placement handoff after accepted db-sync state as unsupported -> the failure was a database/source-topology compatibility problem, not Kubernetes rollout ordering.
- Keep the frozen `DatabaseIdentityFingerprint` wire shape unchanged -> adding placement to that hash would have degraded existing resources on upgrade.
- Add separate accepted placement metadata -> `status.database.acceptedPlacementMode` plus `yacd.meigma.io/dbsync-placement-mode` can reject unsafe flips while preserving existing database identity semantics.
- Preserve no-duplicate workload guards -> pre-acceptance or cleanup paths still need to avoid running dedicated and sidecar db-sync processes against the same state.

## Changes
- `api/v1alpha1/cardanodbsync_types.go` and generated CRD - added `status.database.acceptedPlacementMode`.
- `internal/controller/cardanodbsync` - stamps accepted placement on DB Sync-owned material, validates accepted placement before applying opposite-placement workloads, backfills legacy placement from live state/workload shape, and scales/stops blocked workloads through existing degraded status flow.
- `internal/controller/cardanodbsync/*_test.go` - added focused builder/controller coverage for placement annotation stamping, sidecar-to-dedicated rejection, dedicated-to-sidecar rejection, and legacy backfill.
- `DESIGN.md` - clarified that changing between `primarySidecar` and `dedicatedFollower` after accepted state requires recreate/fresh-or-compatible database state.

## Open Threads
- Supporting true post-acceptance placement migration remains a future explicit reset/migration feature, not an implicit patch behavior.
- Public-network `primarySidecar` support remains intentionally unsupported; this session only verified the negative contract.
- The original failed runtime evidence remains under `/tmp/yacd-sidecar-functional-20260527/dedicated-handoff-failure`; the fixed regression evidence is under `/tmp/yacd-sidecar-functional-20260527-fix`.

## References
- PR #46: https://github.com/meigma/yacd/pull/46
- PR #45: https://github.com/meigma/yacd/pull/45
- Session 025: `.journal/025/SUMMARY.md`
- Session 024: `.journal/024/SUMMARY.md`

## Lessons
- A safe rollout order does not imply the underlying database state is portable between runtime topologies. For db-sync, the node/socket/source topology belongs in accepted state even when the upstream database identity hash stays stable.
