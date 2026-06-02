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

## 2026-06-01 17:48 — Coverage audit complete + plan made canonical
User approved the revised plan. Actions: deleted the old TEST_PLAN.md, promoted
TEST_PLAN_REVISED.md → canonical `.journal/051/TEST_PLAN.md` (185 rows).

Then ran ultracode workflow `audit-test-coverage` (148 agents, ~7.5M tok, 22m):
15 group auditors graded each requirement against EXISTING tests
(satisfied/partial/not-satisfied), every satisfied+not-satisfied verdict
adversarially re-checked (skeptic pokes holes in "satisfied", hunts coverage for
"not-satisfied"), then a synthesis pass.

Result — **74 satisfied (40%) / 86 partial (46%) / 25 not-satisfied (14%)**.
Strong at unit tiers (CLI/HST/TOP, FCT/FTX, TLS/PIN, Kong opts); thin at the two
apiserver tiers. THREE structural weaknesses:
  1. **CardanoNetwork CRD admission is entirely unguarded** — there is NO
     api/v1alpha1/cardanonetwork_validation_test.go at all (CardanoDBSync HAS one).
     Whole CNV cluster (mode XOR, enum closure, port range, margin pattern,
     defaulting) unverified at apiserver. Highest-leverage gap.
  2. **Reconcile-output contracts proven only by fake-client unit tests where the
     plan wants Env** (DBS-01/02/03/05/06, DBD-02, CNI-02/03/04, CNL-06...). The
     primary-sidecar happy-path manager envtest REMOVED in 048 was never replaced.
  3. **E2E is a single manager-smoke suite** — never drives CLI down/run/exec/
     connect, never asserts GC cascade (CNL-10/DBF-09/CLI-09), never sends an
     unauth metrics request (MGR-07), no chart-render guard (HLM-02/03).
Per-group: CNL+CNP+API strongest (14S/11P/0N); CNV+CNI weakest (0S/8P/7N).

Output: `.journal/051/TEST_COVERAGE_ANALYSIS.md` — exec summary, 7-item
prioritized roadmap, quick wins, biggest holes, level-mismatch themes, + a full
185-row per-category appendix (ID | Scenario | Status | Covering tests | Gap).

Committed journal artifacts (plan + analysis + this checkpoint). Next: await user
direction — likely candidates are closing gap cluster #1 (cardanonetwork
validation envtest) and/or the low-effort unit quick-wins.
