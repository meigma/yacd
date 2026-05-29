---
id: 036
title: TEST_REPORT follow-through
started: 2026-05-29
---

## 2026-05-29 12:32 — Kickoff
Goal for the session: not yet stated by the user. `/session-new` was invoked to
prime the session; the specific request is pending. The standing campaign across
sessions 031–035 has been fixing `.journal/TEST_REPORT.md` findings one slice
per session, so the most likely next request is another TEST_REPORT
follow-through, but await the user's actual pick before starting implementation.

Current state of the world:
- `master` is at `dea708e` (`fix(controller): surface rejected PVC expansion in
  status`, PR #53). Local checkout clean.
- TEST_REPORT findings fixed so far: A3 (PR #49), A4 (PR #50), B1 (PR #51),
  B2 (PR #52), B6 (PR #53). Remaining open findings: D1, D2, D6, F0, F2/F4.
  Consult `.journal/TEST_REPORT.md` for concrete reproductions and suggested
  fixes before touching the relevant code paths.
- Session-startup note: the journal worktree carried an uncommitted session-035
  closeout (NOTES/INDEX/TECH_NOTES dirty, SUMMARY.md written on disk but never
  `git add -f`'d, so it was ignored and would have been lost on a clean
  checkout). Committed it as `122af54` (`docs(journal): close session 035`) and
  pushed before priming 036. A prior `wip: cleanup` (`9118e41`, already pushed)
  deleted some old 030 planning docs and the SNAPSHOT_* design files.
- Implementation worktree not yet created; `moon run root:dev-up` not yet run.
  Per `.session.md`, start the dev stack once after the implementation worktree
  is selected/created.

Plan: wait for the user's request. When it lands, create the implementation
worktree from fetched `master`, run `moon run root:dev-up`, load
`k8s-operator` and any other task-relevant skills, then implement.

## 2026-05-29 13:05 — Phase 0 (test harness) kicked off
Request: review and complete Phase 0 of `.journal/TEST_HARNESS_PLAN.md` (background
in `.journal/TEST_HARNESS_PROPOSAL.md`). Effort set to ultracode.

Journal-hygiene note: PLAN/PROPOSAL were deleted from `.journal/030/` by the
`wip: cleanup` commit and now exist UNTRACKED at `.journal/` root
(`git check-ignore` confirms `.gitignore:5:.journal/`); only
`.journal/030/TEST_HARNESS_DESIGN.md` remains tracked. Needs a decision at
close: force-add the root copies or restore the tracked 030 copies. Flagged to
user, not yet resolved.

Phase 0 is a de-risking/EVIDENCE phase, not feature code. Three deliverables:
(1) cold-start time-to-Ready for KinD+operator+representative localnet on a
standard hosted runner vs ~10–12m budget; (2) teardown completeness (delete
CardanoNetwork → all owned children GC'd); (3) host-access (run/exec/connect)
assumptions hold.

Recon workflow (6 agents) findings:
- ① UNKNOWN. e2e is `runInCI: false`; the Chainsaw smoke runs nowhere in CI, so
  zero historical cold-start evidence. Must measure fresh on `ubuntu-latest`.
  `.dev/scripts/test-e2e.sh` already encodes the kind+build+load+chainsaw path.
- ② Code-clean but unproven. All 11 child types carry controller ownerRefs
  (Deployment, PVC, 4 Services, faucet Secret, network-artifacts ConfigMap,
  artifact-publisher SA/Role/RoleBinding); no finalizers; no per-network
  cluster-scoped RBAC. Chainsaw only deletes the namespace, never asserts
  child GC after `delete cardanonetwork`.
- ③ Confirmed from code: all 4 chain-API containers (cardano-node, ogmios,
  kupo, faucet) co-locate in ONE primary Pod; all 4 Services select it on
  container-named ports (single port-forward serves run/connect); node socket
  `/ipc/node.socket` on shared EmptyDir mounted in cardano-node+ogmios
  (in-pod exec viable). All containers `PullIfNotPresent`.
- Budget flag: localnet sets NO cpu/mem requests (only mainnet does); with
  `Recreate` strategy an OOM evict wouldn't self-heal. Cold-start doubles as a
  schedulability test on 2 vCPU/7–8 GB.

Decisions (user): EVIDENCE-ONLY spike (throwaway branch, no permanent CI);
measure CardanoNetwork localnet ONLY (Ogmios+Kupo+faucet), no db-sync.
Deferred local `moon run root:dev-up` — evidence comes from the hosted runner,
not local iteration.

Did: created throwaway worktree `spike/phase0-ci-feasibility` (`.wt/...`) off
master; added `.github/workflows/phase0-feasibility.yml` (push-triggered,
ubuntu-latest, 35m cap) + `.dev/scripts/phase0-measure.sh` (times each
cold-start stage; verifies host-access via port-forward + in-pod
`cardano-cli query tip`; verifies teardown by deleting the CR and polling for
ownerRef'd children to hit 0). Committed `c7d1a7d`, pushed; CI run
`26659474962` in progress. Awaiting numbers, then write the go/no-go.
