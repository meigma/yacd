---
id: 030
title: YACD test harness design
date: 2026-05-28
status: complete
repos_touched: [yacd]
related_sessions: [029]
---

## Goal
Design how YACD should support using the operator as a test harness for E2E (and
similar) tests that need a crafted Cardano network — a developer-forward,
spec-driven, k8s-centric experience that works the same locally and in CI.
Design only; implementation deferred to future sessions/agents.

## Outcome
Goal met. This was a design/discussion session producing **no code** — all
output is documentation on the `journal/jmgilman` branch (no implementation PR;
design docs live under `.journal/`, which is never merged to the default
branch). Three linked artifacts were written to `.journal/030/`:
- `TEST_HARNESS_DESIGN.md` — the multi-agent workflow report (analysis +
  rejected alternatives + adversarial critique record).
- `TEST_HARNESS_PROPOSAL.md` — the decided, human-authored design.
- `TEST_HARNESS_PLAN.md` — a six-phase, non-technical implementation plan.

Earlier in the session the snapshot/restore direction (`SNAPSHOT_DESIGN.md`,
`SNAPSHOT_RESEARCH.md`) was critically reviewed and set aside as a possible
later cache, not the v1 path.

## Key Decisions
- **Fresh-build per test over a bespoke snapshot/restore format** -> snapshots
  carry hard slot/time re-anchoring (every CI restore happens at a later wall
  clock) and node-version-pinning problems; fresh-build is the substrate a
  snapshot would merely cache, so it is the sounder first investment and avoids
  both problems. Snapshot kept as a possible later optimization only.
- **Use the existing operator + CLI as the harness; build thin tooling, not a
  testing framework** -> the operator already ships the load-bearing pieces
  (compressed-epoch localnet via `LocalNetworkSpec`, deploy+wait, `info --json`,
  faucet `topup` to an arbitrary address); the gap was teardown, host access,
  and a CI wrapper.
- **Ran an adversarial design workflow (14 agents) instead of single-pass
  design** -> it verified real blockers against the code (cluster-internal
  ClusterIP endpoints unreachable from host; chart still `0.0.0`/unreleased; the
  KinD+localnet path has never run in hosted CI; `connection.json` lacks the
  chain-API endpoints; `topup` returns on submission not inclusion) rather than
  asserting feasibility.
- **Identity becomes a CLI argument; drop `metadata` from the devconfig spec**
  -> spec describes network *shape*, name/namespace are runtime identity, so one
  spec deploys under many names (parallel shards, local vs CI). Free to do now
  while unreleased.
- **`run` vs `exec` split** -> `run` wires host TCP access (Ogmios/Kupo/faucet)
  via scoped port-forwards + a `YACD_*` env-var contract; `exec` runs in-pod for
  socket-bound tools because `cardano-cli` uses the node Unix socket, which a
  port-forward cannot expose. This was a concrete feasibility catch in the
  workflow's `run -- cardano-cli` example.
- **`connect` is foreground + supervised in v1; detached/background deferred**
  -> a managed background forwarder is a large complexity step (process
  supervision, stale-state) for a local-only convenience.
- **Namespaces auto-created and ownership-stamped; no auto-delete in v1** ->
  deleting a namespace YACD did not create is destructive; defer behind the
  stamp.
- **Validate the make-or-break CI-runner cost before building the Action** ->
  the phased plan puts a feasibility spike (cold-start measurement + teardown-GC
  assertion) first, using today's tooling.

## Changes
- `.journal/030/TEST_HARNESS_DESIGN.md` - workflow-generated analysis/report
  (added).
- `.journal/030/TEST_HARNESS_PROPOSAL.md` - decided design: verb set
  (`up/down/list/info/connect/run/exec/topup --await/env`), identity model,
  host-access engine, `YACD_*` contract, `yacd-env` GitHub Action, criteria fit
  (added).
- `.journal/030/TEST_HARNESS_PLAN.md` - six-phase, non-technical work plan with
  dependency flow and exit criteria (added).
- `.journal/030/NOTES.md` - running session log (design discussion, CLI
  assessment, workflow launch/result, refinements).
- No source code changed; no implementation PR.

## Open Threads
- **Unverified make-or-break:** whether a KinD + localnet reaches readiness
  inside a standard hosted CI runner (~2 vCPU / 7-8 GB) within budget. Phase 0
  of the plan must answer this before the Action is built.
- **Operator/chart is unreleased (`0.0.0`)** — a hard prerequisite for the CI
  story; only `cardano-testnet/*` tags exist; OCI ref is
  `oci://ghcr.io/meigma/yacd/chart`.
- **`exec` assumes** the chain-API containers and node socket live in the
  primary node Pod — confirm against the rendered Pod before building it.
- **Clean ownerRef teardown unverified** for artifact-publisher RBAC,
  network-artifacts ConfigMap, and PVCs; Phase 0 should assert full GC.
- **`topup --await` depends on Kupo**; needs an alternative confirmation source
  if a spec disables Kupo.
- Implementation has not begun; the plan delegates each phase to future agents.

## References
- Workflow run: `wf_b8b8be33-2ec` (14 agents; 3 designs x 3 adversarial
  reviewers -> synthesis -> report).
- `.journal/030/TEST_HARNESS_DESIGN.md`, `TEST_HARNESS_PROPOSAL.md`,
  `TEST_HARNESS_PLAN.md`.
- Prior snapshot work: `.journal/SNAPSHOT_DESIGN.md`,
  `.journal/SNAPSHOT_RESEARCH.md`.
- CLI surface assessed: `cli/internal/cli/{deploy,info,topup}.go`,
  `cli/internal/kube/{client,wait}.go`, `cli/internal/devconfig/config.go`,
  `examples/local/yacd.yaml`, `api/v1alpha1/cardanonetwork_types.go`.

## Lessons
- The snapshot-vs-fresh-build question dissolves once you see a snapshot as a
  *cache* over the fresh-build path rather than a peer to it: fresh-build is the
  substrate, so it is strictly the first thing to build, and it sidesteps
  re-anchoring and version-pinning entirely.
- A spec-driven harness gets *stronger*, not weaker, by moving identity
  (name/namespace) out of the spec and onto the CLI — the spec then captures
  only what should be identical across environments.
- An adversarial design pass earns its cost in feasibility: it caught the
  ClusterIP host-reachability gap, the `cardano-cli` socket problem, and the
  unreleased chart — each of which would otherwise have surfaced mid-build.
