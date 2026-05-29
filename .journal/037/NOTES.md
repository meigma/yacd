---
id: 037
title: TEST_REPORT issue follow-through
started: 2026-05-29
---

## 2026-05-29 12:38 — Kickoff
Goal for the session: Continue fixing issues found in `.journal/TEST_REPORT.md`.
Current state of the world: Recent sessions fixed A3, A4, B1, B2, and B6; remaining concrete findings are D1, D2, D6, F0, and F2/F4. The journal already had session 036 in progress, so this new session was assigned ID 037 per the incrementing session protocol.
Plan: Prime the journal only, then wait for the user's specific next issue or implementation request before touching code.

## 2026-05-29 12:59 — D1 implementation start
Goal for the checkpoint: Implement the approved D1 faucet auth Secret recovery plan.
Current state of the world: Created implementation worktree `feat/d1-faucet-auth-recovery` at `/Users/josh/code/meigma/yacd/.wt/feat-d1-faucet-auth-recovery`; ran `direnv allow`; `moon run root:dev-up` completed successfully and left Tilt running on port 10350.
Plan: Add deterministic faucet auth token hashing to the primary Deployment template, wire owned faucet Secret events into CardanoNetwork reconciliation, add unit/envtest coverage, then run native validation plus the manual D1 proof.

## 2026-05-29 13:15 — D1 implementation validated
Goal for the checkpoint: Record the completed D1 implementation and validation evidence.
Current state of the world: Implementation branch `feat/d1-faucet-auth-recovery` now reconciles owned faucet auth Secret create/update/delete events, stamps `yacd.meigma.io/faucet-auth-token-hash` onto the primary Deployment pod template from live Secret data, and removes the annotation when faucet is disabled.
Validation: `moon run root:test`, `moon run root:check`, and `git diff --check` passed. Manual Kind proof on `yacd-smoke/phase4-smoke` deleted `phase4-smoke-faucet-auth`; the Secret was recreated with a new UID, the token hash changed from `sha256:4d88501e6fb1b022148e8493a1f263a1ae3a307a7d7e97a5b1c9a0197f0526a8` to `sha256:09693f5b261007f15d9827c186846221dcdfa5cc7367107cc4e40e0ba95e639c`, `Ready` and `FaucetReady` reported `False:DeploymentProgressing` during rollout, the primary pod UID changed from `9349b229-1503-4cab-8003-7d0146bd66ce` to `1a8b9e6c-717b-4bf8-8728-49a2673168e7`, the old token returned HTTP 401, and the new token passed auth before failing validation with HTTP 400.

## 2026-05-29 13:30 — Close
PR #54 (`fix(controller): reconcile faucet auth secret recovery`) merged via squash to `master` at `3754baedc6fd96332cb102a9e142ee1c669a0202`. Local `master` was fast-forwarded, `moon run root:dev-down` shut down the Kind/Tilt dev stack, and Worktrunk removed the `feat/d1-faucet-auth-recovery` worktree. Session 037 is complete; D2, D6, F0, and F2/F4 remain open TEST_REPORT follow-ups.
