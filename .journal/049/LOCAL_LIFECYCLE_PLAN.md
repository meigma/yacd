# yacd CLI — Local Cluster Lifecycle (Implementation Plan)

Implements `LOCAL_LIFECYCLE_DESIGN.md`. Each phase is **one PR**. Decided design
is not relitigated here — this plan is sequencing, scope boundaries, guardrails,
and exit criteria so the implementer stays on pace and on-architecture.

## How to read this

Each phase lists: **Goal**, **Depends on**, **In scope**, **Out of scope**,
**Guardrails**, **Exit criteria**, **Verification**. Phases are ordered by
dependency. Keep PRs reviewable; if a phase grows large, split it but preserve the
order. Do not pull later-phase scope forward to "save a PR."

## Global conventions (every PR)

- **Moon is the task front door.** No Makefile. Run `root:generate` after any
  marker/manifest/CRD change; `root:check` and `root:test` must be green; keep the
  Moon task surface small.
- **Architecture discipline.** New port/adapter pairs follow the design layout: a
  light port package (interface + types + `doc.go`) with each adapter as a
  subpackage. Keep the Cobra command layer thin; orchestration lives in
  `lifecycle.Manager`. Do **not** fork the `CardanoNetwork` apply/wait lifecycle —
  reuse the existing `kube.Client` path.
- **Tests.** New port interfaces get mockery (v3) mocks under `cli/internal/mocks`;
  unit tests use Testify and run against mocks. Adapters that touch real systems
  (k3d, Docker, a live cluster) get **integration tests gated** behind a build tag
  or a dedicated Moon task — never in the default unit suite.
- **Scope discipline (anti-gold-plating).** Across all phases do NOT: import k3d as
  a Go library; overload `up` with provisioning; build a `yacd cluster
  create|list` noun group; implement `--persist`, a local registry, multi-node, or
  mainnet quickstart. These are explicitly out of scope per the design.
- **Documentation is out of scope.** No phase ships user documentation
  (quickstart, README, guides) or help-text polish — docs are handled as a whole
  in a separate session. Implement cobra command metadata minimally (`Use`/
  `Short`); runtime output the feature needs (progress, next-step hints, the
  first-run banner) is behavior, not docs, and stays in scope.
- **Definition of done (per PR).** Builds; `root:check`/`root:test` green;
  `root:generate` idempotent (`git diff --check` clean); new code is tested;
  Conventional Commit PR title; PR body links this plan + the design doc and names
  the phase.

## Dependency graph

```
P1 (release v0.1.0) ─────────────► P4 (operator install: embeds v0.1.0 chart)
P2 (toolbin) ──► P3 (cluster + clusterstate)
P3, P4 ─────────────────────────► P5 (devnet all-in-one)  ──► P6 (hardening + docs)
P7 (funded wallet, operator-side) gates P5's funded-wallet UX — sequence per open question
```

---

## Phase 1 — Cut `v0.1.0` of the operator + Helm chart

**Goal.** Produce a real, published, installable `v0.1.0`: operator/sidecar
images on `ghcr.io/meigma/yacd` and the Helm chart at `0.1.0` (appVersion
`v0.1.0`), so the CLI has a reliable, versioned thing to install. This is pure
release plumbing — **no operator behavior changes.**

**Depends on.** Nothing in this plan. Precondition: master is in a coherent,
releasable state — coordinate with the in-flight F0 series so `v0.1.0` represents
a consistent operator (local + curated-public per current direction). Do not cut
mid-redesign.

**In scope.** Drive the existing release-please + `release.yml` flow to a `v0.1.0`
tag; confirm images and the OCI chart publish and are attested; ensure the chart's
default image references resolve to the `v0.1.0` published images and CRDs ship in
the chart.

**Out of scope.** Any CLI work; any new operator features; chart restructuring
beyond what the release requires.

**Guardrails.**
- Use the established release-please/`release.yml` path; do not hand-roll a
  release. Keep the chart name `chart`.
- Do not seed `CHANGELOG.md` for any component (release-please owns it).
- Keep CRDs delivered as today (chart `crds/`); do not move them in this PR.
- The published chart's rendered default must reference `ghcr.io/meigma/yacd`
  images at `v0.1.0` — this is what the CLI will later embed.

**Exit criteria.**
- Tag `v0.1.0` exists; `release.yml` ran green; images + OCI chart published and
  attested.
- A fresh KinD/k3d cluster + `helm install` (or pull) of the published chart at
  `0.1.0` brings the operator Deployment to Available, and a representative
  `CardanoNetwork` reaches Ready.
- The canonical chart OCI ref + version and the published image refs are recorded
  in the PR body for Phase 4 to pin against.

**Verification.** Existing release-dry-run + e2e; one manual install-from-published
smoke into a throwaway cluster.

---

## Phase 2 — `toolbin`: pinned k3d binary resolver

**Goal.** Resolve a verified, pinned k3d binary on demand: locate it, or fetch +
checksum-verify + cache it under an XDG path.

**Depends on.** None.

**In scope.** The `toolbin.Resolver` port + `toolbin/ghrelease` adapter (see
Design §10.2/§10.3); per-os/arch asset selection; SHA256 verification against a
digest **embedded in the CLI at build time**; install under `$XDG_DATA_HOME/yacd/
bin/k3d-<ver>`; GC of superseded versions; pre-staged skip (binary already present
or `YACD_K3D_PATH`).

**Out of scope.** Any cluster/Docker interaction; using the binary.

**Guardrails.**
- Verify against the **embedded** digest, not a checksum file fetched at runtime.
  Pin the download host; reject redirects to other hosts; fail closed on mismatch.
- This package must not import Docker or k3d-library code. HTTP client is
  injectable (mock in unit tests).
- XDG path resolution must have a sane per-OS fallback (honor `XDG_*` when set).

**Exit criteria.**
- `Resolve` returns a path to a verified, executable, pinned k3d; re-runs are
  cache hits; a corrupted/short download fails closed with expected-vs-got.
- Old pinned versions are GC'd on a new fetch; `YACD_K3D_PATH`/pre-staged binary
  skips the fetch.

**Verification.** Unit tests with a mocked HTTP client (success, checksum
mismatch, redirect rejection, pre-staged skip). One gated integration test that
actually fetches the pinned release and verifies it.

---

## Phase 3 — `cluster` + `clusterstate`: provisioning core

**Goal.** Idempotently create / heal / delete / inspect the singleton managed k3d
cluster, with host-side bookkeeping and a process lock. Library only — no user
commands yet.

**Depends on.** P2 (`cluster/k3d` uses `toolbin.Resolver`).

**In scope.** `cluster.Provisioner` port + `cluster/k3d` adapter (the
`EnsureCluster` state machine: absent→create, healthy→no-op,
unhealthy→delete+recreate; transactional create with partial-create rollback;
pinned k3s image; readiness gating). `clusterstate.Store` port +
`clusterstate/file` adapter (`Record` JSON + `flock`). The fixed managed name
(`k3d-yacd`) and pinned k3s image defined as single-source constants.

**Out of scope.** Operator install; the `devnet` commands; the targeting resolver
wiring into existing verbs (P5); context-switch UX wording (P5 surfaces it).

**Guardrails.**
- `cluster/k3d` shells out through an **injected command runner** so the adapter
  is unit-testable without Docker; the real k3d call path is covered by a gated
  integration test.
- `EnsureCluster` treats the runtime as authoritative (Design §6 "State & source
  of truth"); the state record is supplementary and self-healed.
- The lock scope is the managed cluster (per user), not a worktree. Hold it across
  all cluster-mutating ops.
- Fixed cluster name + k3s pin live in exactly one place; the context
  (`k3d-yacd`) is derived, not hardcoded at call sites.

**Exit criteria.**
- Integration test (gated, Docker required): `EnsureCluster` creates a cluster;
  a second call is a no-op; deleting the cluster out-of-band then calling
  `EnsureCluster` heals it; an injected mid-create failure rolls back (no orphan).
- Unit tests: `clusterstate` load/save/clear round-trips; missing record returns
  not-found cleanly; concurrent `Lock` serializes.

**Verification.** Unit (mocked runner + filesystem) + one gated Docker integration
test. Mocks generated for both new ports.

---

## Phase 4 — `operator`: idempotent install via SSA

**Goal.** Install/upgrade the operator into the managed cluster from the embedded,
build-time-rendered `v0.1.0` chart, idempotently, with version reconciliation.

**Depends on.** P1 (a real `v0.1.0` chart + images to render/embed and pull).

**In scope.** A generate step (Moon task) that renders `charts/yacd` at the pinned
`v0.1.0` into a manifests dir, embedded via `//go:embed`. The `operator.Installer`
port + `operator/ssa` adapter: apply CRDs first and wait Established, then
RBAC/SA/Deployment, under a yacd field-owner with label-based prune. Record + read
the installed operator version; reconcile (absent→install, older same-major→
upgrade, newer/major-mismatch→refuse with an actionable message).

**Out of scope.** The `devnet` commands; cluster provisioning (P3); the funded
wallet (P7).

**Guardrails.**
- Pre-render at build time; do **not** import the Helm SDK at runtime and do not
  pull the chart over the network at runtime. The render is generated, not
  hand-maintained, and pins `v0.1.0`.
- CRDs upgrade via SSA only on this path (Design §7); CRDs-first + wait Established
  before any CR-bearing object.
- Keep the `operator` port dependency-light; the SSA client + embedded `fs.FS`
  live only in `operator/ssa`.
- Default image refs come from the rendered chart (the `v0.1.0` published images);
  no registry assumed.

**Exit criteria.**
- Integration test (gated): into a Phase-3 cluster, `EnsureOperator` brings the
  manager to Available; a second call is a no-op; a CRD schema change re-applies
  in place; a simulated newer-in-cluster version is refused with the documented
  message.
- `root:generate` reproduces the embedded manifests deterministically.

**Verification.** Unit tests for version-reconcile logic (mocked); gated Docker
integration test for the real SSA install.

---

## Phase 5 — `devnet`: the all-in-one (first end-to-end milestone)

**Goal.** `yacd devnet` takes a Docker-only machine to a Ready local devnet; this
is the first user-facing milestone.

**Depends on.** P2, P3, P4.

**In scope.** `lifecycle.Manager` composing the ports (Design §10.4); the `devnet`
command subtree (`devnet [--bare]`, `devnet down`, `devnet status`); the embedded
default local Environment; the shared targeting resolver (`target.go`, precedence
in Design §6) wired into all existing verbs; context switch + tracking + restore;
the `Options` factories for the new ports; stepwise progress output incl.
image-pull/pod sub-progress; the magic-interpolated `exec` tip-query hint in
output.

**Out of scope.** Failure-message taxonomy polish, `--purge`/binary GC surfacing,
docs/quickstart (P6); the funded wallet (P7) — until P7, output points users to
derive an address via `exec` instead of claiming a funded wallet.

**Guardrails.**
- Reuse the existing `up` apply/wait path for the default network; do not
  re-implement CR rendering or readiness.
- One targeting resolver; every mutating verb prints the resolved target. The
  managed-context tier must only engage when a managed cluster exists, so existing
  automation (explicit `KUBECONFIG`/`--context`) is unaffected — confirm CI/
  Chainsaw stay green with no test edits.
- `Manager.Up` is idempotent/reentrant: a re-run after interruption converges, not
  errors. Hold the cluster lock around the mutating sequence.
- Keep `devnet.go` thin; all sequencing in `lifecycle.Manager`, unit-tested with
  mocked ports.

**Exit criteria.**
- On a Docker-only machine: `yacd devnet` → cluster + operator + default network
  Ready, endpoints + next-step hints printed; `devnet status` reflects reality;
  `devnet down` removes the cluster and restores the prior kube context; a re-run
  of `devnet` is a near-instant no-op; `--bare` stops after the operator.
- Existing verbs (`info`/`run`/`exec`/`topup`/`up`) operate on the managed cluster
  with no explicit `--context`.
- CI/Chainsaw/`yacd-env` unchanged and green.

**Verification.** Unit tests for `lifecycle.Manager` against mocked ports (incl.
interruption/reentrancy); a new gated k3d-based end-to-end (provision→install→
network→use→down). The existing KinD Chainsaw e2e stays as-is for operator
testing.

---

## Phase 6 — Hardening & UX

**Goal.** Make first-run and failure paths legible, and ship the cleanup/guards
that make the tool safe to "just try."

**Depends on.** P5.

**In scope.** A typed failure taxonomy mapped to actionable messages (Docker
unavailable, host port conflict, disk pressure / pod eviction, checksum mismatch,
version mismatch); Docker/VM-disk preflight; `devnet down --purge` (cluster +
managed state + fetched binaries) and binary GC surfacing; the first-run banner
(what was created + where + the cleanup command); WSL2 validation; an ARM
multi-arch CI guard for the operator/Cardano images.

**Out of scope.** New behavior beyond messaging/cleanup/guards; `--persist`;
documentation (handled in a separate docs session).

**Guardrails.**
- Map adapter errors to human messages at the command layer; don't leak raw
  output. Don't promise a fixed first-run time — frame it as image-pull-bound.
- `--purge` must remove the full on-disk footprint described in the design.

**Exit criteria.**
- Each named failure mode produces a specific, actionable message (covered by
  tests where the error is synthesizable).
- `devnet down --purge` leaves no yacd-created cluster, state, or binaries; GC
  keeps only the current pinned binary.
- WSL2 path validated; CI fails if a published operator/Cardano image loses its
  arm64 manifest.

**Verification.** Unit tests for the error mapper and GC; manual WSL2 run; the ARM
guard exercised in CI.

---

## Phase 7 — Funded-wallet bootstrap (operator-side dependency)

**Goal.** The default devnet ships at least one pre-generated, pre-funded wallet
the user owns, so the user story's "fund an address" is true zero-config.

**Depends on.** P1 (operator release cadence). **Gates** P5's funded-wallet output
line. This is **operator/CRD** work, distinct from the CLI lifecycle; sequence per
open question (can land before P5 to complete the story on day one, or as a fast
follow with P5 shipping an `exec`-based address-derivation hint in the interim).

**In scope.** Operator bootstraps wallet keys into a Secret for the default
(local) network and publishes the address in status; the CLI surfaces the address
(and a key-export path) in `devnet`/`info` output.

**Out of scope.** A general wallet/key-management platform; non-local profiles.

**Guardrails.**
- Keep it narrow (a bootstrap wallet for local devnets), consistent with the
  faucet/topup design. Follow the operator practices in the repo skills (status
  conditions, owned children, RBAC aligned to reads/writes).
- This is the only phase that touches `api/` + `internal/controller`; run
  `root:generate` and add envtest coverage per the k8s-operator skill.

**Exit criteria.**
- A fresh `yacd devnet` yields a funded address shown in `info`/output, usable
  immediately (e.g., a `topup`/query against it succeeds).
- Operator envtest + a Chainsaw assertion cover the bootstrap.

**Verification.** envtest for the controller behavior; Chainsaw for the
installed-operator path; manual `devnet` → funded-address check.

---

## Open questions that affect the plan

- **Verb name** (`devnet` vs `dev`/`quickstart`/`start`) — resolve before P5; it
  fixes the command name and help text.
- **Funded-wallet sequencing** — decides whether P7 lands before or after P5 (see
  P7). Until resolved, P5 ships the `exec`-based interim hint.
- **Chain-data default** (ephemeral vs `--persist`) — does not block any phase;
  `--persist` is post-plan. Confirm the ephemeral default messaging in P6.
