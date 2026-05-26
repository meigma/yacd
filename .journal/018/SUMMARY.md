---
id: 018
title: dbsync planner package refactor
date: 2026-05-26
status: complete
repos_touched: [yacd]
related_sessions: [011, 013, 014, 015, 017]
---

## Goal
Kick off a multi-PR readability/maintainability/architectural-purity refactor sweep across the YACD codebase, starting with `internal/cardano/dbsync`. The package's 560-line `plan.go` monolith, missing field godocs, inverted-default Runtime booleans, and all-or-nothing Insert defaults all violate the rubric the user laid out for this sweep.

## Outcome
The goal was met. PR #35 merged as `e030333` with the squash title `refactor(dbsync): split planner package and freeze identity wire`. CI (Go unit + envtest, fmt, lint, helm, chainsaw manifests) and Kusari Inspector passed before merge. Local `master` was fast-forwarded, the implementation worktree and branch were removed, and the dev stack was stopped.

## Key Decisions
- Mirror the `localnet` sibling layout (`doc.go`, `types.go`, `defaults.go`, `normalize.go`, `validate.go`, `config.go`, `topology.go`, `fingerprint.go`, `env.go`, `plan.go`) rather than inventing a new structure -> the convention is established in the repo, and forcing dbsync to match makes the next refactor target easier to predict.
- Keep `Plan.Spec` as the public normalized projection rather than hiding it behind a slimmer view -> the controller legitimately reads `Spec.Paths`, `Spec.Storage.StateStorageSize`, and `Spec.Database.*`; the projection idea would have hurt consumer ergonomics without a real boundary win.
- Reject sharing `Invocation` / `Fingerprint` with the `localnet` sibling -> the rubric explicitly prefers small bespoke contracts. Each domain owns its own.
- Rename `Runtime.Cache`/`EpochTable` to `DisableCache`/`DisableEpochTable` -> the new names are zero-value safe and match the CLI flag spelling, eliminating the double-negative trap; controller call site flipped to `settings.DisableCache = !crdRuntime.Cache` so end-user CRD semantics stayed unchanged.
- Export `DefaultInsertOptions()` rather than removing the all-or-nothing zero collapse -> the controller relies on the collapse for the empty-Insert case, while the export gives new callers an obvious safe baseline to start from.
- Add idiomatic camelCase JSON tags to `Spec` and accept the one-time `Fingerprint` shift across pre-prod CRs -> the plan fingerprint just triggers a rollout (not a rejection), and pre-production is the cheapest time to take the hit.
- Freeze the `DatabaseIdentityFingerprint` wire shape behind private legacy-shape structs (`insertIdentity`, `txOutIdentity`, `featureSelectionIdentity`) -> the controller treats database identity as immutable and rejects drift as `UnsupportedDatabaseIdentityChange`; an inadvertent shift would have scaled db-sync to zero on every existing healthy CR after upgrade. Caught by the review agent and added a regression test pinning the legacy hash for `minimalSpec()` so future drift fails immediately.

## Changes
- `internal/cardano/dbsync/*` - 560-line `plan.go` monolith split into 10 focused files mirroring the `localnet` sibling layout; godocs added to every exported type, field, constant, and function; `validateSpec` switched to `errors.Join`; hard-coded log rotation/scribes/tracing extracted into named factories; `featureConfigFor` ceremony reduced to a typed conversion; `runtimeInvocation` renamed to `buildInvocation`.
- `internal/cardano/dbsync/types.go` - renamed `Runtime.Cache`/`EpochTable` to `DisableCache`/`DisableEpochTable`; added camelCase JSON tags to every Spec input field for plan fingerprint stability; reordered types so input contracts precede the output Plan.
- `internal/cardano/dbsync/defaults.go` - exported `DefaultInsertOptions()` as the recommended Insert construction baseline; grouped constants by concern (database / runtime / storage / paths / insert) with godocs.
- `internal/cardano/dbsync/fingerprint.go` - introduced private legacy-shape structs (`insertIdentity`, `txOutIdentity`, `featureSelectionIdentity`) that freeze the `DatabaseIdentityFingerprint` wire shape independent of public-type JSON-tag evolution.
- `internal/cardano/dbsync/plan_test.go` - tests updated for the rename and `DefaultInsertOptions()` export; `TestSpecJSONShapeIsStable` locks the Spec JSON key set; `TestDatabaseIdentityFingerprintIsFrozenAgainstLegacyWire` pins the pre-refactor identity hash for `minimalSpec()` so any wire-shape regression fails immediately.
- `internal/controller/cardanodbsync/workload_builder.go` - `runtimeSettings` inverted to map the CRD's positive Cache/EpochTable fields into dbsync's new disable booleans; the redundant `Cache: true`/`EpochTable: true` defaulting is dropped because the new zero value already means "active".

## Open Threads
- The full plan `Fingerprint` value shifted with the new Spec JSON tags; pre-prod CRs will re-roll their stored fingerprint on the next reconcile (no rejection, just one extra rollout). Acceptable since YACD has no production users yet.
- The next packages in the refactor sweep are still TBD; the rubric and approach captured here should generalize. The user-confirmed approach (file-level categorization, godoc discipline, footgun reduction, identity-wire preservation when applicable) should be reused.
- Redundant default knowledge still lives in `internal/controller/cardanodbsync/workload_builder.go` for `storageSettings` (`LedgerBackend: "lsm"`, `NearTipEpoch: 580` re-encoded outside the planner). This is a controller-package concern and was intentionally deferred to the next sweep target.

## References
- PR #35: https://github.com/meigma/yacd/pull/35
- Merge commit: `e030333dbdf81354e09d169ee95aa916682487ff`
- Session notes: `.journal/018/NOTES.md`
- Plan file: `/Users/josh/.claude/plans/we-re-going-to-do-stateless-platypus.md`
- Prior dbsync sessions: `.journal/011/SUMMARY.md`, `.journal/013/SUMMARY.md`, `.journal/014/SUMMARY.md`, `.journal/015/SUMMARY.md`
- Related ctrlkit foundation: `.journal/017/SUMMARY.md`
