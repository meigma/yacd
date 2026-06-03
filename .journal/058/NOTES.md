---
id: 058
title: New session
started: 2026-06-03
---

## 2026-06-03 10:16 — Kickoff (superseded)
Session started via `session-new` but never received a request; world-state
below was stale before any work began. Re-initialized in the entry that follows.

## 2026-06-03 11:43 — Kickoff (re-initialized)
Goal for the session: not yet stated — session (re)started via `session-new`;
awaiting the user's request.

Current state of the world:
- `master` at `b611645` (PR #93, session 057: all-namespaces list, self-
  forwarding topup, `yacd init`), clean.
- Local-lifecycle plan core is **complete**: `P1✅ P4✅ P5✅ P2✅ P3✅ P6✅`;
  only **P7 (hardening & UX)** remains (typed failure taxonomy, Docker/disk
  preflight, `devnet down --purge`, `--isolate-kubeconfig`, WSL2/ARM guards,
  first-run banner, devnet image preload/preflight).
- `yacd devnet` all-in-one local k3d lifecycle shipped + manually functional-
  tested (sessions 055/056); wallet funded 100k ADA on-chain verified.
- Operator releases live: `v0.1.1` (manager/faucet/chart), embedded in the CLI
  SSA install. release-please root PR #87 open (`release 0.1.2`); GitHub draft
  releases still await a human Publish (GHCR artifacts already public).
- Open PRs: #91 (`docs/mkdocs-site`, docs site — owes the session-057 doc fixes:
  `list -A`, `topup` form), #87 (release-please 0.1.2), #44/#43 (dependabot).
- Other carried threads: deterministic primary-sidecar manager-envtest refactor;
  TEST_REPORT F2/F4; test-harness `yacd-env` Action + examples/how-to.

Plan: await the user's actual request before doing substantive work. Dev-stack
startup (`moon run root:dev-up`) is deferred until an implementation worktree is
selected and the task is known.

## 2026-06-03 14:58 — `yacd install`: research + proposal + PR1 shipped (awaiting review)
Goal: add a `yacd install` subcommand to install the operator onto an arbitrary
target cluster. Note: an earlier draft of this NOTES briefly held a parallel
"managed test wallets" design (different concurrent 058 conversation); that work
was **moved to session 059** (`5d009e3`) to resolve the session-ID overlap. 058 is
now exclusively the operator-install effort.

Research (workflow `wf_6a7b721e-e8c`, 8 agents): the operator install today is a
k3d-decoupled port (`cli/internal/operator` + `ssa` adapter) that applies a
**build-time pre-rendered, digest-pinned, yacd-system-baked embedded manifest**
via SSA (CRD-first → wait-Established → kind-ordered → `yacd-cli` field owner →
label-prune with CRDs excluded). Everything chart-configurable is frozen at render
time; `InstallSpec.Namespace` was only a reject-gate. Full findings + critic in the
workflow output.

Decided direction (user): two components. **PR1** = upgrade the install package to
render an **embedded copy of `charts/yacd` in-memory** via the lean Helm subset
(`loader`/`chartutil`/`engine` + `CRDObjects`) with a typed `Values`/`Default()`
contract; **PR2** = the `yacd install` command (explicit `--kubeconfig`/`--context`,
real `-n`, `--wait`/`--dry-run`); **PR3 (optional)** = OCI version-fetch + uninstall.
Empirically validated before building: lean render = identical 12-object set as
`helm template --include-crds`; dep cost = ~52 net-new pure-Go pkgs, **zero
OCI/docker** (vs ~270 via `pkg/action`). Proposal: `.journal/058/OPERATOR_INSTALL_PROPOSAL.md`.

**PR1 implemented** (workflow `wf_50d0f5ff-473`: implement → test → 4 adversarial
reviewers → fix). Branch `refactor/operator-render-install` (worktree
`.wt/refactor-operator-render-install`), **PR #94 open — NOT merged, awaiting review**.
- Embedded chart copy `cli/internal/operator/ssa/chart/` synced by
  `.dev/scripts/sync-operator-chart.sh`; render script + pre-rendered `operator.yaml`
  removed; drift guard (`check.sh`/`moon.yml`) now diffs the chart copy.
- `ssa/render.go` (validated recipe), `operator/values.go` (typed `Values` +
  `Default()` with the two v0.1.1 digests as Go consts), `InstallSpec` = `{Namespace,
  Values}` (dropped `Version`). Namespace threaded through render + apply so RBAC
  subjects and namespaced objects agree (envtest proves a non-yacd-system install).
- `devnet` rewire = one line (`InstallSpec{Values: operator.Default()}`); behavior
  identical. helm v3.21.0, render-only subpackages; no k8s/controller-runtime downgrade.
- Gates independently re-verified on-branch: `go build`/`go vet` clean, `go mod verify`
  ok, dep deny-list empty, forced `-count=1` operator/ssa envtest PASS (30s),
  `moon run root:check` + `root:test` PASS. Review raised only low/nit (3 applied,
  3 deferred: OperatorState namespace-arg → PR2, inert `.helmignore` bypass, kyverno
  render coverage).

Deliberately **did NOT start `moon run root:dev-up`**: PR1 is CLI-package-only with
byte-identical chart content/render output — the Kind/Tilt operator stack wouldn't
exercise the new CLI path, and it's a shared singleton. Surfaced to the user.

**Next:** user review of PR #94. After merge → PR2 (`yacd install` command). Open
shaping decisions for PR2 recorded in the proposal §7 (no-flag targeting, command
shape vs `operator` noun group, version-source fork for PR3).

## 2026-06-03 15:30 — PR1 follow-up: in-place chart embed (killed the copy)
User pushback on PR1's chart **duplication** (`cli/internal/operator/ssa/chart/`
copy + sync script + drift guard = tech debt keeping two copies in sync). Asked
if a build hook could avoid it. Key insight: **no build hook can** — `//go:embed`
resolves at `go build` time and the IDE/`go test`/CI all call the toolchain
directly, so a moon-only generate-the-copy step makes plain `go build` embed stale
bytes / fail (non-hermetic). But the copy was never needed: `//go:embed` only bans
`..`, and a Go file in `charts/` (an **ancestor** of `charts/yacd`) can embed the
chart **in place**. Validated empirically (prototype: 10 templates + 2 CRDs,
`_helpers.tpl` via `all:`, helm loader loads it), then implemented.
- Added `charts/embed.go` (package `charts`, `//go:embed all:yacd` →
  `charts.OperatorChart fs.FS` via `fs.Sub`). `ssa.New` takes the chart `fs.FS`;
  root.go passes `charts.OperatorChart`. render.go walks the chart-rooted FS (`.`).
- Removed: the whole `ssa/chart` copy (16 files), `ssa/embed.go`,
  `sync-operator-chart.sh`, the moon `sync-operator-chart` task +
  `embeddedChartSources` group + generate sync step, and the check.sh chart-copy
  drift guard. gofmt now covers `charts/`; goSources tracks `charts/**/*.go`.
- **Net +56 / −3178.** Single source of truth; CRDs flow controller-gen →
  `charts/yacd/crds` → embed with zero sync. Build stays hermetic.
- Gates re-verified on-branch: `go build`/`go vet` clean, forced `-count=1`
  operator/ssa envtest PASS (30s), `moon run root:check` + `root:test` PASS.
  Second commit `629b518` on `refactor/operator-render-install`, pushed; **PR #94
  updated (body + design doc §5.1), still open / not merged**. CI running
  (ci/e2e/cardano-tools-image pending). Awaiting user review.
