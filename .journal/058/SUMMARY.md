---
id: 058
title: yacd install command (render-based operator install + value overrides)
date: 2026-06-03
status: complete
repos_touched: [yacd]
related_sessions: [049, 053, 054, 055, 056, 057]
---

## Goal
Add a `yacd install` subcommand that installs/upgrades the YACD operator onto an
arbitrary Kubernetes cluster. Research the existing install mechanism first, then
implement.

## Outcome
**Met.** Shipped as two squash-merged PRs (the work was split for reviewability):

- **PR #94 (`refactor`, `ded61fa`) — render-based operator install.** Replaced the
  build-time pre-rendered, digest-pinned, yacd-system-baked embedded manifest with
  the chart **rendered in-memory at install time** via the lean Helm subset, driven
  by a typed `operator.Values`/`Default()` contract. devnet behavior unchanged.
- **PR #96 (`feat`, `5383f76`) — the `yacd install` command** + Helm value
  overrides. Top-level verb; explicit-or-ambient targeting; `-n`, `--wait/--timeout`,
  `--dry-run`, and `-f`/`--set`/`--set-string`. Grew the operator port with
  `OperatorState(ctx, namespace)`, `Plan` (dry-run), and a shared `WaitForReady`.

Every PR was independently gate-verified (forced `-count=1` envtest, `root:check` +
`root:test`, dep deny-list) **and live-validated on throwaway k3d clusters**. A final
manual functional pass exercised the full flag matrix across 32 checks — all correct.
Remaining work is the optional PR3 (uninstall + OCI version fetch); it was scoped out
deliberately, not dropped.

## Key Decisions
- **In-memory Helm render of the embedded chart** (lean `loader`/`chartutil`/`engine`
  + `CRDObjects`) instead of a static pre-render → unlocks runtime values + a real
  namespace input. Empirically: identical 12-object set to `helm template
  --include-crds`; ~52 net-new pure-Go pkgs, **zero OCI/docker** (vs ~270 via
  `pkg/action`). Reuse the existing SSA apply pipeline unchanged.
- **Embed the chart IN PLACE** via a 5-line `charts/embed.go` (an *ancestor* dir of
  `charts/yacd`, so `//go:embed all:yacd` works) rather than a copied in-package dir
  + sync script + drift guard. Single source of truth, hermetic `go build`, CRDs flow
  `controller-gen → charts/yacd/crds → embed` with zero sync. Net `+56/−3178`.
  Rejected a build-hook-generated copy: `//go:embed` resolves at `go build` time and
  the IDE/`go test`/CI all invoke the toolchain directly, so a moon-only hook makes a
  plain build non-hermetic.
- **`yacd install` targets explicit-or-ambient, never the managed-devnet record**
  (not wrapped in `withManagedReconcile`, does not call `rejectExplicitTarget`) — the
  opposite of devnet, which owns its own cluster. Installing into a running devnet is
  never the silent default.
- **Port growth:** `OperatorState(ctx, namespace)` (closes the PR1-deferred gap so
  `install -n foo --wait` waits in foo), `Plan(ctx, spec) → Decision` (renders + reads
  + `Decide`, no apply) for `--dry-run`, and a shared `operator.WaitForReady` extracted
  from lifecycle so devnet and install share one poll. `operator.DefaultNamespace` is
  the single source.
- **Value overrides = Model A** (user-chosen): `-f`/`--set`/`--set-string` merge into
  `Values.Extra` and deep-merge over `Default()`; schema-validated against
  `values.schema.json`. The operator image stays **digest-pinned** (upgrade the CLI to
  change versions). Known nuance surfaced in review: the deep-merge makes `--set
  image.tag` inert (digest-over-tag) but `--set image.digest`/`image.repository` *do*
  repoint the image — documented in `--help` rather than code-enforced (enforcing
  would violate the "simple deep-merge" model the user picked).

## Changes
- `charts/embed.go` (new) — in-place chart embed (`charts.OperatorChart fs.FS`).
- `cli/internal/operator/{operator.go,reconcile.go,values.go,ready.go}` — port
  (`InstallSpec{Namespace,Values}`, `OperatorState(ns)`, `Plan`, `Decision`,
  `DefaultNamespace`, `WaitForReady`), typed `Values`/`Default()`. `Decide` unchanged.
- `cli/internal/operator/ssa/{ssa.go,apply.go,render.go,doc.go}` — render the embedded
  chart, namespace threaded through render + apply, schema validation in `render`.
  Removed the old `embed.go` + the copied `chart/` dir.
- `cli/internal/cli/{install.go,install_values.go,root.go}` — the command, value
  assembly (helm `strvals`), registration (unwrapped).
- `cli/internal/lifecycle/manager.go` — rewired to `WaitForReady` (behavior identical).
- `moon.yml`, `.dev/scripts/check.sh` — dropped the render script + chart-copy drift
  guard; gofmt now covers `charts/`. Deleted `.dev/scripts/render-operator-chart.sh`
  and `sync-operator-chart.sh`.
- Tests: command tests via `mocks.Installer`, render/schema tests, namespace + Plan
  "no-apply" envtests. Added dep `helm.sh/helm/v3/pkg/strvals` (no new module).

## Open Threads
- **PR3 (optional):** `yacd uninstall` — needs a new `Installer.Remove` port method +
  a deliberate CRD-deletion policy (CRDs cascade to user CRs; today they are never
  pruned). And runtime version selection — an OCI chart-source fetch from
  `ghcr.io/meigma/yacd/chart` for `--version`/latest (keep the embedded chart as the
  offline default; isolate registry deps to that opt-in path). Shaping decisions in
  `OPERATOR_INSTALL_PROPOSAL.md` §7/§8.
- **Cosmetic:** the refuse messages double-state advice (Decide's error text already
  ends with short advice; `refuseGuidance` restates it). Left as-is per user LGTM;
  a one-line tidy if desired.
- **Model-A image leak:** if true image non-overridability is ever wanted, hard-strip
  `image.*`/`faucet.image.*` from user overrides before the merge.
- Session-overlap note: a parallel conversation briefly used 058's NOTES for an
  unrelated "managed test wallets" design; it was relocated to **session 059**.

## References
- PRs: #94 (render-based install + in-place embed), #96 (`yacd install` + value
  overrides). Both merged to `master` (`5383f76`).
- Design doc: `.journal/058/OPERATOR_INSTALL_PROPOSAL.md`.
- Workflows: research `wf_6a7b721e-e8c`; PR1 `wf_50d0f5ff-473`; PR2 `wf_7089e486-fbd`;
  value-flags `wf_697dc647-a4e`.
- Prior: `.journal/054/SUMMARY.md` (P5 operator SSA install), `.journal/053/SUMMARY.md`
  (operator v0.1.1 release), `.journal/049/` (devnet lifecycle design).

## Lessons
- The IDE/gopls "module not in workspace" + a flood of "undefined"/"BrokenImport"
  diagnostics during a workflow are usually a stale mid-`go mod tidy` snapshot, not a
  real break — `go build` + `go mod verify` are the ground truth. Saw this twice; both
  times the on-disk state was clean.
- `//go:embed`'s only real constraint is no `..` traversal — embedding from an
  *ancestor* package sidesteps the "must copy the asset into the package" pattern
  entirely. Worth remembering for the existing `devnet.yaml`/`init.yaml` byte-copies.
