---
id: 032
title: TEST_REPORT finding fixes
started: 2026-05-28
---

## 2026-05-28 20:25 — Kickoff
Goal for the session: continue fixing the issues found in `.journal/TEST_REPORT.md`; the user asked only to start a new session and report readiness before continuing.
Current state of the world: the personal journal worktree `journal/jmgilman` is clean and up to date. Recent session context says session 029 produced `.journal/TEST_REPORT.md` with 10 unfixed operator findings and no source changes. The main checkout is on `master`, and Worktrunk shows an existing clean implementation worktree for `feat/artifact-recovery-throttle` one commit ahead with `fix(cardanonetwork): throttle artifact recovery rollouts`.
Plan: wait for the user's next instruction, then inspect `.journal/TEST_REPORT.md` and the active implementation worktree, continue with the smallest next fix, validate with repo-native Moon tasks, and append meaningful checkpoints here.

## 2026-05-28 21:48 — A4 implementation start
Goal for this slice: implement the A4 plan by changing primary-sidecar placement conflicts from symmetric "attach none" behavior to deterministic incumbent selection.
Current state of the world: current `master` includes PR #49 for artifact recovery throttling, and the old `feat/artifact-recovery-throttle` worktree has been removed. Created implementation branch/worktree `feat/a4-primary-sidecar-incumbent` from `master`.
Checkpoint: `moon run root:dev-up` completed successfully from the implementation worktree and left the local Tilt/Kind stack running for validation.

## 2026-05-28 22:15 — A4 implemented and validated
Implemented deterministic primary-sidecar incumbent selection on branch `feat/a4-primary-sidecar-incumbent`. The old symmetric conflict behavior is replaced by first-claim ownership: the incumbent continues to publish/attach sidecar material, while later primary-sidecar peers report `PlacementConflict` on their own `CardanoDBSync` status.
Validation passed: focused controller tests with envtest assets, `moon run root:test`, `moon run root:check`, and `git diff --check`.
Manual validation passed on the running Kind/Tilt stack. The original proposed DB Sync sample conflicted with the local example faucet on metrics port `8080`, so the manual proof used metrics port `8081` for the stable DB Sync. Ten toggles of `a4-dbs-toggler` between `primarySidecar` and `dedicatedFollower` kept `phase4-smoke-node` at Deployment generation `2`; `a4-dbs-stable` stayed `SidecarMaterialReady=True`; the toggler reported `PlacementConflict` while in `primarySidecar` and `ExternalDatabaseSecretMissing` when parked at `dedicatedFollower`; the primary pod template retained the `cardano-db-sync` container.

## 2026-05-28 22:29 — Closeout
PR #50 was approved and squash-merged as `5939ecb`. Local `master` was fast-forwarded to the merge commit, the Kind/Tilt dev stack was stopped with `moon run root:dev-down`, and the Worktrunk implementation worktree `feat/a4-primary-sidecar-incumbent` was removed.
Closed the session journal by adding `SUMMARY.md`, indexing session 032, and updating `TECH_NOTES.md` so future sessions know that multiple primary-sidecar claims now use deterministic incumbent selection rather than network-level attach-none conflict behavior.
