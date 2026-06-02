---
id: 049
title: yacd CLI local cluster lifecycle (k3d) — design + plan
date: 2026-06-01
status: complete
repos_touched: [yacd]
related_sessions: [008, 030, 036, 041]
---

## Goal
Design (no implementation) an "all-in-one" local lifecycle for the yacd CLI —
one command that stands up a local Kubernetes cluster, installs the yacd
operator, and creates a Cardano network — and produce a phased implementation
plan. Record both persistently in the journal for a later execution session.

## Outcome
**Met.** Two reviewed-and-approved documents were produced under `.journal/049/`:
a clean, design-only `LOCAL_LIFECYCLE_DESIGN.md` and a phased
`LOCAL_LIFECYCLE_PLAN.md` (one PR per phase, with exit criteria + guardrails, no
code). No yacd code was changed; this was a design session. The work began from a
`deep-research` pass on `tilt-dev/ctlptl` (concluded: emulate its idempotent
declarative model, do **not** import it) and a prior decision to standardize on
k3d, then went through two adversarial multi-agent refinement workflows
(grounding/feasibility, then alternatives + 5 critics) and several user review
rounds. Execution is deferred to a new session.

## Key Decisions
- **Runtime = k3d** for end users; **KinD stays** for controller testing fidelity.
  (Recorded in auto-memory `cli-local-runtime-k3d`.)
- **All-in-one is a NEW verb `yacd devnet`** (takes no name); `up`/`down` are left
  untouched. Reason: CI/Chainsaw/the `yacd-env` Action select their cluster via an
  *ambient* `KUBECONFIG` (no `--context` flag), which the CLI cannot distinguish
  from a default at the flag layer — so conditional provisioning inside `up` would
  silently break CI. A separate verb makes provisioning explicit.
- **One singleton managed cluster** (`k3d-yacd`) hosting many networks; not
  cluster-per-network.
- **Kubeconfig:** switch the kubectl context (ecosystem convention) **and** track/
  pin yacd's own managed context for its verbs. Precedence: explicit flag/var >
  tracked managed context > ambient. The runtime (by fixed cluster name) is the
  source of truth; `$XDG_STATE_HOME/yacd` is a supplementary record + lock.
- **Operator install = pre-render the chart at build time → embed → server-side
  apply** (CRDs-first, label prune, version reconcile). Not the Helm SDK; not
  ctlptl/k3d-as-library (shell out to a pinned, checksum-verified k3d binary).
- **Hexagonal layout:** new ports `cluster`, `toolbin`, `operator`,
  `clusterstate` as light port packages with adapters as subpackages
  (`cluster/k3d`, `toolbin/ghrelease`, `operator/ssa`, `clusterstate/file`);
  `lifecycle.Manager` orchestrates; existing `kube.Client` is reused unchanged.
- **Funded wallet sequenced BEFORE the devnet phase** (operator-side, shipped in
  the embedded operator release `v0.2.0`) so `devnet` shows a real funded address
  from day one.
- **Chain data ephemeral only**; persistence (`--persist`/bind-mounts) out of scope.
- **Plan Phase 1 = cut operator `v0.1.0`** (chart + images) so the CLI has a
  reliable install target. **Documentation excluded** (a separate docs session).

## Changes
Journal artifacts only, on `journal/jmgilman` — no yacd code touched.
- `.journal/049/LOCAL_LIFECYCLE_DESIGN.md` — the design document (produced).
- `.journal/049/LOCAL_LIFECYCLE_PLAN.md` — the phased implementation plan (produced).
- `.journal/049/NOTES.md` — session log.
- `.journal/049/CLI_LIFECYCLE_DESIGN.md` — earlier verbose draft; refined into
  `LOCAL_LIFECYCLE_DESIGN.md`, then deleted.
- Auto-memory `cli-local-runtime-k3d` — the k3d-vs-KinD decision + rationale.

## Open Threads
- **Execution not started.** A new session begins the plan at Phase 1 (cut
  operator `v0.1.0`).
- **Precondition for Phase 1:** cut `v0.1.0` from a coherent operator state —
  coordinate with the in-flight F0 series (session 048, PR-B1) before releasing.
- **Documentation** for the lifecycle is deferred to a dedicated docs session.

## References
- Produced docs: `.journal/049/LOCAL_LIFECYCLE_DESIGN.md`,
  `.journal/049/LOCAL_LIFECYCLE_PLAN.md`.
- Prior: `.journal/030/SUMMARY.md` (test-harness design — the up/down/run/exec
  verb set the lifecycle builds on), `.journal/036/SUMMARY.md` &
  `.journal/041/SUMMARY.md` (CLI verbs + `YACD_*` contract),
  `.journal/008/SUMMARY.md` (CLI foundation).
- External: ctlptl https://github.com/tilt-dev/ctlptl ; k3d https://k3d.io .

## Lessons
- Adversarial multi-agent refinement earned its keep: it surfaced the load-bearing
  constraint that killed the obvious design (overloading `up`) — CI selects its
  cluster via an ambient `KUBECONFIG` env var that the CLI can't tell apart from a
  default, so "provision unless a flag is set" would break CI. Verify how existing
  consumers select state before changing an established verb's behavior.
