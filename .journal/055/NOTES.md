---
id: 055
title: Local lifecycle plan — P6 (devnet all-in-one)
started: 2026-06-02
---

## 2026-06-02 12:31 — Kickoff
Goal for the session: continue executing the session-049 `LOCAL_LIFECYCLE_PLAN.md`
for the `yacd devnet` all-in-one local lifecycle, picking up after session 054.
Plan status: `P1✅ P4✅ P5✅ P2✅ P3✅` — remaining `P6 → P7`. Next up is **P6**.

Current state of the world (from sessions 053 + 054 SUMMARYs and TECH_NOTES):
- Operator releases `v0.1.0` (P1) + `v0.1.1` funded wallet (P4) are published +
  attested in GHCR. P5 embeds `v0.1.1` (NOT the plan's "0.2.0" — pre-1.0 `feat`s
  bump PATCH here). Pinned refs: manager
  `ghcr.io/meigma/yacd:v0.1.1@sha256:5d53ca…f0f8e21`, faucet
  `…/faucet:v0.1.1@sha256:826f8d…fd3f66`, chart `…/chart:0.1.1@sha256:a8d24d…8a5a049f`.
- All P6 dependencies are merged + library-only under `cli/internal/`, each with a
  generated mock in `cli/internal/mocks`, none wired into `options.go` yet:
  - **P5** `operator.Installer` port + `operator/ssa` adapter (embedded
    `manifests/operator.yaml` SSA apply, install ns pinned `yacd-system`).
  - **P2** `toolbin.Resolver` port + `toolbin/ghrelease` adapter (pinned k3d v5.9.0,
    embedded SHA256, `DefaultK3dPin`, `DefaultDir`).
  - **P3** `exec.Runner` seam; `cluster.Provisioner` port (+`ManagedName`/
    `ManagedContext`/`K3sImage` consts) + `cluster/k3d` adapter (`EnsureCluster`
    state machine); `clusterstate.Store` port + `clusterstate/file` adapter (atomic
    JSON + gofrs/flock; lock composed by P6, not held inside cluster/k3d).
- `master` @ `1c12560` (P3, PR #89). Clean tree, no impl worktree yet, dev stack down.

P6 scope (Plan §"Phase 6"; Design §10.4/§10.5/§5–§9):
- `cli/internal/lifecycle` — `Manager` orchestrating the ports (`Up`/`Down`/`Status`),
  unit-tested against the 4 generated mocks (no Docker).
- `cli/internal/cli/devnet.go` — thin `devnet` / `devnet down` / `devnet status`
  subtree; embedded default local Environment (Conway, 1 pool, Ogmios+Kupo+faucet+
  wallet); stepwise progress to stderr; print funded wallet address + exec tip-query hint.
- `cli/internal/cli/target.go` — ONE shared targeting resolver (Design §6 precedence)
  wired into ALL existing verbs.
- `cli/internal/cli/options.go` — add the deferred factory fields.
- GUARDRAIL: managed-context tier only engages when a managed cluster exists, so
  CI/Chainsaw (explicit KUBECONFIG/--context) stay green with NO test edits.
- New gated k3d e2e (provision→install→network→fund→use→down); reuse existing `up`
  apply/wait path; don't re-implement CR rendering/readiness.

Plan: awaiting the user's next instructions before starting implementation. Will need
to create an implementation worktree from `origin/master` (lesson from 054: branch
from `origin/master`, not stale local default) and run `moon run root:dev-up` once
in that worktree before substantive work.

## 2026-06-02 13:45 — P6 implemented + live-proven (pre-PR)
Approved plan (single PR; write+run the gated live e2e). Impl worktree
`feat/cli-devnet` off `origin/master` (1c12560); dev stack up.

Shipped (all under `cli/`):
- `lifecycle` pkg (`Manager` Up/Down/Status + `Reporter`/`NopReporter`, injectable
  `CaptureContext`/`RestoreContext` seams). Up: lock → capture prior ctx BEFORE
  EnsureCluster → EnsureCluster → reconcileRecord+Save (preserves real prior across
  re-runs; record-repair) → EnsureOperator → unless Bare: render embedded env +
  EnsureNamespace+Apply+WaitReady. Down: lock → DeleteCluster → restore prior ctx
  (best-effort) → Clear. Status: no lock; Provisioner.Status + OperatorState + list.
- `cli/target.go`: `ResolveTarget` (explicit > managed record > ambient) +
  `resolveKubeClient` helper + `announceManagedTarget` (fires ONLY on managed tier,
  to stderr — zero-risk for existing tests). Wired into all 8 verbs.
- `cli/devnet.go`: `devnet [--bare]`/`down`/`status`, stderr `stepReporter`, stdout
  result (cluster/operator/endpoints/wallet + magic-interpolated exec hint).
- `cli/embed.go`+`devnet.yaml` (byte-identical copy of examples/local/yacd.yaml,
  drift-guard test).
- `kube/context.go`: `CurrentContext`/`SetCurrentContext` (clientcmd seams).
- Options/root wiring: 3 factory fields (`ClusterProvisionerFactory`,
  `OperatorInstallerFactory`, `ClusterStateFactory`) + `k3dVersion`; root.go is the
  only adapter-importing site. info gained additive wallet output.

KEY DECISION: `announceManagedTarget` only prints when tier-2 (managed) engages, so
explicit-target (CI/Chainsaw, all existing verb tests) and ambient resolution stay
silent → existing verb tests pass with NO edits (confirmed: `root:test` green).

Verification: `root:generate` idempotent (operator render stable, mocks unchanged);
`root:check` green; `root:test` green (incl. all envtest pkgs + every existing verb
test, no edits). **Live e2e PASSED (88.6s)**: provision k3d → install operator v0.1.1
→ funded network Ready → `exec ... query tip --testnet-magic 42` → idempotent re-run
→ `devnet down` removed cluster + restored prior context (verified: no k3d containers,
context back to kind-yacd-dev).

LESSON: the k3d adapter (P3) hardcodes `KubeconfigPath: clientcmd.RecommendedHomeFile`
but k3d honors `$KUBECONFIG`. So overriding KUBECONFIG in a test breaks the operator
install (context not in the reported file). Latent for real KUBECONFIG-set users — a
P7 follow-up (adapter should report the path k3d actually wrote). Live test uses the
real default kubeconfig (the real product path) + restores prior ctx.

NEXT: commit, open PR (links plan+design, names Phase 6), then session close (dev-down).

## 2026-06-02 14:05 — Review fixes (PR #90, a4fee80)
Review agent raised 3 findings. Disposition:
- **P1 (FIXED):** k3d adapter `infoFor` reported `clientcmd.RecommendedHomeFile`,
  but k3d `--kubeconfig-update-default` honors `$KUBECONFIG`. → `defaultKubeconfigPath()`
  = `NewDefaultClientConfigLoadingRules().GetDefaultFilename()`. Re-added KUBECONFIG
  isolation to the live e2e (temp file) — PASSED (90.8s), proving install/apply/restore
  work off a non-home kubeconfig; real ~/.kube/config untouched. Coverage gap closed.
- **P2 (DEFER — documented design):** ResolveTarget intentionally reads the cheap
  record, not a Docker/cluster probe (design §6), to keep read verbs Docker-free. A
  cluster deleted out-of-band → stale targeting that self-heals on next `devnet`; the
  "cluster gone → run yacd devnet" hint + status-driven repair are P7 (failure
  taxonomy/UX). Normal `devnet down` already clears the record. Kept `status` read-only
  (no lock, no mutation) for P6. No change.
- **P3 (FIXED):** `devnet` + `devnet down` now reject `--timeout <= 0` (matches up/down),
  + unit test `TestDevnetRejectsNonPositiveTimeout`.
`root:check` + `root:test` green after fixes.

## 2026-06-02 14:30 — P2 mitigation implemented (orphan reconcile, 41ef1ec)
User opted to address P2 now rather than defer. Implemented on-fail orphan cleanup
(NOT a happy-path probe):
- `commandContext.managedEngaged` set by `resolveKubeClient` when tier-2 (managed
  record) engages (via new `isManagedTarget` predicate, shared with announce).
- `orphan.go`: `withManagedReconcile` wraps each of the 8 network verbs' RunE in
  root.go (no verb-body edits). On error AND managedEngaged → `clearOrphanedManagedState`:
  build provisioner → `Status(ManagedName)` → if `!Exists` → `clusterState.Clear()` +
  stderr notice. Best-effort (swallows its own errors; never masks the original).
  Keys on `!Status.Exists` (definitive: k3d has no such cluster), so a transient API
  blip on a live cluster won't wipe the record. Original error preserved; next call
  resolves ambient.
- `devnet status` clears a stale record at its reconciliation point (`!Exists &&
  HasRecord`) with the same notice.
- Tests: `orphan_test.go` (gone→clear+msg / present→keep / explicit→no probe) +
  devnet status stale-record case. `root:check` + `root:test` green. (Live e2e
  doesn't exercise the orphan path, left as-is.)
Pushed a4fee80..41ef1ec. Awaiting CI re-run + merge decision.

## 2026-06-02 14:55 — Review round 2 fixes (3490031)
Second review (done before 41ef1ec). Disposition:
- **Stale records hijack (P2.1):** already fixed by 41ef1ec (orphan reconcile) — the
  reviewer hadn't seen it. Covers out-of-band delete + down-fails-before-clear (next
  managed-targeted verb probes + clears).
- **Corrupt state blocks teardown (FIXED):** `Manager.Down` aborted on `Load` error,
  blocking DeleteCluster. Runtime is authoritative → Down now logs + proceeds (found=
  false), deletes the cluster, and Clear()s the bad record. Test: "tears down even when
  the state record is unreadable".
- **Operator readiness over-reported (FIXED):** `Up` printed "Operator ready" even when
  SSA returned not-ready (it applies but doesn't wait for the manager Deployment). New
  `ensureOperatorReady` polls `OperatorState` (interval 3s, bounded by `--timeout`,
  immediate=true so warm/ready installs don't sleep) → honest message + `--bare`
  delivers a ready operator. Test: "waits for the operator to become ready...".
`root:check` + `root:test` green. **Live e2e re-run PASSED (88.8s)** — now exercises the
readiness-wait path; clean teardown, context restored. Pushed 41ef1ec..3490031.

## 2026-06-02 15:35 — Review round 3 fixes (2c86516)
Four findings, all fixed:
- **P1 — healthy cluster deleted on KUBECONFIG drift:** `statusVia` probed health via
  ambient kubeconfig → a current config lacking k3d-yacd marked a running cluster
  unhealthy → EnsureCluster delete+recreate. Added `cluster.Spec.KubeconfigPath`;
  `Up` now loads the record BEFORE EnsureCluster and passes `existing.KubeconfigPath`
  into the spec; adapter probes through it. `reconcileRecord`→`buildRecord` (takes the
  already-loaded record). Tests: adapter `TestEnsureClusterProbesHealthThroughSpecKubeconfig`
  + manager "probes existing-cluster health through the recorded kubeconfig".
- **P2a — devnet ignored --kubeconfig/--context:** added `rejectExplicitTarget` to all 3
  devnet subcommands (devnet manages its own cluster/kubeconfig; flags apply to network
  verbs; isolation is a P7 feature). Test `TestDevnetRejectsExplicitTarget`.
- **P2b — lock acquisition unbounded:** `Up` now wraps the whole op in
  `context.WithTimeout(o.Timeout)` like `Down` → `--timeout` bounds lock + all phases.
- **P3 — status could fetch k3d to report "no cluster":** `Status` loads the record
  first; `!found` → report absent without probing the runtime (a managed cluster always
  leaves a record; out-of-band clusters are reconciled by `devnet`). Tests updated.
`root:check` + `root:test` green. **Live e2e re-run PASSED (97.8s)** — clean teardown,
context restored. Pushed 3490031..2c86516.

## 2026-06-02 17:20 — Close
P6 merged and session closed. **PR #90 squash-merged (`db7887b`)** after four review
rounds; all CI gates green (`ci`, `e2e`, Kusari, cardano-tools-image). Local `master`
fast-forwarded `1c12560..db7887b`; impl worktree `feat/cli-devnet` removed; remote
branch deleted; dev stack down (Kind cluster + registry deleted, no docker leftovers).
Note: `gh pr merge --squash --delete-branch` errored on its LOCAL branch-delete
("master already used by worktree") but the remote merge succeeded — verified via
`gh pr view --json state` (MERGED), then ff'd master + deleted the remote branch by hand.
Plan status: `P1✅ P4✅ P5✅ P2✅ P3✅ P6✅` — only **P7 (hardening & UX)** remains
(incl. the `--isolate-kubeconfig` flag devnet currently rejects). SUMMARY + INDEX
(→complete) + TECH_NOTES (P6 bullet) written.
