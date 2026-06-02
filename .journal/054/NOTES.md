---
id: 054
title: Local lifecycle plan — continue from Phase 2/3/5
started: 2026-06-02
---

## 2026-06-02 07:19 — Kickoff
Goal for the session: continue executing the session-049 `LOCAL_LIFECYCLE_PLAN.md`
for the `yacd devnet` all-in-one local lifecycle, picking up from where session 053
left off. The user asked me to review session 053 first, then await the next
instructions before doing substantive work.

Current state of the world:
- The plan lives at `.journal/049/LOCAL_LIFECYCLE_{PLAN,DESIGN}.md` (design approved,
  execution started). Dependency graph:
  `P1 → P4 → P5`, `P2 → P3`, `P3,P5 → P6 → P7`.
- **Phase 1 (operator `v0.1.0`) and Phase 4 (funded developer wallet, released as
  `v0.1.1`) are DONE+merged** (session 053). NOTE: pre-1.0 `feat`s bump PATCH here
  (`bump-patch-for-minor-pre-major: true`), so the wallet shipped as `v0.1.1`, NOT
  the plan's "v0.2.0". **Phase 5 must embed `0.1.1`**, not 0.2.0.
- Published, attested refs to pin against:
  - manager `ghcr.io/meigma/yacd:v0.1.1@sha256:5d53ca824dacad39c482dc93edfd2db4a65d5803f43dce5b18b1a7482b0f8e21`
  - faucet `ghcr.io/meigma/yacd/faucet:v0.1.1@sha256:826f8d52f0a4b0f607e2293cf72a8217de27700b5e5f1b35e1af86ef18fd3f66`
  - chart `oci://ghcr.io/meigma/yacd/chart:0.1.1@sha256:a8d24dfaa19a4af0279ed26654ff36a44e5cf50a05a5e0ffa02481688a5a049f`
- **Remaining plan phases:** P2 (`toolbin` — pinned k3d binary resolver), P3
  (`cluster` + `clusterstate` — depends on P2), P5 (`operator` install via SSA,
  embeds 0.1.1 — depends on P1+P4, both done), P6 (`devnet` all-in-one — depends on
  P2/P3/P5), P7 (hardening). P2/P3 and P5 are independent and can proceed in
  parallel.
- Two GitHub draft releases (`v0.1.0`, `v0.1.1`) are intentionally left unpublished
  for the user to Publish manually; GHCR artifacts are already public.
- Master is clean at `8c388cd` (release 0.1.1, #85). No implementation worktree
  selected yet; dev stack not started.

Plan: awaiting the user's specific instruction on which phase to start. Will select/
create an implementation worktree and run `moon run root:dev-up` once the work is
scoped (note: the dev stack matters for operator-side work like P5's SSA install
testing; pure CLI port work P2/P3 may not need it, TBD per instructions).

## 2026-06-02 08:31 — Phase 5 implemented (operator install via SSA)

User chose Phase 5. Plan approved (digest-pin images + pin install namespace to
`yacd-system`). Implemented on worktree/branch `feat/cli-operator-install` (from
master `8c388cd`). Dev stack `kind-yacd-dev` started once via `root:dev-up` (exit 0).

What landed (commit `38cc848`):
- `cli/internal/operator/` port: `InstallSpec`/`State`/`Installer` + pure
  `Decide(embedded, state)` reconcile (x/mod/semver). doc.go/operator.go/
  reconcile.go + table test.
- `cli/internal/operator/ssa/` adapter: `New(kubeconfig, ctx, fs.FS)`,
  `EnsureOperator`/`OperatorState`. Parses embedded multi-doc YAML →
  unstructured, applies CRDs first + waits Established (typed apiextensionsv1),
  then SSA-applies the workload in a stable kind order under field owner
  `yacd-cli` (+ForceOwnership), namespace-defaulting namespaced objects via the
  RESTMapper scope, label-based prune (`yacd.meigma.io/install=operator`) over a
  fixed GVK set that **excludes CRDs**. Version read from the manager
  Deployment's `app.kubernetes.io/version` label (no ConfigMap). Install ns
  pinned to `yacd-system`; foreign ns rejected.
- `embed.go` `//go:embed manifests/operator.yaml`; manifest rendered by
  `.dev/scripts/render-operator-chart.sh` (`helm template … --include-crds
  --no-hooks` + `--set-string image.digest`/`faucet.image.digest` to the v0.1.1
  published digests). Version label stays appVersion `v0.1.1` (the reconcile
  source of truth); digests live only in the render script (bump on release).
- Wiring: `render-operator-chart` Moon task + inlined into `root:generate`
  AFTER controller-gen (so CRD changes flow into the embed same pass); manifest
  added to generate outputs + `check.sh` drift guard. `.mockery.yml` gained
  `operator.Installer` → `cli/internal/mocks/installer.go`.

Verification (all green): `moon run root:generate` (render idempotent, mock
written), `moon run root:check` (fmt/lint/helm/drift), `moon run root:test`
(full suite). New envtest test starts envtest with NO preloaded CRDs and proves:
install from embedded manifests, CRDs Established, namespace defaulting,
cluster-scoped get no ns, idempotent re-apply, stray-labeled object pruned,
`OperatorState` version, **asserts NOT Ready** (envtest has no
kube-controller-manager), refuse-on-newer (`ErrNewerOperator`), foreign-ns
rejection. Pure tests: Decide table, manifest parse/empty-skip, kind ordering,
embedded version == v0.1.1, Available predicate.

Deliberately NOT done (scope/decisions):
- Live `Ready` (Deployment Available with the digest image) is the one thing
  envtest can't prove. NOT run against `kind-yacd-dev` (would take Helm field
  ownership from Tilt and break the dev loop). It's already evidenced by session
  053's published-chart smoke using the SAME v0.1.1 digests, and is the P6
  gated k3d e2e's job. Left as deferred.
- No devnet commands / cluster provisioning / lifecycle wiring / Options factory
  (P3/P6).

Next: push branch, open PR. Then P2 (toolbin) / P3 (cluster) remain before P6.

## 2026-06-02 09:05 — PR #86 merged

PR #86 opened. CI green: `ci`, `e2e` (6m20s), `cardano-tools-image` all PASSED.
**Kusari Inspector** failed (the only red). All Kusari findings were pre-existing
operator RBAC — the `yacd-manager-role` secrets verbs
(create/delete/get/list/patch/watch) + services verbs
(create/delete/get/list/patch/update/watch) are **byte-identical** to
`charts/yacd/templates/rbac-manager.yaml` (controller-gen'd, shipped in v0.1.1).
My embed is a faithful `helm template` copy — zero new privilege. Kusari passed
on #84 only because it skips Helm templates (invalid standalone YAML); my
rendered `operator.yaml` is the first concrete YAML it scans. Weakening the RBAC
would break the controllers (they create/delete/patch faucet/wallet/pgpass
Secrets + node/ogmios/kupo/metrics Services), so that was off the table.
`master` is **not a protected branch** → Kusari is advisory, non-blocking.

User chose **merge as-is**. Posted an accepted-risk comment on #86 explaining the
above, then squash-merged → `941c0c0` on master. Removed the worktree + deleted
the merged branch `feat/cli-operator-install`. (release-please will queue a
patch release PR, 0.1.1→0.1.2, for the `feat(cli)` — gated, not auto-released.)

**Phase 5 is DONE+merged.** Remaining local-lifecycle plan: P2 (toolbin), P3
(cluster+clusterstate, needs P2), then P6 (devnet, needs P2/P3/P5), P7. P5's
live-`Ready` proof is the P6 gated k3d e2e's job (already evidenced by session
053's published-chart smoke with the same v0.1.1 digests). Dev stack still up.

## 2026-06-02 09:28 — Phase 2 (toolbin) implemented + PR #88

User chose P2 next. Plan approved (pin k3d **v5.9.0**; live test gated by env-var
skip). Branch `feat/cli-toolbin` from master `941c0c0`.

Landed (commit on branch, PR #88):
- `cli/internal/toolbin/` port: `Resolver` iface, `Pin{Version,AssetURL,SHA256
  map[os/arch]}`, local `HTTPDoer` seam (defined in toolbin, NOT imported from
  cli — keeps dep direction adapter→port), `DefaultDir()` (XDG_DATA_HOME/yacd/bin,
  ~/.local/share fallback). doc.go/toolbin.go/toolbin_test.go.
- `cli/internal/toolbin/ghrelease/` adapter: `New(pin,dir,doer)` + `Resolve()`:
  pre-staged `YACD_K3D_PATH` escape → digest cache hit → fetch → verify embedded
  SHA256 → atomic install (CreateTemp+Write+Chmod 0o755+Rename) → GC superseded
  `k3d-*`. **Redirect handling is the key twist vs cardano-tools fetch**: GH
  release assets 302 to `release-assets.githubusercontent.com`, so the adapter
  manually follows redirects but allow-lists GitHub download hosts
  (`github.com`, `*.githubusercontent.com`) and rejects others; `DefaultHTTPClient()`
  returns an http.Client with `CheckRedirect → ErrUseLastResponse` so the adapter
  (not the client) controls following. Digest is the real guard (fail-closed on
  mismatch). pin.go: `DefaultK3dPin` = v5.9.0 + 4 digests from checksums.txt.
- `.mockery.yml` += `toolbin.Resolver` → `cli/internal/mocks/resolver.go`.

Verification: `root:generate` idempotent, `root:check` (fmt/lint — fixed an
`unparam` on a test helper), `root:test` all green. Mocked unit suite covers
download/verify/install (0o755), CDN-redirect follow, disallowed-host reject,
digest-mismatch fail-closed, pre-staged skip (0 calls), cache hit (0 calls), GC,
unsupported platform, host-allowlist predicate. **Live test RAN locally**
(`YACD_TOOLBIN_LIVE=1`, darwin/arm64): really downloaded k3d v5.9.0, followed the
real CDN redirect, verified digest, ran `k3d version` → v5.9.0 (2.0s). No new
go.mod deps (stdlib only).

Next: watch PR #88 CI. Then P3 (cluster + clusterstate, depends on P2/toolbin).

## 2026-06-02 09:33 — PR #88 merged (Phase 2 done)

CI: `ci` PASS (1m35s), **Kusari PASS** (21s — no RBAC/manifest/new-deps to flag,
unlike P5). `e2e`/`cardano-tools-image` were still running at merge but are
structurally unaffected (CLI-only change; e2e builds the manager image + runs
Chainsaw, no `cli/` dependency). Squash-merged → `bc2f739` on master; worktree
removed + branch deleted. release-please will queue another patch bump for the
`feat(cli)`.

**Phases done this session: P5 (#86) + P2 (#88).** Plan status:
P1✅ P4✅ P5✅ P2✅ | remaining: **P3** (cluster+clusterstate, needs P2 ✓ now) →
P6 (devnet, needs P2/P3/P5 — P2✓ P5✓, blocked on P3) → P7. Dev stack still up.
