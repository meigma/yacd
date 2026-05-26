---
id: 021
title: cardanonetwork controller refactor
started: 2026-05-26
---

## 2026-05-26 09:01 — Kickoff
Goal for the session: refactor `internal/controller/cardanonetwork` for readability, maintainability, and architectural purity, mirroring the planner-bar set by sessions 018 (dbsync planner, PR #35) and 019 (localnet planner, PR #36). First controller-package target in a multi-PR sweep.
Current state of the world:
- `master` at `72e376c refactor(localnet): tighten file layout and godoc bar (#36)`; primary checkout tree is clean.
- Most recent merged work was the `internal/cardano/localnet` readability/layout refactor in session 019 (PR #36), which mirrored the `internal/cardano/dbsync` planner package split done in session 018 (PR #35). Both PRs preserved public API and pinned fingerprints byte-for-byte.
- The multi-package refactor sweep started in session 018 is still in progress; `localnet` and `dbsync` planners are done, but the next target package has not been picked. Open thread from 018 also notes redundant default knowledge still living in `internal/controller/cardanodbsync/workload_builder.go` (`storageSettings.LedgerBackend: "lsm"`, `NearTipEpoch: 580`) as a deferred follow-up.
- A dangling session 020 was started earlier today (2026-05-26 08:22) with only a kickoff entry, no stated goal, and no `SUMMARY.md`; no implementation worktree or PR was opened for it. It should be closed out separately so the index stays honest.
- No active implementation worktree; only `master` (primary checkout) and `journal/jmgilman` (`.wt/journal-jmgilman`) are present.
- Dev stack is not running; will start `moon run root:dev-up` from an implementation worktree only if implementation work is needed.
Plan: wait for the user's actual request, then decide whether to continue the refactor sweep (next package TBD), pick up the deferred `workload_builder.go` storage-default cleanup, address the dangling session 020, or do something new. Select or create the appropriate implementation worktree and bring up the dev stack only when implementation work is on the table.

## 2026-05-26 09:18 — Goal set + plan approved
User goal: targeted readability/maintainability/architectural-purity refactor pass on `internal/controller/cardanonetwork`, first controller-package target in the multi-PR sweep. Same rubric as sessions 018 (`internal/cardano/dbsync`, PR #35) and 019 (`internal/cardano/localnet`, PR #36).

Phase 1 exploration: 3 Explore agents in parallel (readability / maintainability / architectural purity), all converged on the same shape — package is already architecturally sound thanks to ctrlkit (no new ports justified), but file layout, godoc/comment discipline, and a small number of maintainability footguns/reuse gaps are below the planner bar.

Plan approved: `/Users/josh/.claude/plans/we-re-going-to-do-zazzy-widget.md`. Target structure: 20 focused files (from 6 today); strong-type conditions; consolidate readiness/readers/kupo cascade/annotations; rewrite faucet auth secret on `ctrlapply.ApplyOwnedObject`; extract pure token helpers; keep network artifacts ConfigMap apply inline (recovery semantics don't fit ApplyOwnedObject). Behavior contract: same wire-shape conditions/reasons, byte-for-byte equivalent k8s objects modulo annotation key ordering.

## 2026-05-26 09:24 — Halted: parallel session 020 collision discovered
Implementation worktree `refactor/controller-cardanonetwork` was created from master successfully, but `moon run root:dev-up` failed because Tilt is already running for `/Users/josh/code/meigma/yacd/.wt/refactor-ctrlkit-package`.

Investigation: my earlier kickoff entry mis-characterized session 020 as a dangling kickoff-only session. Reading `.journal/020/NOTES.md` directly shows that 020 is in fact a **parallel active session** doing the `internal/ctrlkit` refactor, with PR #37 (`refactor/ctrlkit-package`, branch is 5 commits ahead of master and pushed to origin) **awaiting CI/Kusari + user review**. Their dev stack is intentionally still running per session protocol ("only stop at explicit close").

The user is evidently running two sibling refactor sessions in parallel — ctrlkit (020) and cardanonetwork (021).

Coordination problem with PR #37 (session 020):
- Their commit `5746bef refactor(ctrlkit/readiness): tighten state naming and collapse result struct` already touched `internal/controller/cardanonetwork/status.go` to consume the renamed `ctrlreadiness.DeploymentReadiness*` types. If I refactor `status.go` on top of master, my branch will conflict with theirs on `status.go` at minimum.
- The dev stack is a singleton; I cannot bring up my own without stopping theirs.
- My branch `refactor/controller-cardanonetwork` is currently based on master (which is one commit behind their branch tip).

Did NOT stop their dev stack. Did NOT touch their worktree. Halted my implementation pending user direction.

Asked user how to proceed: (a) pause 021 until #37 merges, (b) base 021 on #37's branch and accept the bundled rebase, (c) proceed on master without dev-up and accept rebase-on-merge cost, (d) something else.
