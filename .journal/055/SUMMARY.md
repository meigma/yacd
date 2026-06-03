---
id: 055
title: Local lifecycle plan — P6 (devnet all-in-one)
date: 2026-06-02
status: complete
repos_touched: [yacd]
related_sessions: [049, 053, 054]
---

## Goal
Execute the final core phase of the session-049 `LOCAL_LIFECYCLE_PLAN.md`: **P6**,
the user-facing `yacd devnet` all-in-one. Compose the library-only ports shipped in
sessions 053–054 (operator SSA install, toolbin k3d resolver, cluster +
clusterstate) into a single command that takes a Docker-only machine to a Ready
local devnet, plus a shared targeting resolver wired into every existing verb.

## Outcome
**Met.** Shipped as **one squash-merged PR (#90, `db7887b`)**, CLI-only. The plan's
core sequence is now complete: `P1✅ P4✅ P5✅ P2✅ P3✅ P6✅` — only **P7
(hardening & UX)** remains. `yacd devnet` provisions a managed k3d cluster, installs
the operator (v0.1.1), applies a default funded local network, and prints endpoints
+ the funded wallet address + a magic-interpolated `exec` tip-query hint; `devnet
down`/`status` round it out. Verified by unit tests against the port mocks **and a
gated `YACD_DEVNET_LIVE` k3d end-to-end run live four times** (provision → install →
funded network Ready → in-pod `cardano-cli query tip` → idempotent re-run → teardown
+ context restore), each ~90s with clean teardown. The PR absorbed **four rounds of
adversarial review** (8 findings) before merge; all CI gates (`ci`, `e2e`, Kusari,
cardano-tools-image) green.

## Key Decisions
- **Targeting resolver reads the cheap state record, never probes Docker** (Design
  §6). Precedence: explicit `--kubeconfig`/`--context` (or `YACD_*`) > tracked
  managed context (when a record exists) > ambient. This keeps read verbs
  Docker-free and makes the resolver a verified **no-op for CI/Chainsaw** (explicit
  target = tier 1) and never-ran machines (no record = tier 3) → existing verb tests
  passed with **zero edits**.
- **The managed-target announce + orphan-reconcile fire only when the managed tier
  engages**, so explicit/ambient usage (all existing tests, scripted/CI) is silent.
- **On-fail orphan cleanup, not happy-path probing** (review P2). A managed-targeted
  verb that fails probes `Provisioner.Status`; on `!Exists` it clears the stale
  record + prints a notice (next call → ambient). `devnet status` reconciles at its
  natural point. Keyed on `!Exists` (definitive) so a transient API blip never wipes
  a live cluster's record.
- **`devnet` rejects explicit `--kubeconfig`/`--context`** rather than silently
  ignoring them (review P2a); honoring them (isolated kubeconfig) is a P7 feature.
- **Operator readiness is waited for, not assumed** (review P2.3). SSA applies but
  doesn't wait for the manager Deployment; `Up` now polls `OperatorState` (bounded by
  `--timeout`) so the "ready" report is honest and `--bare` returns a usable operator.
- **A running cluster is probed for health through its *recorded* kubeconfig**
  (review P1, the most serious): the ambient-kubeconfig probe meant a `KUBECONFIG`
  change marked a healthy cluster unhealthy → `EnsureCluster` deleted+recreated it.
  `Up` now loads the record before `EnsureCluster` and threads
  `cluster.Spec.KubeconfigPath` into the probe.
- **The k3d adapter reports the real default-kubeconfig target** (`GetDefaultFilename`,
  honoring `KUBECONFIG`) instead of hardcoded `~/.kube/config` (review P1, round 1),
  so install/apply/restore find the context k3d actually wrote.
- **Embedded default env is a byte copy of `examples/local/yacd.yaml`** in the cli
  package (`go:embed` can't reach outside the package dir), drift-guarded by a test.
- **`Down` and `status` are robust to bad/absent state**: `Down` tears down even when
  the record is corrupt (runtime is authoritative); `status` reports absent from a
  missing record without resolving/fetching the k3d binary.

## Changes
All under `cli/` (squash `db7887b`, PR #90):
- **`cli/internal/lifecycle/`** (new) — `Manager` (`Up`/`Down`/`Status`), `Reporter`/
  `NopReporter`, injectable context capture/restore seams, `ensureOperatorReady` poll,
  `buildRecord` (prior-context preservation + record repair).
- **`cli/internal/cli/target.go`** (new) — `ResolveTarget`, `resolveKubeClient`,
  `isManagedTarget`, `announceManagedTarget`, `rejectExplicitTarget`.
- **`cli/internal/cli/orphan.go`** (new) — `withManagedReconcile` wrapper +
  `clearOrphanedManagedState`/`clearManagedStateRecord`.
- **`cli/internal/cli/devnet.go`** + `embed.go` + `devnet.yaml` (new) — the
  `devnet`/`down`/`status` subtree, stderr `stepReporter`, stdout result output.
- **`cli/internal/kube/context.go`** (new) — `CurrentContext`/`SetCurrentContext`.
- **`cli/internal/cli/{options,root}.go`** — 3 factory fields + `k3dVersion`; root is
  the only adapter-importing composition site; 8 verbs wrapped with the reconcile.
- **8 verbs** (`up/down/list/info/run/exec/connect/topup`) — `kube.Config{...}` →
  `resolveKubeClient`; mutating verbs `announceManagedTarget`. `info` gained additive
  wallet output.
- **`cli/internal/cluster/`** — `Spec.KubeconfigPath`; `k3d` adapter reports the real
  default-kubeconfig path and probes health through the spec's kubeconfig.
- Tests throughout (manager/target/orphan/devnet unit + adapter + gated live e2e).

## Open Threads
- **P7 — Hardening & UX** is the only remaining plan phase: typed failure taxonomy →
  actionable messages (Docker down, port conflict, disk pressure, checksum/version
  mismatch), Docker/VM-disk preflight, `devnet down --purge` + binary GC surfacing,
  first-run banner, WSL2 validation, an ARM multi-arch CI guard, **and the
  `--isolate-kubeconfig` flag** (honor `--kubeconfig`/`--context` for devnet, which
  P6 currently rejects).
- **k3d adapter residual edge:** if a healthy cluster's *recorded kubeconfig file is
  deleted entirely* (not just KUBECONFIG-drift), the probe still fails → recreate.
  Rarer than drift; a future robustness item (e.g. re-merge via `k3d kubeconfig`
  instead of recreate).
- release-please queued the pre-1.0 **PATCH** bump on the open root release PR (#7),
  as expected from each `feat(cli)`.
- Carried: operator GitHub **draft** releases `v0.1.0`/`v0.1.1` still need a human to
  Publish (GHCR artifacts already public); deterministic primary-sidecar
  manager-envtest refactor; TEST_REPORT F2/F4; test-harness `yacd-env` Action +
  examples/how-to; a dedicated docs session.

## References
- PR: #90 (`db7887b`, squash-merged). Plan/design: `.journal/049/
  LOCAL_LIFECYCLE_{PLAN,DESIGN}.md`. Prior: `.journal/054/SUMMARY.md` (P5/P2/P3),
  `.journal/053/SUMMARY.md` (P1/P4 releases).
- Published refs P5/P6 embed: manager `ghcr.io/meigma/yacd:v0.1.1@sha256:5d53ca…`,
  faucet `…/faucet:v0.1.1@sha256:826f8d…`, chart `…/chart:0.1.1@sha256:a8d24d…`.

## Lessons
- **A cluster health probe must not depend on the ambient kubeconfig.** Probing
  through whatever `KUBECONFIG` currently points at means an unrelated env change
  destroys a healthy cluster. Tie the probe to the kubeconfig the cluster was
  recorded in (runtime/record authoritative), not the caller's current view.
- **Isolating a live test by overriding `KUBECONFIG` can mask *and* expose adapter
  bugs.** The first live run failed because the adapter reported `~/.kube/config`
  while k3d wrote to the temp `KUBECONFIG`; that surfaced review P1. Once the adapter
  honored `KUBECONFIG`, re-adding the override became the regression test.
- **"Cheap record vs. authoritative runtime" is a per-operation choice, not global.**
  Mutating paths (`devnet` up) probe the runtime; read/targeting paths trust the
  record (and reconcile on failure). Forcing a k3d binary download to answer a
  read-only `status` on a clean machine is the wrong trade.
- **`gh pr merge --squash --delete-branch` fails its local branch-delete step when
  the default branch is checked out in another worktree** ("master is already used by
  worktree"). The remote squash-merge still succeeds — verify `gh pr view --json
  state`, then fast-forward the main checkout and delete the remote branch manually.
