---
id: 023
title: cli package refactor (readability + hexagonal + mockery)
started: 2026-05-26
---

## 2026-05-26 14:23 — Kickoff
Goal for the session: not yet stated; waiting on user request.
Current state of the world:
- `master` at `b131069` (session 022 / PR #40 merged), tree clean.
- Last three closed sessions: 020 (ctrlkit pass, PR #37), 021 (cardanonetwork
  controller refactor, PRs #38 + #39), 022 (cardanodbsync controller refactor,
  PR #40). The multi-package readability/maintainability/architectural-purity
  sweep that started in 018 has now touched the two main controller packages
  plus ctrlkit.
- Journal worktree `journal/jmgilman` checked out clean, up to date with
  origin; main-branch divergence is normal (`.journal/` is journal-branch-only).
- Local dev stack is not running; bring it up only after an implementation
  worktree is selected.
- Open threads from prior sessions (per session 022 SUMMARY): five rejected
  architectural items in cardanodbsync stand as separable decisions
  (no `KubernetesClient` port, no `ReadinessProber` port, no `runtimeProber`
  split into Postgres+Ogmios, no subpackage adapter, no mockery introduction).
  Pre-existing INDEX.md gap for session 016 still present.
Plan: wait for user request.

## 2026-05-26 15:05 — Scope locked + implementation worktree up
Goal: apply the session 020/021/022 readability + maintainability + hexagonal
purity rubric to every package under `cli/`. Plan written to
`/Users/josh/.claude/plans/we-re-going-to-do-crystalline-curry.md` and
approved. Single PR, no behavior change.

Confirmed scope decisions:
- Kubernetes port stays in `cli/internal/kube/` alongside its adapter; the
  rule-7 fix is to make `NewClient` return the concrete `*Adapter` and rename
  `runtimeClient → Adapter`.
- Mockery + Testify get introduced for the first time in this repo. `kube.Client`
  and the (now-exported) `cli.HTTPDoer` are the two mocked seams. The hand-rolled
  `fakeKubeClient` and `fakeHTTPClient` are migrated to generated mocks.
- Single PR. Typed condition vocabulary cascades through every file; splitting
  would create non-compiling intermediate states (precedent: session 022).

Audit input came from three parallel Explore agents (architectural purity,
naming/file organization, godoc hygiene) plus my own first-hand reads. Plan
explicitly rejects: renaming `FreshCondition`/`Options`/`RuntimeConfig`,
unexporting `Environment.Metadata`/`Spec`, typed `LogLevel`/`LogFormat` enums,
replacing function-typed `KubeClientFactory`/`KubeNamespaceResolver` with
typed interface ports.

State of the world:
- Implementation worktree: `.wt/refactor-cli-packages` on branch
  `refactor/cli-packages`, branched from `master@b131069`.
- Dev stack up via `moon run root:dev-up` (47s); Kind context `kind-yacd-dev`,
  Tilt UI on http://localhost:10350/, logs at `.run/yacd-dev/tilt.log` (under
  the primary checkout per the singleton-runtime contract).
- Task list seeded with 13 tasks covering the eight plan deliverables plus
  verification + PR.
