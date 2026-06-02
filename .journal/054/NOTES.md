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
