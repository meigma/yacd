---
id: 051
title: Verify + revise TEST_PLAN.md
started: 2026-06-01
---

## 2026-06-01 16:41 — Kickoff
Goal for the session: not yet stated — session started via `session-new`,
awaiting the user's actual request.

Current state of the world:
- `master` at `bd8e0bf` (clean). The **F0 redesign series is COMPLETE** through
  PR-D (sessions 046–050): the runtime artifact data path is fully HTTP/PVC-based,
  the `<net>-network-artifacts` ConfigMap and `custom-public` profile are gone,
  the cardano-testnet artifact publisher and the cardano-tools `report` verb are
  deleted, and the manager's `cardano-tools` image default is digest-pinned to
  `11.0.1-yacd.5@sha256:d3283ca5…`. YACD now supports only local + curated-public
  (preview/preprod/mainnet).
- Open release-please PR **#7** (`yacd 1.0.0`) is intentionally left open awaiting
  a deliberate operator-release decision.
- Session **049** (yacd CLI all-in-one local cluster lifecycle / k3d design) is a
  separate concurrent session still `in-progress`.

Carried/open threads (none blocking):
- cardano-testnet digest-pin parity (optional, mirrors PR-D's cardano-tools work).
- Deterministic primary-sidecar manager-envtest refactor (the flaky test removed
  in 048).
- TEST_REPORT F2/F4 remain.
- Test-harness Phases 3 (release), 4 (`yacd-env` Action), 5 (examples + how-to).

Plan: wait for the user's request before doing substantive work.

## 2026-06-01 17:07 — Verified + revised TEST_PLAN.md (paused for review)
Task: an unverified agent dropped `.journal/051/TEST_PLAN.md` (185 requirement
rows, 20 categories). Asked to (1) verify all tests are legit/target real code,
(2) deduplicate, (3) notate each as full-E2E (chainsaw) vs quasi-mocked
(testenv), and produce a revised doc next to the original. Pause for review.

Method: ultracode workflow `verify-test-plan` (19 agents, ~1.57M tok). 15
category-group verifiers read the plan rows + real source in parallel; every
consequential flag was adversarially re-checked (refute-biased); a synthesis
pass clustered cross-category overlap.

Findings: the draft was strong — **175 legit / 9 minor-inaccuracy / 1
hallucinated** (CNL-07: local genesis tuning is NOT implemented — builder rejects
non-nil spec.local.genesis with UnsupportedSpec). Adversarial pass **overturned 2
false positives** (CNP-07, DBF-06). No F0-removed surface referenced (current
with master). 11 rows corrected (all values re-grounded against source: db-sync
13.7.1.0 / postgres 17.2-alpine defaults, exact reject strings, yacd.5 digest,
toolsimage split, etc.).

Type taxonomy decision: the user framed it as binary (chainsaw vs testenv), but
verification shows the right split is THREE tiers — **Unit 115 / Env 53 / E2E 17**
(primary). Forcing the 115 pure-unit rows (faucet HTTP, CLI flags, cardano-tools
verbs, public pins, Helm) into "testenv" would be wrong. Added a `Level` column
(E2E/Env/Unit, `(+X)` = thin secondary smoke) + a taxonomy section explaining it.

Dedup: 19 overlap clusters — MOST are intentional layered coverage (operator →
CLI → server), kept distinct + cross-referenced; only 2 genuine merges/re-scopes
applied (CNP-07→CNV-08, MGR-03↔MGR-04). Added a Consolidation & cross-references
section + a Known coverage gaps section (~10 untested sub-cases already implied
by in-scope rows).

Output: `.journal/051/TEST_PLAN_REVISED.md` (next to the original, original left
untouched). Validated: 185 rows, all 5-col tables, delimiters fixed, GFM pipe in
CNV-09 margin regex escaped. NOT committed — paused for user review.

Next: await review; on approval, decide whether to keep both files or replace
the original, and whether the gaps/cross-refs warrant follow-up test work.
