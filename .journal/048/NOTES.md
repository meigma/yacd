---
id: 048
title: F0 redesign — PR-B1 (remove network-artifacts ConfigMap + custom-public)
started: 2026-06-01
---

## 2026-06-01 07:11 — Kickoff

Goal for the session: pick up from session 047 and execute **PR-B1** of the F0
redesign — remove the `<net>-network-artifacts` ConfigMap concept and
custom-public entirely, repoint curated-public node + ogmios to read
`/state/artifacts` from the PVC, re-source the three ConfigMap-coupled signals
(`ArtifactsReady`, sync-timing, db-sync identity) onto single-path/served data,
and make db-sync single-path serve. This is the F0 mainnet unblock (mainnet
renders with zero ConfigMap). API-breaking but pre-1.0 and intended.

Current state of the world:
- master is at `231ccde` (PR #77, PR-C merged). F0 PR-A (#75) + PR-C (#77) are
  done+merged. PR-B1 is fully planned and APPROVED; the durable plan lives at
  `.journal/047/PR-B1-PLAN.md`.
- The prior PR-B WIP branch (`feat/f0-delete-network-configmap`) was discarded
  clean (no commits). PR-B1 should branch fresh off master.
- PR-B1 is a large (~23-file), compile-coupled change with no intermediate
  checkpoint per the plan: do the whole spine, then compile.
- Remaining after PR-B1: PR-B2 (delete publisher binary/module + Dockerfile
  stage + new cardano-testnet image) and PR-D (remove `report` verb, pin manager
  cardano-tools image to a published digest, DESIGN.md, drop e2e build+load
  hack). Carried: TEST_REPORT F2/F4; test-harness Phases 3-5.
- Dev stack: per session 046 notes the stack may have been left UP/orphaned on
  `kind-yacd-dev`; verify/clean before `dev-up` for this session's
  implementation worktree.

Plan: per `.journal/047/PR-B1-PLAN.md`. Awaiting user go-ahead before starting
implementation (user asked to review the plan and confirm readiness first).
