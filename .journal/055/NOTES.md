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
