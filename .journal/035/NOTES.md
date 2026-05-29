---
id: 035
title: TEST_REPORT follow-through
started: 2026-05-29
---

## 2026-05-29 10:52 — Kickoff
Goal for the session: Continue fixing issues found in `.journal/TEST_REPORT.md`.
Current state of the world: Recent sessions fixed and merged A3, A4, B1, and B2. The journal technical notes list B6, D1, D2, D6, F0, and F2/F4 as the remaining concrete findings to consult before touching relevant code paths.
Plan: Prime the journal session, then wait for the next implementation request. Before making fixes, select or create the implementation Worktrunk worktree, start the local dev stack, read `.journal/TEST_REPORT.md`, and validate the chosen slice with focused tests plus the repo gates.

## 2026-05-29 11:37 — B6 implementation
Implemented B6 on `feat/b6-storage-expansion-status` with commit `8bc30a5` (`fix(controller): surface rejected PVC expansion in status`).
What changed: `ctrlkit/apply.ApplyOwnedObject` now has an optional persistence-error mapper, controller storage maps Forbidden/Invalid PVC expansion update failures to `StorageExpansionRejected`, and both CardanoNetwork and CardanoDBSync PVC apply paths use it.
Validation: focused Go tests passed; `moon run root:test`, `moon run root:check`, and `git diff --check` passed. Manual Kind/Tilt proof confirmed default StorageClass `standard` has no `allowVolumeExpansion`, `2Gi -> 5Gi` on `phase4-smoke` surfaces `Degraded=True` / `Ready=False` / `StorageExpansionRejected` with PVC still at `2Gi`, and reverting to `2Gi` recovers `Ready=True`.
