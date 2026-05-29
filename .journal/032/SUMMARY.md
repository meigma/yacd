---
id: 032
title: TEST_REPORT finding fixes
date: 2026-05-28
status: complete
repos_touched: [yacd]
related_sessions: [029, 031]
---

## Goal

Continue fixing issues from `.journal/TEST_REPORT.md`, specifically A4:
peer `CardanoDBSync` resources toggling between `primarySidecar` and
`dedicatedFollower` should not detach an unrelated stable primary-sidecar DB
Sync from the primary `CardanoNetwork` Pod.

## Outcome

Goal met. PR #50 merged as squash commit `5939ecb`, adding deterministic
primary-sidecar incumbent selection, updating `CardanoDBSync` and
`CardanoNetwork` behavior and coverage, fast-forwarding local `master`,
stopping the dev stack, and removing the implementation Worktrunk worktree.

Validation passed with focused controller tests, `moon run root:test`,
`moon run root:check`, `git diff --check`, and a manual 10-cycle Kind/Tilt
toggle proof.

## Key Decisions

- Treat primary-sidecar ownership as deterministic first-claim ownership so
  stable sidecar continuity survives late peer churn.
- Keep the policy in a pure `cardanodbsync` selector so both controllers share
  one contract while API-server listing stays at controller edges.
- Report conflicts on losing `CardanoDBSync` resources, not on
  `CardanoNetwork`, so the network keeps composing the valid incumbent.
- Adjust the manual proof to DB Sync metrics port `8081`; the proposed `8080`
  sample conflicts with the local faucet and would test port validation instead
  of A4.

## Changes

- `internal/controller/cardanodbsync` now has the shared placement claim
  selector, uses it in placement gating, and covers incumbent/conflict behavior
  with unit and manager-backed tests.
- `internal/controller/cardanonetwork` now selects and attaches the incumbent
  primary-sidecar claim even when later peers exist, and no longer reports a
  network-level `PlacementConflict` for those late peers.
- `.journal/TECH_NOTES.md` records the durable primary-sidecar conflict
  semantics after the merge.

## Open Threads

- Other `.journal/TEST_REPORT.md` findings remain open.
- The manual validation sample should use a non-faucet DB Sync metrics port,
  such as `8081`, when run against `examples/local/yacd.yaml`.

## References

- PR #50: https://github.com/meigma/yacd/pull/50
- Merge commit: `5939ecb`
- TEST_REPORT A4: `.journal/TEST_REPORT.md`
- Session 029: `.journal/029/SUMMARY.md`
- Session 031: `.journal/031/SUMMARY.md`

## Lessons

A symmetric "attach none" conflict policy can become a disruption vector when
the conflicter is controlled by a different user. Conflict reporting belongs on
the losing peer while the stable incumbent remains attached.
