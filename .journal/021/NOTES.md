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

## 2026-05-26 10:48 — Implementation complete, PR opened
Session 020 was closed by the user during the pause; PR #37 merged into master at `c4974ab` and the dev stack was released. Rebased `refactor/controller-cardanonetwork` onto fresh master (no conflicts — the ctrlkit/readiness type renames in #37 were absorbed cleanly) and started the dev stack from the implementation worktree.

Implemented across 7 commits on the branch (squash merge will collapse them):
1. `ba76c8d` — extract defaults / names / labels / annotations / errors.
2. `9101295` — extract settings (sidecar types + resolve*) and validate (spec-side validation + image parsers).
3. `1042715` — split containers and resources, slim builder.go (from 1283 → 232 lines; the test file is renamed via `git mv` so blame survives).
4. `f892296` — split apply.go into apply / callbacks / delete / faucet_auth (from 593 → ~170 lines).
5. `a339773` — split status.go into status / readiness / conditions (from 562 → ~210 lines). statusReader collapsed into liveReader.
6. `1f463a2` — add doc.go (package contract paragraph) and tighten remaining godocs (artifacts.go and controller.go internal types + orchestrators).

Final shape: 18 source files (from 6), each one a coherent category — mirrors the `internal/cardano/dbsync` / `internal/cardano/localnet` planner discipline.

Maintainability wins landed:
- statusReader → liveReader consolidation (§3a).
- Kupo cascade default has an explicit comment naming the product rule and clarifying that the dependent "kupo requires ogmios" check is the builder's responsibility (§3c).
- Annotation keys centralized in `cardanoNetworkOwnedAnnotations` so future owned annotations auto-extend `mergeOwnedAnnotations` (§3d).
- Label key strategy documented in `labels.go` (§3e).
- `resolvedDefaultFaucetImage` consolidated next to its caller in settings.go with a comment naming the deliberate controller-side default exception (§3h).
- Pure `validFaucetAuthToken` and `generateFaucetAuthToken` extracted into faucet_auth.go with a `faucetAuthTokenByteLength` const wiring the validator to the generator (§4a).

Deferred to follow-up PRs (recorded in PR body):
- `applyPrimaryFaucetAuthSecret` rewrite onto `ApplyOwnedObject` (§3f) — behavior preserved, but the rewrite expands the test surface and belongs in its own PR.
- Readiness-method consolidation (§3b) — the four primary*ReadyCondition methods stay readable as-is.
- Strong-typed `conditionType` / `conditionReason` (§3g) — would require updating `assertCondition` and every test call site (~50+); cleaner as a focused commit later.

Verification all green from the implementation worktree:
- `moon run root:check` — 23s, gofmt / vet / lint / helm / chainsaw-manifests clean.
- `moon run root:test` — 12s with cache hits, all envtest matrices + unit packages.
- `moon run root:test-e2e` — 3m 29s, Chainsaw `manager-smoke` passed: local-mode CardanoNetwork reached `Ready=True`, returned real Ogmios `queryNetwork/tip` through Service, optional services flipped off cleanly, ownership-protected teardown succeeded.

PR #38 opened: https://github.com/meigma/yacd/pull/38 — awaiting CI/Kusari + user review. Branch pushed; dev stack stopped (`root:dev-down`) per session protocol since this is the explicit close path.

## 2026-05-26 10:54 — PR #38 merged; followup branch created
User merged PR #38 as squash commit `3570d8c`. Fast-forwarded `master` in the primary checkout, removed the implementation worktree, and created a fresh worktree `refactor/cardanonetwork-followups` at `.wt/refactor-cardanonetwork-followups` based on the new master.

## 2026-05-26 11:32 — Followup plan approved, implementation complete
Re-entered plan mode. Rewrote `/Users/josh/.claude/plans/we-re-going-to-do-zazzy-widget.md` for the four deferred items: strong-typing condition vocabulary, sidecar readiness consolidation, kupo cascade two-step, faucet auth reshape. One design pivot from the previous plan: §3f does NOT route the faucet auth Secret through `ctrlapply.ApplyOwnedObject` — verifying ctrlkit's signature showed two hard misfits (Secrets are uncached + ApplyOwnedObject.Mutate does not run on Create). The plan was updated to "reshape inline + document the exception," matching the artifact ConfigMap precedent. User approved with the recommended option.

Started the dev stack from the followup worktree (`moon run root:dev-up`, 45s) and implemented across four commits:

1. `7be0b1d` — strong-type `conditionType` / `conditionReason`. Casts at the ctrlstatus seam keep wire shape unchanged; `assertCondition` / `conditionHas` retype; five `assert.Equal(t, constant, got.Reason)` sites cast back to the typed alias. Caught the testify type-strictness regression on the first run and fixed by casting the live string side of the comparison.
2. `f34684b` — sidecar readiness consolidation. Extract `primarySidecarReadyCondition` + `sidecarReadinessConfig` in `readiness.go`; ogmios/kupo/faucet collapse to adapters. Faucet's Secret-token check plugs in through `cfg.preReadinessCheck` (`faucetAuthSecretReady`). Node stays separate.
3. `815225c` — kupo cascade two-step. `resolveKupoSettings` reads only `spec.ChainAPI.Kupo` now and returns `(kupoSettings, kupoMentioned bool, error)`. New `applyDependentDefaults` encodes the single product rule (kupo follows ogmios when unmentioned). Hard invariant "kupo requires ogmios" stays in builder.
4. `352a480` — faucet auth reshape. Split into `createFaucetAuthSecretWithToken` + `reconcileFaucetAuthSecret`, with `applyPrimaryFaucetAuthSecret` as a small live-reader dispatcher. File-level comment names the two ctrlkit-fit constraints and points at the `applyNetworkArtifactsConfigMap` precedent.

Verification all green from the implementation worktree:
- `moon run root:check` — 22s, gofmt / vet / lint / helm / chainsaw-manifests clean.
- `moon run root:test` — passed including the cardanonetwork envtest matrices; pinned condition strings unchanged on the wire.
- `moon run root:test-e2e` — 4m 17s, Chainsaw `manager-smoke` passed end-to-end with the new readiness consolidation in the runtime path.

PR #39 opened: https://github.com/meigma/yacd/pull/39 — awaiting CI/Kusari + user review. Dev stack still running per protocol (only stop at explicit session close).

## 2026-05-26 12:07 — Close
User approved PR #39 (LGTM) and invoked `session-close`. PR #39 squash-merged as `777ead0`. Local `master` fast-forwarded from `3570d8c` to `777ead0`. `refactor/cardanonetwork-followups` worktree and remote branch removed via `wt remove`. Dev stack stopped via `moon run root:dev-down` — Tilt-managed resources removed, Kind cluster and local registry deleted, `.run/yacd-dev` runtime state gone.

Merged PRs for this session: #38 (`3570d8c`, structural split + godoc + bulk maintainability) and #39 (`777ead0`, typed conditions + readiness consolidation + kupo two-step + faucet auth reshape). `internal/controller/cardanonetwork` is now at the bar set by the planner-package refactors (#33, #35, #36, #37). The next sibling target in the sweep is `internal/controller/cardanodbsync`; the deferred dbsync controller storage-default bleed from session 018's open threads should be addressed during that pass.

Handoff state: clean. `master` at `777ead0`, primary checkout clean, only `journal/jmgilman` worktree remains under `.wt/`.
