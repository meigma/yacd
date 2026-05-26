---
id: 019
title: localnet planner package refactor
date: 2026-05-26
status: complete
repos_touched: [yacd]
related_sessions: [018]
---

## Goal
Apply a targeted readability/maintainability/architecture cleanup pass to `internal/cardano/localnet`, mirroring the bar already set by `internal/cardano/dbsync` (which was refactored to the same bar in session 018, PR #35). First package in a multi-package series; each package gets its own branch and PR.

## Outcome
The goal was met. PR #36 merged as `72e376c` with the squash title `refactor(localnet): tighten file layout and godoc bar`; CI and Kusari Inspector passed. The session worktree was removed and `master` was fast-forwarded in the primary checkout. Public API, JSON tags, fingerprint algorithm, and arg-grammar are byte-for-byte unchanged — the existing `plan_test.go` (with the pinned `8523eefd...26aa80` default fingerprint and pinned `--slot-length 0.1` output) passed without modification, and the downstream `internal/controller/cardanonetwork` consumers (`workload_builder.go`, `init_container.go`, and their tests) built and tested without edits.

## Key Decisions
- Split assessment across three parallel Explore agents (dbsync reference pattern, localnet caller contract, `go-style`/`go-testing` skill rules), then validated through one Plan agent — the user explicitly asked for split-and-synthesize work, and the assessment dimensions were independent enough to parallelize cleanly.
- Mirror dbsync's file layout exactly (`defaults.go`, `normalize.go`, `validate.go`, `fingerprint.go`, `plan.go`, plus a domain-specific render file) — `dbsync` is the merged reference and consistency across the two sibling packages is itself a readability win.
- Keep `manifestSchemaVersion` co-located with `computeFingerprint` in `fingerprint.go` instead of inventing a one-const `manifest.go` — `Manifest` embeds `Fingerprint`, so they form a single wire trio; splitting them would be over-categorization.
- Inline `Layout` and `Manifest` struct assembly in `BuildPlan` (4 lines each) instead of extracting `buildLayout`/`buildManifest` helpers — the Plan agent proposed both helpers, but four-line struct literals don't form their own category and extracting them adds files without aiding comprehension.
- Extract `buildCreateEnvInvocation` into the new `invocation.go` (replaces `format.go`) — cardano-testnet CLI grammar (`--num-pool-nodes`, `--testnet-magic`, etc.) is a real category that justifies its own home and could grow; `format.go` was a generically named one-function file that hid this category.
- Rename `cleanAbsolutePath` → `normalizeContainerPath` (private, two call sites) — "clean" hid that this is the path-normalization step in `normalizeSpec`; the new name reuses the package's "normalize" verb and explicit "container path" domain term.
- Skip `moon run root:dev-up` for this session — pure side-effect-free domain code with no controller, manifest, or runtime change; Kind/Tilt provides no signal. `moon run root:test` (envtest-aware) was sufficient.
- No ports/adapters introduced — `localnet` is pure side-effect-free domain code and the user's rule 7 explicitly forbids port/adapter pairs for pure code; matches the dbsync precedent.

## Changes
- `internal/cardano/localnet/doc.go` - expanded the one-line package doc into a contract paragraph (pure-domain planner, no side effects, what `BuildPlan` returns).
- `internal/cardano/localnet/types.go` - added a shared "JSON tags are the wire contract" note above the `Fingerprint`/`Manifest`/`ManifestInputs` trio; tightened `Tool.Binary`, `ManifestInputs.SlotLength`, and `Manifest.Inputs` field comments toward a single vocabulary ("fingerprint inputs").
- `internal/cardano/localnet/defaults.go` - new file owning the `default*` constants, the generated-environment filename constants, and `DefaultSpec()`; section comments group default values vs. emitted filenames.
- `internal/cardano/localnet/normalize.go` - new file owning `normalizeSpec` (moved from `plan.go`) and the renamed `normalizeContainerPath` (moved from `validate.go`); added an inline comment on the `EnvDir` derivation.
- `internal/cardano/localnet/validate.go` - reduced to `validateSpec` only; tightened the doc comment.
- `internal/cardano/localnet/fingerprint.go` - kept `computeFingerprint`, `fingerprintAlgorithm`, and `manifestSchemaVersion` co-located; rewrote `manifestSchemaVersion`'s comment and added a tag-stability sentence to `computeFingerprint`.
- `internal/cardano/localnet/invocation.go` - new file replacing `format.go`; hosts the moved `formatSlotLength` (with a one-line CLI-format clarification) and the new private `buildCreateEnvInvocation` helper.
- `internal/cardano/localnet/plan.go` - reduced to the `BuildPlan` orchestrator only; rewrote the godoc to describe what the returned `Plan` carries while preserving the existing zero-value-as-default paragraph.
- `internal/cardano/localnet/format.go` - deleted; contents moved into `invocation.go`.

## Open Threads
- This is the first of a multi-package refactor series. Subsequent packages will each get their own branch + PR; the user has not yet picked the next package.
- No follow-ups in `localnet` itself: the package matches `dbsync`'s bar and the public API contract is preserved.

## References
- PR #36: https://github.com/meigma/yacd/pull/36
- Squash merge commit: `72e376cb60ab63ca467af95da0a119167f29127c`
- Reference pattern (sibling package, prior session): `.journal/018/SUMMARY.md` and PR #35
- Session notes: `.journal/019/NOTES.md`
