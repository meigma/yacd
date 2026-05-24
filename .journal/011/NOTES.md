---
id: 011
title: Phase 6 db-sync supporting service
started: 2026-05-23
---

## 2026-05-23 16:42 — Kickoff
Goal for the session: move on to phase 6 from `.journal/PLAN.md` after completing phase 5.
Current state of the world: phase 5 is complete; Kupo and the authenticated faucet/topup path are merged, and the plan now points at db-sync as the first supporting-service CRD. Phase 6 is scoped around a db-sync resource that references the primary environment, runs with a dedicated follower node and database wiring, reports readiness/sync progress, and does not mutate or restart the primary node Pod.
Plan: wait for the user's actual phase 6 request, then keep the first slice prototype-oriented. Select or create an implementation Worktrunk worktree before starting the local dev stack with `moon run root:dev-up`.

## 2026-05-23 17:07 — Implementation start
Goal for this slice: implement only the `CardanoDBSync` CRD/API surface from the reviewed plan.
Current state of the world: implementation branch `feat/cardano-dbsync-crd` was created from `origin/master` in `.wt/feat-cardano-dbsync-crd`. The local Kind/Tilt dev stack was started from that worktree with `moon run root:dev-up` and reported ready.
Plan: add API types, scheme registration, PROJECT metadata, generated CRD/deepcopy output, and minimal API registration coverage. Do not add controller, RBAC, Helm workload, CLI, devconfig, example, or Chainsaw runtime behavior in this slice.

## 2026-05-23 17:13 — CRD slice implemented
Implemented the staged `CardanoDBSync` API-only slice in `.wt/feat-cardano-dbsync-crd`: typed API structs, scheme registration, PROJECT resource metadata, generated deepcopy output, generated CRD YAML, and a lightweight scheme registration test.
Validation passed with `moon run root:check`, `moon run root:test`, `git diff --check`, and `git diff --cached --check`. The implementation remains intentionally limited to the CRD/API surface; no controller, RBAC, Helm workload templates, CLI, devconfig, examples, or Chainsaw runtime tests were added.

## 2026-05-23 19:09 — PR opened
Opened PR #17 for `feat(api): add CardanoDBSync CRD`: https://github.com/meigma/yacd/pull/17.
The branch head is signed commit `57643077eae2ae378be48c1d4e15945aa13ea15a`. GitHub checks reached a clean terminal state: `ci` passed, Kusari Inspector passed, and release dry-run jobs were skipped.

## 2026-05-23 19:13 — Close
Merged PR #17 with a squash commit: https://github.com/meigma/yacd/pull/17.
The primary checkout was fast-forwarded to `master` at `c25390355e73080c731705cc810508aad4fe444d`, the `feat/cardano-dbsync-crd` worktree was removed, and the local dev stack was stopped with `moon run root:dev-down`.
Closeout artifacts were written in the journal branch: `.journal/011/SUMMARY.md`, the session row in `.journal/INDEX.md`, and a compact db-sync CRD note in `.journal/TECH_NOTES.md`.
