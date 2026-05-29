# YACD Test Harness — Implementation Plan

Status: **proposed plan** (session 030). Companion to
[`TEST_HARNESS_PROPOSAL.md`](./TEST_HARNESS_PROPOSAL.md).

This document sequences the work to deliver the proposed test harness. It is a
**work breakdown and ordering**, not an implementation guide: it captures *what*
each phase delivers, *why it sits where it does*, and *what "done" looks like*.
Detailed technical steps and code are left to the agent(s) implementing each
phase, working from the proposal.

## Principles guiding the sequence

- **Validate the make-or-break first.** The single largest unknown is whether a
  KinD + localnet environment can reach readiness inside a standard CI runner
  within a tolerable time budget. Everything CI-facing depends on that answer,
  so it is proven before it is built on.
- **Local-complete before CI-complete.** The CLI is the foundation the CI story
  wraps. We finish a fully usable *local* experience first, then package it for
  CI.
- **Release gates CI, not local.** The operator/chart is currently unreleased.
  That blocks the published GitHub Action but not local CLI work, so the release
  runs in parallel rather than at the front of the line.
- **Each phase is a delegable unit** with its own exit criteria. A phase should
  not start until its dependencies' exit criteria are met.

## Phase map

| Phase | Delivers | Gates |
|---|---|---|
| 0 — Validate feasibility | Evidence the CI path and teardown actually work | Go/no-go for the CI story |
| 1 — CLI foundation | Identity model + environment lifecycle (`up`/`down`/`list`) | Local create/destroy |
| 2 — Host access + contract | `run`/`connect`/`exec`, `topup --await`, the env-var contract | **Local story complete** |
| 3 — Release (parallel) | First operator + chart release | Prerequisite for Phase 4 |
| 4 — CI integration | The `yacd-env` GitHub Action | **CI story complete** |
| 5 — Adoption surface | Examples + how-to docs | External developer can adopt |

Dependency flow:

```
Phase 0 ──▶ Phase 1 ──▶ Phase 2 ──┐
                                  ├──▶ Phase 4 ──▶ Phase 5
Phase 3 (parallel, anytime) ──────┘
```

---

## Phase 0 — Validate feasibility (de-risk)

**Goal.** Confirm the load-bearing assumptions before building on them, using
the tooling that exists today (no new CLI verbs required).

**Why first.** If a localnet cannot reach readiness inside a standard hosted CI
runner within budget, the entire CI design must change (smaller environments,
self-hosted runners, or a different bring-up strategy). It is far cheaper to
learn this now than after the Action is built.

**Scope.**
- Stand up KinD + the operator + a representative localnet inside a
  GitHub-hosted CI run and measure time-to-ready (cold start).
- Confirm that deleting a network cleanly removes **all** of its child resources
  (storage, secrets, generated config, supporting RBAC).
- Confirm the chain-API services (and the node socket) are reachable in the way
  the proposal's `run`/`exec` split assumes.

**Exit criteria.**
- A real cold-start measurement and a documented go/no-go for CI on standard
  runners (and, if no-go, the recommended alternative).
- Confirmation that teardown is complete, or a list of the gaps to fix.
- Confirmation (or correction) of the host-access assumptions.

**Dependencies.** None.

---

## Phase 1 — CLI foundation: identity and lifecycle

**Goal.** Make environment identity a command-line concern and deliver the
create/inspect/destroy lifecycle.

**Why here.** Everything else keys on the identity model and the lifecycle
verbs. This is also where the one intentional breaking change lands, while it is
still free to make.

**Scope.**
- Move network identity (name, namespace) out of the spec file and onto the
  command line; default the namespace to the network name; auto-create and
  ownership-stamp the namespace; reject invalid names clearly.
- Deliver the lifecycle verbs: bring an environment up and wait for readiness,
  tear it down and wait for clean removal, and list environments in a cluster.

**Exit criteria.**
- A developer can create, list, and destroy a named environment on a local
  KinD/k3d cluster from a single spec file, with namespaces handled
  automatically and teardown that waits for real cleanup.

**Dependencies.** Phase 0 (so teardown behavior is understood before `down`
relies on it).

---

## Phase 2 — Host access and the integration contract

**Goal.** Let a developer run their own tests against the environment with zero
awareness of any YACD file format, and fund a known address deterministically.

**Why here.** This is the heart of the harness and the point at which the
**local story is complete**: manual local testing works end to end and satisfies
the spec-over-tuning, UX, and k8s-centric criteria locally.

**Scope.**
- Define and document the environment-variable contract that tests consume.
- Deliver the three access ergonomics over a shared forwarding mechanism: a
  scoped "run my command with the environment wired in" path (the primary one),
  a persistent foreground "keep it reachable while I work" path, and an in-pod
  path for socket-bound tools.
- Make funding wait for on-chain confirmation so tests do not race inclusion.

**Exit criteria.**
- A developer can execute an arbitrary test command locally that reaches the
  network purely through the env-var contract, fund a checked-in address and see
  the funds confirmed, and use socket-bound tooling through the in-pod path.

**Dependencies.** Phase 1.

---

## Phase 3 — Release the operator and chart (parallel)

**Goal.** Produce the first real, versioned operator and chart release so a
pinned version exists to install.

**Why parallel.** The CI story cannot install a pinned chart until one exists,
but this work does not depend on the CLI changes, so it can proceed alongside
Phases 1–2.

**Scope.**
- Resolve the placeholder versioning and cut the first published operator/chart
  release through the repo's normal release process.

**Exit criteria.**
- A pinned, published operator/chart version is installable and ready to be
  referenced by the CI integration.

**Dependencies.** None (can start anytime); must complete before Phase 4.

---

## Phase 4 — CI integration: the GitHub Action

**Goal.** Package the now-proven local flow into one reusable CI integration so
the same spec and verbs run in CI with a single step.

**Why here.** It depends on both a complete local flow (Phase 2) and a
releasable operator/chart (Phase 3), and it is validated by the feasibility
evidence from Phase 0.

**Scope.**
- A first-party reusable action that provisions a cluster, installs the pinned
  operator/chart, brings the environment up, runs the developer's test command
  through the harness, and guarantees teardown on every exit (including
  cancellation).
- Failure diagnostics that capture the environment's child workloads, not just
  the operator.
- Prove it in this repository's own CI before recommending it to external
  consumers.

**Exit criteria.**
- The same spec and verbs used locally run green in this repo's CI via one step,
  with reliable teardown and useful failure artifacts.

**Dependencies.** Phases 2 and 3; informed by Phase 0.

---

## Phase 5 — Adoption surface: examples and docs

**Goal.** Make the harness easy to pick up, so a developer can reach a first
green E2E test by copying and adapting a worked example.

**Why last.** It documents the finished, proven flow; doing it earlier risks
documenting something that still shifts.

**Scope.**
- A complete worked example (spec, a checked-in test key, and the small steps to
  derive and fund an address).
- A task-oriented how-to that shows the identical local-and-CI flow, the
  run-versus-in-pod distinction, and the fresh-build model.

**Exit criteria.**
- An external developer can follow the example to a working E2E test against a
  crafted network, using the same spec locally and in CI.

**Dependencies.** Phase 4.

---

## Milestones (criteria coverage)

- **End of Phase 2:** local manual testing complete — criteria 2, 3, 4 met
  locally and the manual half of criterion 1.
- **End of Phase 4:** CI complete — criterion 1 fully met, with the same spec
  driving local and CI.
- **End of Phase 5:** adoption-ready — the UX criterion is demonstrated, not
  just claimed.

## Explicitly out of this plan (future backlog)

Recorded so they are not forgotten, but not scheduled here:

- Background/detached `connect` and a paired disconnect.
- Guarded namespace auto-delete honoring the ownership stamp.
- An in-cluster execution mode for tests that cannot run on the runner host.
- First-class k3d support in the CI action.
- A snapshot/restore cache layered over fresh-build.

## How to run this plan

Treat each phase as a self-contained assignment for one or more agents. The
implementing agent derives the technical steps from
[`TEST_HARNESS_PROPOSAL.md`](./TEST_HARNESS_PROPOSAL.md); the analysis and
rejected alternatives behind these decisions live in
[`TEST_HARNESS_DESIGN.md`](./TEST_HARNESS_DESIGN.md). Do not begin a phase until
its dependencies' exit criteria are satisfied; Phase 0's go/no-go may revise the
CI-facing phases before they start.
