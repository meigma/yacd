---
id: 054
title: Local lifecycle plan — P5 (operator SSA install) + P2 (toolbin k3d resolver) + P3 (cluster/clusterstate)
date: 2026-06-02
status: complete
repos_touched: [yacd]
related_sessions: [049, 053]
---

## Goal
Continue executing the session-049 `LOCAL_LIFECYCLE_PLAN.md` for the `yacd devnet`
all-in-one local lifecycle, picking up after session 053 shipped P1 (operator
`v0.1.0`) and P4 (funded wallet, `v0.1.1`). Land the remaining library-only port/
adapter slices that P6 (the user-facing `devnet`) depends on.

## Outcome
**Met.** Three phases implemented, reviewed by CI, and squash-merged to `master`,
each as an independent library-only PR:

- **P5 — operator install via SSA** (PR #86, `941c0c0`).
- **P2 — `toolbin` pinned k3d binary resolver** (PR #88, `bc2f739`).
- **P3 — `cluster` + `clusterstate` provisioning core** (PR #89, `1c12560`).

All three are merged; local `master` fast-forwarded to `1c12560`; all impl
worktrees/branches removed; the dev stack was brought down at close. Every PR was
verified green (`root:generate` idempotent, `root:check`, `root:test`) and P2/P3
additionally proven by **real live tests** (P2 downloaded+ran k3d v5.9.0; P3
provisioned a real k3d cluster end to end). The plan's remaining work is **P6
(devnet all-in-one)** and **P7 (hardening)** — see "Remaining Work" below.

Plan status: `P1✅ P4✅ P5✅ P2✅ P3✅` | remaining `P6 → P7`.

## Key Decisions
- **P5 install namespace pinned to `yacd-system`** (== chart render namespace) so
  the chart's baked-in RBAC subject namespaces match; a foreign namespace is
  rejected. Configurable-namespace deferred to P6. Manager+faucet images
  **digest-pinned to the v0.1.1 published digests** in the embedded render; the
  reconcile *version* still comes from the `app.kubernetes.io/version` chart label
  (not the digest). Prune **never deletes CRDs** (cascades to user CRs).
- **P5 Kusari findings accepted, merged as-is.** Kusari flagged the operator's
  `ClusterRole` secret/service verbs — but those are pre-existing, byte-identical to
  `charts/yacd/templates/rbac-manager.yaml` (the embed is a faithful `helm template`
  copy), and required by the controllers. `master` is unprotected so Kusari is
  advisory. Weakening RBAC was off the table. (P2/P3 Kusari passed clean.)
- **P2 pins k3d `v5.9.0`**; the embedded SHA256 (not host trust) is the integrity
  guard. GitHub release assets 302 to `release-assets.githubusercontent.com`, so the
  resolver **allow-lists GitHub download hosts and follows the redirect** rather than
  refusing all redirects (unlike `cardano-tools fetch`).
- **P3 file lock = `github.com/gofrs/flock`** (new dep) for ctx-aware
  `TryLockContext`; the lock lives in `clusterstate` and is **composed by P6, not
  acquired inside `cluster/k3d`**, keeping the two ports independently mockable.
  k3s pinned `v1.32.5-k3s1` (k3d v5.9.0's own default). Verified live: `k3d cluster
  list <name>` exits non-zero when absent → list **without** a name and filter the
  JSON; `serversRunning>=1` = control plane up.
- **Env-var-gated live tests** (`YACD_TOOLBIN_LIVE`, `YACD_CLUSTER_LIVE`) keep the
  real-network / real-Docker paths out of the default `root:test` suite while still
  giving an exercisable end-to-end proof.

## Changes
All under `cli/internal/` (library-only; no `devnet` commands, no `Options`/lifecycle
wiring — that is P6):
- **P5** `operator/` (port: `Installer`, `InstallSpec`/`State`, pure `Decide` reconcile)
  + `operator/ssa/` (embedded `manifests/operator.yaml`, CRDs-first SSA apply + wait
  Established, namespace defaulting, label prune, version read). `.dev/scripts/
  render-operator-chart.sh` + `root:generate`/`root:check` wiring. `+x/mod`,
  `+apiextensions-apiserver` direct.
- **P2** `toolbin/` (port: `Resolver`, `Pin`, `HTTPDoer`, `DefaultDir`) + `toolbin/
  ghrelease/` (`New`, `Resolve`: pre-staged/cache/fetch+verify+atomic-install+GC,
  host-allowlisted redirects; `DefaultK3dPin` = v5.9.0 + 4 digests).
- **P3** `exec/` (`Runner` seam + `OS()`), `cluster/` (port + `ManagedName`/
  `ManagedContext`/`K3sImage` consts) + `cluster/k3d/` (`EnsureCluster` state machine
  + rollback + `/healthz` prober), `clusterstate/` (port + `DefaultDir`) +
  `clusterstate/file/` (atomic JSON record + gofrs/flock lock). `+gofrs/flock`.
- `.mockery.yml` gained `operator.Installer`, `toolbin.Resolver`,
  `cluster.Provisioner`, `clusterstate.Store` → generated `cli/internal/mocks/*`.

## Remaining Work (hand-off for the next agent)

The lifecycle plan + design live at `.journal/049/LOCAL_LIFECYCLE_{PLAN,DESIGN}.md`.
Dependency graph: `P2→P3→P6`, `P1→P4→P5→P6`, `P6→P7`. **P2/P3/P5 are all merged, so
P6 is fully unblocked.**

### P6 — `devnet`: the all-in-one (the first user-facing milestone; the big slice)
Plan §"Phase 6"; Design §10.4 (`lifecycle.Manager`), §10.5 (wiring + targeting), §5–§9.
Composes everything shipped this session. In scope:
- **`cli/internal/lifecycle`** — `Manager` orchestrating the ports (Design §10.4).
  `Manager.Up` flow: acquire `clusterstate.Store.Lock` → preflight (Docker/disk) →
  `cluster.Provisioner.EnsureCluster` → `Store.Save` (record context + prior context)
  → build `operator.Installer` against the returned kubeconfig/context and
  `EnsureOperator` → unless `--bare`, build the `kube.Client`, render the embedded
  default Environment, `EnsureNamespace`+`ApplyCardanoNetwork`+wait Ready → return
  endpoints/wallet. `Down`/`Status` too. Unit-test against the 4 generated mocks
  (`mocks.Provisioner/Store/Installer/Client`) — no Docker.
- **`cli/internal/cli/devnet.go`** — thin `devnet` / `devnet down` / `devnet status`
  subtree; **embedded default local Environment** (Conway, 1 pool, Ogmios+Kupo+faucet
  +**wallet**); stepwise progress to stderr (image-pull/pod sub-progress); print the
  funded wallet address + the magic-interpolated `exec` tip-query hint.
- **`cli/internal/cli/target.go`** — ONE shared targeting resolver (Design §6
  precedence: explicit `--kubeconfig`/`--context` or `YACD_*` > yacd's tracked
  managed context `k3d-yacd` when the managed cluster exists > ambient
  KUBECONFIG/current-context). Wire it into ALL existing verbs (up/info/run/exec/
  topup/connect/list/down) feeding `kube.NewClient(kubeconfig, context)`.
- **`cli/internal/cli/options.go`** — add the factory fields the prior phases
  deliberately did NOT wire: `ClusterProvisionerFactory`, `OperatorInstallerFactory`
  (`func(kubeconfig, context string) (operator.Installer, error)` → `ssa.New`),
  `ClusterStateFactory`, defaulting to the subpackage adapters
  (`k3d.New`/`ghrelease.New`/`ssa.New`/`file.New`), overridable in tests.
- Context switch + tracking + restore on teardown; `Manager.Up` idempotent/reentrant.
- **GUARDRAIL (Design §6 / Plan §6):** the managed-context tier must only engage when
  a managed cluster exists, so existing automation (explicit `KUBECONFIG`/`--context`,
  i.e. CI + Chainsaw) is unaffected — confirm CI/Chainsaw stay green with no test edits.
- Reuse the existing `up` apply/wait path; don't re-implement CR rendering/readiness.
  The composition root passes `ssa.Manifests`, `ghrelease.DefaultK3dPin`+
  `toolbin.DefaultDir`, `cluster.DefaultSpec`, `clusterstate.DefaultDir`.
- New e2e: a gated k3d-based end-to-end (provision→install→network→fund→use→down);
  the existing KinD Chainsaw e2e stays for operator testing.

### P7 — Hardening & UX (Plan §"Phase 7", after P6)
Typed failure taxonomy → actionable messages (Docker down, port conflict, disk
pressure, checksum/version mismatch); Docker/VM-disk preflight; `devnet down --purge`
(cluster + state + fetched binaries); first-run banner; WSL2 validation; an ARM
multi-arch CI guard for the operator/Cardano images.

### Other carried threads
- **Operator releases:** two GitHub *draft* releases (`v0.1.0`, `v0.1.1`) from
  session 053 remain for a human to Publish (GHCR artifacts already public). Each
  `feat(cli)` merged this session queued a pre-1.0 **PATCH** bump on the open root
  release PR (release-please; `bump-patch-for-minor-pre-major: true`).
- Pre-existing: deterministic primary-sidecar manager-envtest refactor; TEST_REPORT
  F2/F4; test-harness `yacd-env` Action + examples/how-to; the wallet double-fund
  residual risk (session 053). Docs are a separate session.
- Embedded-manifest maintenance: on an operator release bump, update the digests in
  `.dev/scripts/render-operator-chart.sh` + re-render; on a k3d bump, update
  `ghrelease.DefaultK3dPin` + the k3s `cluster.K3sImage`.

## References
- PRs: #86 (P5 operator SSA, `941c0c0`), #88 (P2 toolbin, `bc2f739`), #89 (P3 cluster,
  `1c12560`).
- Plan/design: `.journal/049/LOCAL_LIFECYCLE_PLAN.md`, `LOCAL_LIFECYCLE_DESIGN.md`.
  Prior: `.journal/053/SUMMARY.md` (P1+P4 releases), `.journal/049/SUMMARY.md` (design).
- Published refs to pin against (from session 053): manager
  `ghcr.io/meigma/yacd:v0.1.1@sha256:5d53ca…f0f8e21`, faucet
  `…/faucet:v0.1.1@sha256:826f8d…fd3f66`, chart `…/chart:0.1.1@sha256:a8d24d…8a5a049f`.

## Lessons
- **`wt switch --create --base ^` branches from the LOCAL default branch**, which can
  be stale. P3's worktree came off pre-P2 master and couldn't import `toolbin`; fixed
  with `git merge --ff-only origin/master` (untracked new files survive). When a phase
  depends on a just-merged sibling, fetch + ff the base (or branch from `origin/master`)
  before coding.
- **A scanner that reads rendered manifests will flag the operator's real RBAC** the
  first time it appears as concrete YAML (Helm templates are skipped as invalid
  standalone YAML). Faithful-copy embeds aren't regressions; don't weaken required RBAC
  to satisfy the scanner — accept/annotate when the check is advisory.
- **GitHub release-asset downloads redirect to a CDN host**, so "refuse all redirects"
  breaks them; allow-list the GitHub download hosts and rely on the embedded digest.
- **CLI-only changes don't affect the `e2e` job** (it builds the manager image + runs
  Chainsaw), so merging on green `ci`+`Kusari` with `e2e` still pending was safe each time.
