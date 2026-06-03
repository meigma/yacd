# Proposal: `yacd install` + render-based operator install

Status: **DRAFT — awaiting review** (session 058, 2026-06-03)
Author: agent (with jmgilman)

## 1. Summary

Add a `yacd install` subcommand that installs the YACD operator onto an
**arbitrary** target Kubernetes cluster. Delivered as two components plus an
optional third:

- **PR1 — operator-install package upgrade.** Replace the build-time
  pre-rendered, digest-pinned, `yacd-system`-baked embedded manifest with an
  **embedded chart rendered in-memory** through the lean Helm render subset,
  driven by a typed **Helm values contract** + an `operator.Default()`
  constructor. This makes namespace, images, resources, and manager flags
  configurable at install time and dissolves the `yacd-system` namespace pin
  (it becomes a render input). The existing SSA apply pipeline is reused
  unchanged. `devnet` rewires to call `operator.Default()` — minor surgery.

- **PR2 — the `yacd install` command.** A thin top-level verb that targets an
  arbitrary cluster (accepts explicit `--kubeconfig`/`--context` — the opposite
  of `devnet`, which rejects them), renders + applies via the upgraded package,
  and exposes `--namespace`, `--wait`/`--timeout`, `--dry-run`, and values
  overrides.

- **PR3 (optional) — runtime version selection + `uninstall`.** A pluggable
  chart *source* (embedded default vs. OCI-pulled from GHCR) for
  `--version`/latest, and a symmetric uninstall. Deferred so PR1/PR2 stay at the
  leanest dependency footprint and remain fully offline. See §7 — this is the
  one place the design diverges from the original "default to fetching latest
  from GHCR" framing, and it is the key decision to confirm.

The cut is deliberate: the **apply engine** (CRD-first → wait-Established →
kind-ordered workloads → `yacd-cli` field owner → label-prune, CRDs never
pruned) is the genuinely reusable part and does not change. We only swap the
**manifest source** from "static `//go:embed`'d pre-render" to "Helm render of
an embedded chart with runtime values."

## 2. Background — how install works today (verified)

`devnet up` runs one idempotent operator-install step that is already fully
decoupled from k3d behind a port:

```
devnet up
  └─ lifecycle.Manager.Up                              (lifecycle/manager.go:38)
       └─ ensureOperatorReady                          (manager.go:136-159)
            ├─ installer = NewInstaller(kubeconfigPath, context)  ← only two strings
            ├─ installer.EnsureOperator(ctx, operator.InstallSpec{})  ← ZERO spec
            └─ poll installer.OperatorState until Ready (3s; bounded by --timeout)
```

- **Port** (`cli/internal/operator/operator.go:47-60`): `EnsureOperator(ctx,
  InstallSpec) (State, error)` and `OperatorState(ctx) (State, error)`.
  `InstallSpec{Namespace, Version, Values map[string]string}`. The adapter
  **never reads `Version` or `Values`** today (both reserved).
- **Decide** (`operator/reconcile.go:45-78`): pure semver policy →
  `Install`/`Upgrade`/`Noop`/`Refuse`, typed errors `ErrNewerOperator`,
  `ErrMajorMismatch` (checked *before* the compare), `ErrUnknownInstalledVersion`.
- **SSA adapter** (`operator/ssa`): `ssa.New(kubeconfig, kubeContext,
  ssa.Manifests)` needs only a kubeconfig path + context string + an `fs.FS`.
  No Helm SDK, no network pull, no k3d/clusterstate dependency.
- **Embedded manifest**: `.dev/scripts/render-operator-chart.sh` runs one
  `helm template yacd charts/yacd --namespace yacd-system --include-crds
  --no-hooks --set-string image.digest=… --set-string faucet.image.digest=…`
  into `operator/ssa/manifests/operator.yaml` (2227 lines), `//go:embed`'d, with
  image digests hand-pinned to v0.1.1 and a `root:generate`/`root:check` drift
  guard.
- **Apply pipeline** (`ssa/ssa.go`, `ssa/apply.go`): Namespace → CRDs → wait
  Established (own fixed 60s/1s budget, independent of `--timeout`) →
  kind-ordered workloads (SA<ClusterRole<Role<CRB<RB<Service<Deployment) →
  label-prune over a fixed GVK set (**CRDs excluded** — they cascade to user
  CRs). Field owner `yacd-cli`, `ForceOwnership`, prune label
  `yacd.meigma.io/install=operator`.

**What is frozen today** (the limitation `install` must overcome): because the
embedded manifest is a static pre-render, *everything* chart-configurable —
images, namespace, replicas, resources, manager flags, RBAC subjects — is baked
at build time. The `yacd-system` pin exists specifically because the render used
`--namespace yacd-system`, which baked that namespace into the RoleBinding /
ClusterRoleBinding **subjects**. `InstallSpec.Namespace` is honored only as a
*reject gate* (`ssa.go:85`), not as a real override.

## 3. Why change

`yacd install` targets clusters the operator team does not own, so it needs to
vary at least: **target cluster** (kubeconfig/context — already supported by
`ResolveTarget` tier-1), **namespace** (today a baked-in lie), and ideally
images / resources / manager flags. None of that is possible against a static
pre-render. Rendering the chart in-memory with runtime values is the smallest
mechanism that unlocks all of it, and it has a pleasant side effect:
`.Release.Namespace` becomes a render input, so **namespace becomes genuinely
configurable** and the pin dissolves into a sensible default.

## 4. Validated technical foundation

Both load-bearing risks were measured empirically before writing this proposal.

### 4.1 The lean render subset reproduces `helm template` exactly

Rendering `charts/yacd` via `loader.LoadDir` → `chartutil.ToRenderValues` →
`engine.Render` (clientless) plus `chart.CRDObjects()` produces an **identical
object set** to `helm template yacd charts/yacd --namespace yacd-system
--include-crds --no-hooks --set-string image.digest=… --set-string
faucet.image.digest=…`:

```
LIB (helm.sh/helm/v3 SDK) objects: 12
CLI (helm v4.0.4) objects:         12
only-in-LIB: []   only-in-CLI: []   → MATCH ✅
  ClusterRole/yacd-manager-role, ClusterRole/yacd-metrics-auth-role,
  ClusterRole/yacd-metrics-reader, ClusterRoleBinding/yacd-manager-rolebinding,
  ClusterRoleBinding/yacd-metrics-auth-rolebinding,
  CustomResourceDefinition/cardanodbsyncs.yacd.meigma.io,
  CustomResourceDefinition/cardanonetworks.yacd.meigma.io,
  Deployment/yacd-controller-manager, Role/yacd-leader-election-role,
  RoleBinding/yacd-leader-election-rolebinding,
  Service/yacd-controller-manager-metrics-service,
  ServiceAccount/yacd-controller-manager
```

Note the v3 SDK matches the v4 CLI for this chart (it uses no v4-only template
features — no `lookup`/`.Capabilities`/`.Files`, verified). We can adopt the
lean v3 subset; revisit only if the chart ever adopts a v4-only feature.

### 4.2 Dependency footprint: 52 net-new pure-Go packages, zero OCI/docker

| Approach | Compiled non-stdlib pkgs | Net-new vs. today's `yacd` binary | Heavy stack? |
|---|---|---|---|
| `loader` + `chartutil` + `engine` (render-only) | 298 | **52** | **none** |
| `helm.sh/helm/v3/pkg/action` (the convenient API) | 701 | ~270 | full oras-go + docker + containerd + helm `registry`/`downloader`/`getter`/`repo`/`kube` |

Embedding the chart is *what makes* the lean path possible: `pkg/action` /
`pkg/registry` / `pkg/downloader` exist to **fetch** charts and **apply** them
via Helm's own kube client — we do neither. The 52 net-new packages are all
small pure-Go libs and mostly unavoidable for faithful rendering: Sprig + its
closure (`Masterminds/sprig`, `goutils`, `semver`, `mitchellh/copystructure`+
`reflectwalk`, `huandu/xstrings`, `shopspring/decimal`, `golang.org/x/crypto`),
a JSON-schema validator (`santhosh-tekuri/jsonschema/v6` — gives us
`values.schema.json` validation for free), plus `dario.cat/mergo`,
`gobwas/glob`, `BurntSushi/toml`, `cyphar/filepath-securejoin`, and 7 thin
`helm.sh/helm/v3` subpackages.

**Import allow-list / deny-list (enforce in review or a tiny check):**
```go
// ALLOW: helm.sh/helm/v3/pkg/{chart, chart/loader, chartutil, engine}
// DENY:  helm.sh/helm/v3/pkg/{action, registry, downloader, getter, repo, kube, cli}
```

### 4.3 Correctness gotchas (pin these for the implementer)

1. **CRDs come from `chart.CRDObjects()`, not `engine.Render`.** Helm renders
   `crds/` separately. Concat them and feed the existing `partitionCRDs` →
   CRD-first → wait-Established path. Reproduces `--include-crds`.
2. **`//go:embed all:chart`** — the `all:` prefix is mandatory, or `embed`
   silently drops `_helpers.tpl` (and any `_`/`.`-prefixed file) and the render
   explodes on missing named templates.
3. **Clientless `engine.Render` ⇒ no `lookup`, no `rest.Config` needed for
   rendering.** Safe because the chart has no `lookup`. Cluster access is only
   for the *apply*, which we already own.
4. **`--no-hooks`** — filter `helm.sh/hook`-annotated objects defensively
   (chart has none today).

## 5. Component A (PR1) — operator-install package design

### 5.1 Chart embedding — in place, no copy

> **Implemented decision (revised after review).** The first cut copied
> `charts/yacd` into `cli/internal/operator/ssa/chart/` with a `root:generate`
> sync + `root:check` drift guard. That was replaced — see below — with an
> **in-place embed** that has no copy and no drift guard. The paragraph below
> reflects what shipped.

`//go:embed` cannot traverse `..`, but it *can* embed a subdirectory of the
embedding Go file's directory. `charts/yacd` lives under `charts/`, so a tiny Go
file at **`charts/embed.go`** (package `charts`, an *ancestor* of the chart)
embeds `charts/yacd` in place via `//go:embed all:yacd`, exposing
`charts.OperatorChart fs.FS` (rooted at the chart through `fs.Sub`). The `ssa`
adapter takes the chart `fs.FS` as a constructor argument; the composition root
passes `charts.OperatorChart`.

This means **one source of truth and nothing to sync**: `controller-gen` writes
CRDs to `charts/yacd/crds` and they are embedded directly, and the build stays
hermetic (`go build` alone works — no codegen/hook prerequisite, which is why a
build-time-only generated copy was rejected: `//go:embed` resolves at `go build`
time and the IDE/`go test`/CI all invoke the toolchain directly).

**Removed:** `.dev/scripts/render-operator-chart.sh`, the pre-rendered
`operator.yaml`, the in-package chart copy, `.dev/scripts/sync-operator-chart.sh`,
the `sync-operator-chart` Moon task + `embeddedChartSources` group, and the
chart-copy drift guard. Net for the in-place swap alone: **+56 / −3178**.

### 5.2 Render path (the lean subset)

```go
//go:embed all:chart
var chartFS embed.FS   // in-package copy of charts/yacd, drift-guarded

func render(spec InstallSpec) (objects []*unstructured.Unstructured, err error) {
    ch, err := loader.LoadFiles(bufferedFilesFrom(chartFS, "chart")) // walk embed.FS → []*loader.BufferedFile
    if err != nil { return nil, err }
    rv, err := chartutil.ToRenderValues(ch, spec.Values.toHelmValues(),
        chartutil.ReleaseOptions{Name: "yacd", Namespace: spec.Namespace}, nil) // coalesce + schema-validate
    if err != nil { return nil, err }
    templates, err := engine.Render(ch, rv) // map[path]yaml; clientless
    if err != nil { return nil, err }
    return parseObjects(templates, ch.CRDObjects()) // CRDs added here; filter hooks; sort by existing kind rank
}
```

The output `[]*unstructured.Unstructured` flows into the **unchanged** apply
pipeline (`partitionCRDs`, `waitEstablished`, kind-ordered apply, prune).

### 5.3 Go contract changes

Replace the flat, unused `Values map[string]string` with a typed values
contract + an escape hatch, and a `Default()` constructor:

```go
// operator/values.go
type Values struct {
    Image          Image  // Repository, Tag, Digest
    FaucetImage    Image
    Replicas       *int
    LogFormat      string // json|text
    LogLevel       string // debug|info|warn|error
    LeaderElection *bool
    // …the common knobs mirrored from values.yaml…
    Extra map[string]any // merged last; full-chart escape hatch
}

func (v Values) toHelmValues() map[string]any { /* struct → helm values tree, Extra merged on top */ }

// Default returns the pinned, offline baseline that reproduces today's install:
// digest-pinned manager + faucet images (Go consts, bumped on operator release),
// leader-election on, secure metrics, json/info logging.
func Default() Values { … }
```

```go
// operator/operator.go  (InstallSpec)
type InstallSpec struct {
    Namespace string   // default "yacd-system"; now a real render input, not a reject gate
    Values    Values   // typed; replaces map[string]string
    // Version removed from the runtime path — the embedded chart's appVersion is the version.
    // (Re-introduced in PR3 if/when an OCI chart source lands.)
}
```

- **Namespace becomes real.** `EnsureOperator` renders with
  `ReleaseOptions.Namespace = spec.Namespace` (default `yacd-system`). RBAC
  subjects, SA, Role/RoleBinding, Service, Deployment all follow
  `.Release.Namespace`; ClusterRole/ClusterRoleBinding stay cluster-scoped. The
  `ssa.go:85` reject gate **relaxes** to "validate DNS-1123, default empty →
  `yacd-system`." `install -n foo` now installs correctly into `foo`.
- **Version / Decide unchanged.** The embedded chart's `appVersion` (read from
  the rendered manager Deployment label, as today) is the "embedded" input to
  `operator.Decide`. Same install/upgrade/noop/refuse safety; same typed errors.
  Upgrading the operator = upgrading the CLI (the embedded chart). Runtime
  version selection is PR3.
- **Image digest pinning moves into `Default()`** (Go consts) instead of the
  render script's `--set-string`. The chart's `values.yaml` stays generic
  (tags); `Default()` supplies digests so the default install remains
  digest-pinned and tamper-evident, offline.

### 5.4 SSA apply pipeline — unchanged

`ssa.New(...)` still takes `(kubeconfig, kubeContext, fs.FS)` and builds the same
controller-runtime client + RESTMapper. `partitionCRDs`, `waitEstablished`
(60s/1s), `applyKindRank`, field owner `yacd-cli`, `ForceOwnership`, prune label
+ GVK set, CRDs-never-pruned — all preserved verbatim. Only the *source* of the
object list changes (render vs. parse-static-yaml).

### 5.5 `devnet` rewire (minor)

`lifecycle.Manager.ensureOperatorReady` changes one line: pass
`operator.InstallSpec{Namespace: "yacd-system", Values: operator.Default()}`
instead of the zero spec. Everything else (the readiness poll, the lock, the
timeout) is unchanged. This is the "supply a `Default()` and the surgery is
minor" point.

### 5.6 Moon task changes

- `root:generate`: drop the `render-operator-chart.sh` step; add a "sync chart
  copy into the operator package" step.
- `root:check`: replace the rendered-manifest drift check with a chart-copy
  drift check (`git diff --exit-code` of `cli/internal/operator/ssa/chart` vs
  `charts/yacd`).

### 5.7 Release maintenance (relocated, not added)

On each operator release the trio to bump becomes: (1) `Chart.yaml` `appVersion`
(release-please, already automated), (2) re-sync the embedded chart copy
(`root:generate`), (3) the two image-digest consts in `operator.Default()`.
Today's equivalent trio is appVersion + render-script digests + re-render —
same surface, relocated into Go.

## 6. Component B (PR2) — the `yacd install` command

### 6.1 Command shape

Recommend a **top-level `yacd install`** (matches the request). The read-only
`OperatorState` can back `yacd info` or a later `yacd operator status`; an
`operator` noun group is an alternative if we expect to grow operator verbs
(status/uninstall) — flagged as an open question.

### 6.2 Targeting

`install` targets an arbitrary cluster, so — unlike `devnet` — it **accepts**
explicit `--kubeconfig`/`--context` and must **not** call `rejectExplicitTarget`
and **not** be wrapped in `withManagedReconcile` (no managed-devnet record to
clear). `ResolveTarget` tier-1 already short-circuits on explicit flags, so the
reuse is direct: `ctx.operatorInstaller(target.Kubeconfig, target.Context)` via
the existing `OperatorInstallerFactory` seam (`root.go:64`), no lifecycle/k3d
involved. **Open question (§7):** with no flags, should `install` fall through to
the tier-2 managed-devnet record, or require explicit/ambient targeting?

### 6.3 Flags

| Flag | Behavior |
|---|---|
| `--kubeconfig` / `--context` | target an arbitrary cluster (root persistent flags, YACD_* env-bound) |
| `-n` / `--namespace` | install namespace (default `yacd-system`); **now real** — must be wired into `InstallSpec.Namespace` (today the root `-n` is not connected to the installer at all) |
| `--wait` / `--timeout` | block on operator Deployment Available (see §6.4) |
| `--dry-run` | plan-only (see §6.5) |
| values overrides (`--set` / `-f`)? | optional; maps into `Values.Extra`. Could defer to keep v1 surface small. |

### 6.4 Readiness wait — extract from lifecycle

The only operator-readiness poll lives in `lifecycle.Manager.ensureOperatorReady`
(private, entangled with devnet). Lift the "EnsureOperator then poll
OperatorState until Ready, bounded by `--timeout`" loop into a shared helper
(candidate: the `operator` package or a small `installwait` helper) so `install`
and `devnet` share one implementation. Note the adapter's CRD-Established wait is
a separate fixed 60s budget *not* bounded by `--timeout`; decide whether
`--timeout` should also bound it (needs a small plumb through `New`/
`EnsureOperator`).

### 6.5 `--dry-run`

Plan-only first: call `OperatorState` + `operator.Decide` and print the planned
`Install`/`Upgrade`/`Noop`/`Refuse` without applying — no new adapter code. A
real server-side dry-run apply (emitting the would-be objects) is a later
enhancement.

## 7. Decisions & open questions

Recommended defaults in **bold**; the starred item is the one that changes scope.

1. **★ Version source (the key decision).** The original framing was "default to
   fetching latest from GHCR." Embedding the chart means the installed version =
   the CLI build's embedded chart (upgrade = upgrade the CLI). Options:
   - **(a) Embedded-only for PR1/PR2** — leanest (52 net-new pkgs, zero OCI),
     fully offline, delivers values + namespace flexibility now. *(recommended)*
   - (b) Add a pluggable OCI chart source in **PR3** so `--version X` / `latest`
     pulls `oci://ghcr.io/meigma/yacd/chart` — isolates the registry deps to that
     opt-in path; "latest" needs tag-list + semver-max (GHCR has no native latest
     tag) or a published moving tag.
   Recommendation: ship (a) now, design the chart-source as an interface so (b)
   slots in without churn. **Confirm this is acceptable** vs. wanting runtime
   version selection in v1.
2. **Values contract shape.** **Typed struct + `Default()` + `Extra` escape
   hatch, schema-validated** (recommended) vs. raw `map[string]any`.
3. **Dependency scope.** **Lean `loader`/`chartutil`/`engine` only** (recommended)
   vs. `pkg/action` (simpler, +270 heavy pkgs). Settled by §4 unless objected.
4. **Command shape.** **Top-level `yacd install`** (recommended) vs. `yacd
   operator install/status/uninstall` noun group.
5. **No-flag targeting for `install`.** Fall through to the managed-devnet record
   (tier-2), or require explicit/ambient? (Leaning: require explicit/ambient and
   only use ambient current-context; do not silently target the devnet.)
6. **Uninstall scope.** **Out of scope for now** (recommended) — net-new (no
   `Remove` port method; prune never deletes CRDs; CRD deletion cascades to user
   CRs) → PR3 with an explicit CRD-deletion policy.
7. **`--timeout` and the 60s CRD wait.** Leave the CRD-Established wait fixed, or
   plumb `--timeout` through to bound it too?

## 8. Phasing & PR plan

- **PR1 — package upgrade (no user-facing command).** Embed chart + drift guard;
  lean render path; `Values` + `Default()` contract; namespace as render input;
  `ssa` apply unchanged; `devnet` rewired to `Default()`; Moon tasks updated;
  remove render script + pre-render. Internally validated (envtest + render unit
  tests + `devnet` live test). *Self-contained; no behavior change for `devnet`
  users.*
- **PR2 — `yacd install` command.** Top-level verb; explicit targeting; `-n`
  wired to `InstallSpec.Namespace`; shared readiness wait; `--dry-run` plan-only;
  command tests via `mocks.Installer`.
- **PR3 (optional) — version selection + uninstall.** Pluggable OCI chart source
  for `--version`/latest; `Installer.Remove` + CRD-deletion policy.

If PR1 is large, it can split further (chart-embed + render swap as one PR, then
the `Values`/`Default()` contract as a second) — but the two are coupled
(rendering is pointless without values), so a single PR1 is cleaner.

## 9. Testing strategy

- **Render unit tests** (no helm CLI in CI): render the embedded chart with
  `Default()` and assert the expected object set + key fields (namespace
  propagation into RBAC subjects; digest-pinned images; manager args). Optional
  generate-time parity check diffing against `helm template` (the §4.1 method).
- **`install_envtest_test.go`** (exists): still covers the SSA apply path
  (CRD-first, wait-Established, prune) against envtest. Extend for namespace
  propagation and a non-`yacd-system` install.
- **`Decide` unit tests**: unchanged.
- **Drift guard test**: the in-package chart copy equals `charts/yacd`
  (or rely on `root:check`'s git-diff).
- **`devnet` live test** (`YACD_DEVNET_LIVE`): unchanged e2e coverage of the
  install-into-k3d path through the new `Default()`.
- **`install` command tests**: targeting precedence, `-n` wiring, dry-run output,
  wait/timeout, via `mocks.Installer` (`cli/internal/mocks/installer.go`).

## 10. Risks & mitigations

- **Dependency surface.** Measured and bounded (52 net-new pure-Go pkgs, zero
  OCI/docker). Mitigate drift with the import deny-list (§4.2); optionally a tiny
  `go list`-based check in `root:check`.
- **Supply-chain / offline regression.** Embedded default keeps install fully
  offline and digest-pinned (digests in `Default()`); GHCR fetch is opt-in (PR3).
- **Helm SDK (v3) vs. CLI (v4) divergence.** Render object-set verified identical
  for this chart (§4.1). The drift guard now guards the **chart copy**, not the
  render output, so SDK-vs-CLI divergence can't silently corrupt an install. Pin
  the helm module version; re-verify if the chart adopts a v4-only feature.
- **CRD upgrade safety.** Unchanged: CRDs applied + waited Established + never
  pruned. PR3 uninstall must keep CRD deletion explicit and opt-in.
- **Namespace flexibility correctness.** Rendered RBAC subjects follow
  `.Release.Namespace`; the only hardcoded `namespace: yacd-system` in today's
  output came from `--namespace`, i.e. `.Release.Namespace`. Covered by a
  non-`yacd-system` envtest install.
- **`all:` embed / `CRDObjects` / hooks** gotchas (§4.3) — documented; cheap to
  get right, expensive to debug if missed.
