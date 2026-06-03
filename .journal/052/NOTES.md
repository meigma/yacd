---
id: 052
title: New session
started: 2026-06-01
---

## 2026-06-01 17:29 — Kickoff
Goal for the session: not yet stated — session primed via `session-new`,
awaiting the user's request.

Current state of the world:
- `master` is clean at `bd8e0bf` (build(cardanonetwork): pin cardano-tools image
  to a published digest, F0 PR-D).
- The **F0 redesign series is COMPLETE** (PR-A #75, PR-C #77, PR-B1 #78,
  PR-B2 #79, PR-D #81+#82). The runtime artifact data path is fully HTTP/PVC
  based; the artifact-ConfigMap concept, `custom-public` profile, and the
  cardano-testnet artifact publisher are all gone. The manager's cardano-tools
  default is digest-pinned to `11.0.1-yacd.5@sha256:d3283ca…`.
- Open / carried threads: root release PR #7 (`yacd 1.0.0`) is open awaiting a
  deliberate operator-release decision; cardano-testnet digest-pin parity
  (optional); the deterministic primary-sidecar manager-envtest refactor (flaky
  test removed in 048); TEST_REPORT F2/F4; test-harness Phases 3–5.
- Concurrent in-progress sessions left untouched: **049** (yacd CLI all-in-one
  local cluster lifecycle / k3d design) and **051** (started, awaiting request).

Plan: wait for the user's stated goal, then survey relevant skills and prime
the implementation worktree / dev stack as needed.

## 2026-06-02 — Docs structure proposal (Diátaxis + Material MkDocs)
Goal (now stated): produce a local proposal document proposing a docs structure
for yacd using the `diataxis` skill and the MkDocs framework. Ultracode on; plan
mode used.

What I did:
- Loaded the `diataxis` skill. Ran 3 Explore agents against the `master` checkout
  to map: the CLI surface (11 verbs + global flags + the `YACD_*` contract + the
  devconfig Environment format), the CRD/API surface (CardanoNetwork +
  CardanoDBSync spec/status/conditions) and the `examples/` inventory, and the
  install/distribution story (OCI Helm chart, goreleaser CLI binaries, manager
  flags).
- Confirmed Material navigation config against current Material for MkDocs docs
  via context7.
- Asked the user two setup decisions: theme = **Material for MkDocs**; hosting =
  **GitHub Pages via CI**.
- Ran a 2-lens review workflow (Diátaxis/completeness vs consolidation/pragmatism,
  Plan agents) against a draft IA. Both lenses converged: drop the two audience
  landing pages, keep Reference generic, single-source the `YACD_*` table,
  consolidate manifests into Recipes. Completeness lens added two gaps worth
  filling: a Troubleshooting how-to and a CLI-install owner; argued Security
  deserves its own explanation page.
- Synthesized a 17-page IA (audience-grouped Developer/Operator guides over
  tutorials+how-tos; generic Reference/Recipes/Concepts; collapsing left sidebar).

Gotcha caught: the consolidation Plan agent ran with CWD = the journal worktree
(`.wt/journal-jmgilman`, on the stale `journal/jmgilman` branch which is ~77
behind master and still the old template-k8s code), so it falsely reported "the
yacd code doesn't exist yet / repo is still the template." Disregarded — the
Explore agents read the real `master` checkout. Lesson: workflow/Plan agents
inherit the journal-worktree CWD during a session-new flow; point repo-reading
agents at the absolute `master` path, and any actual docs/code work must happen
in an implementation worktree off master, not the journal worktree.

Deliverable: `.journal/052/DOCS_PROPOSAL.md` (the proposal). Plan file:
`~/.claude/plans/for-this-session-i-squishy-crab.md` (approved).

Next: await user review of the proposal. If approved, the build is a separate
implementation session (worktree off master) following the phased plan in the
proposal; no dev stack needed for docs work.
