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

## 2026-06-03 16:10 — PR1 merged; PR2 (`yacd install`) shipped (awaiting review)
- **PR #94 (PR1) MERGED** to master (`ded61fa`, squash, type `refactor` so no
  release bump). Post-merge master CI is **green** (`CI: completed success`,
  incl e2e). Worktree + branches cleaned up via `wt remove -D`.
- **PR2 = `yacd install` (PR #96, branch `feat/yacd-install`, OPEN / not merged).**
  Built via workflow `wf_7089e486-fbd` (implement→test→4 reviewers→fix). Top-level
  command: explicit-or-ambient targeting (NOT the managed-devnet record, NOT
  withManagedReconcile, the opposite of devnet); `-n` namespace, `--wait`/
  `--timeout` (bounds the whole op even with --wait=false), `--dry-run`; refusals
  → actionable guidance + nonzero exit.
- **Operator-port growth (closes the PR1-deferred gap):** `OperatorState(ctx,
  namespace)`; new `Plan(ctx,spec)→Decision` (renders+reads+Decide, NO apply) for
  dry-run; shared `operator.WaitForReady` extracted from lifecycle (devnet rewired,
  behavior identical); `operator.DefaultNamespace` single source. New files
  `operator/ready.go`, `cli/install.go`.
- **Validation:** independently re-verified on-branch (build/vet clean, deny-list
  empty, forced `-count=1` operator/ssa+cli+lifecycle envtest PASS, root:check +
  root:test PASS). **LIVE on a throwaway k3d cluster:** `yacd install --wait` →
  manager Deployment Available v0.1.1, both CRDs Established; `--dry-run` → no-op
  re-apply; idempotent re-install. 14 command tests + 2 SSA Plan "no-apply"
  envtests. Review = low/nit + 1 medium (non-default ns not proven in wait path);
  all genuine fixes applied, 2 non-CLI-reachable lows deferred. PR #96 CI running.
- **Next:** user review of PR #96. Then PR3 (optional): uninstall (`Remove` port +
  CRD-deletion policy) + runtime version selection (OCI chart fetch). The release-
  please root PR #87 will now carry PR2's `feat` (CLI release).

## 2026-06-03 17:05 — PR2 follow-up: Helm value overrides on `yacd install` (model A)
User asked why install took no value-customization flags. It didn't (deferred in
the proposal); the plumbing existed (`operator.Values.Extra`). Added via workflow
`wf_697dc647-a4e`. On `feat/yacd-install` (PR #96), commit `42cc75c`:
- `--values/-f`, `--set`, `--set-string` (Helm `strvals`) → assembled by
  `buildUserOverrides` (new `cli/internal/cli/install_values.go`) into one map →
  `operator.Default().Extra`. Precedence: `-f` (in order) < `--set` < `--set-string`
  (fixed; intentionally NOT Helm's arg-order interleave — documented).
- Schema validation added in `ssa/render.go` (`CoalesceValues` + `ValidateAgainstSchema`,
  with `ToRenderValuesWithSchemaValidation(...skip=true)` to avoid double-validate);
  runs for EnsureOperator AND Plan, so `--dry-run` validates too.
- **Model A (user-chosen):** image stays digest-pinned; user values deep-merge over
  `Default()`. Review HIGH catch: the deep-merge means `--set image.tag` is inert
  (digest-over-tag) but `--set image.digest/repository` DO repoint the image — the
  help text was corrected to say this precisely (NOT enforced in code, per model A).
  **Surfaced to the user as a known nuance** — option to hard-strip `image.*` later
  if they want true non-overridability.
- `strvals` adds no new module (helm v3.21.0 already a dep); deny-list still clean.
- Verified: build/vet clean, forced `-count=1` cli+ssa+operator PASS (ssa 37s),
  root:check + root:test PASS. **LIVE on k3d:** `--set replicaCount=2 --wait` →
  2/2 ready, image still digest-pinned; `--set manager.logLevel=bogus --dry-run` →
  fail-fast schema error (exit 1); `-f file --dry-run` → plan ok. 9 new tests
  (7 command + 2 render/schema). Review = 1 high (help-text accuracy, fixed) + meds
  (double-validate, precedence coverage — fixed) + nits; 3 deferred with reasons.
- PR #96 body updated; CI re-running on `42cc75c`. Still OPEN / not merged.

## 2026-06-03 18:40 — Functional test (k3d) + PR #96 MERGED
- **Manual functional pass** of `yacd install` on two fresh isolated k3d clusters
  (NOT the Tilt dev stack — Tilt Helm-installs the operator and would contend; bare
  cluster is the faithful target for an install-onto-arbitrary-cluster command).
  32 checks, ALL behaviors correct: dry-run (empty→install / installed→re-apply),
  install --wait + verify (Available, version label, digest-pinned image, 2 CRDs),
  idempotent re-install, `--set replicaCount=2`→2 replicas (image still pinned),
  `-f` file + `-f`<`--set` precedence (log-level), `--set-string` type-reject +
  `--set …logLevel=bogus` enum-reject (schema fail-fast, exit 1), `--wait=false`,
  explicit `--kubeconfig/--context` honored, upgrade (seed v0.0.1→upgrade), refuse
  major-mismatch (v9.9.9) + newer-same-major (v0.9.9→"upgrade the CLI") both exit 1
  with NO mutation, `-n yacd-test` install (objects+RBAC subjects in ns, nothing in
  yacd-system). Clusters torn down. Only finding: a cosmetic doubled refuse-advice
  line (Decide error already states the advice, refuseGuidance restates it) —
  offered to tighten; user said LGTM/merge, so left as-is.
- **PR #96 MERGED** to master (`5383f76`, squash, `feat(cli)` → release-please root
  PR #87 now carries the CLI feature). All 4 checks green incl e2e. Worktree +
  branches removed via `wt remove -D`.
- **`yacd install` is DONE.** Operator-install epic status: PR1 (#94, render-based
  install + in-place embed) ✅; PR2 (#96, `yacd install` + value overrides) ✅.
  Remaining = **PR3 (optional)**: `yacd uninstall` (`Remove` port + explicit
  CRD-deletion policy) + runtime version selection (OCI chart fetch). Proposal
  `.journal/058/OPERATOR_INSTALL_PROPOSAL.md` §7/§8 has the open shaping decisions.
- Dev stack never started this session (all work was CLI-package-only); nothing to
  tear down.

## 2026-06-03 18:55 — Close
Session closed. Goal MET: `yacd install` shipped across two squash-merged PRs.
- **#94** (`ded61fa`, `refactor`) — render the embedded chart in-memory; chart embedded
  in place via `charts/embed.go` (no copy / no drift guard).
- **#96** (`5383f76`, `feat`) — the `yacd install` command + `-f`/`--set`/`--set-string`
  value overrides; port grown (`OperatorState(ns)`, `Plan`, `WaitForReady`).
Both merged to `master`; local `master` fast-forwarded to `5383f76`; both impl worktrees
removed (`wt remove -D`). All CI green incl e2e. SUMMARY.md written; INDEX row → complete;
TECH_NOTES updated (new in-place-embed + `yacd install` bullets, superseding the
session-054 build-time-render note). No dev stack to tear down.
Hand-off: optional **PR3** (uninstall + OCI version fetch) is the only remaining item —
shaping in `OPERATOR_INSTALL_PROPOSAL.md` §7/§8. Minor open: cosmetic doubled refuse-advice
wording (left per user); model-A image-override leak (hard-strip `image.*` if true
non-overridability ever wanted).
