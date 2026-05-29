---
id: 038
title: TEST_REPORT continuation
started: 2026-05-29
---

## 2026-05-29 13:47 — Kickoff
Goal for the session: Continue fixing issues found in `.journal/TEST_REPORT.md`; the user asked for a new session and to be told when ready to continue.
Current state of the world: The journal branch is current. Recent closed sessions fixed B2, B6, and D1; durable notes say A3, A4, B1, B2, B6, and D1 are fixed, with D2, D6, F0, and F2/F4 still remaining. Session 036 is still listed as in-progress and was left untouched.
Plan: Wait for the user's actual implementation request before reading or editing `.journal/TEST_REPORT.md` or repository code. No implementation worktree or dev stack has been selected or started for this session yet.

## 2026-05-29 13:58 — D2 implementation start
Goal for this checkpoint: Implement the approved D2 primary PVC deletion honesty plan.
Current state of the world: Created implementation branch `feat/d2-primary-pvc-deletion` at `/Users/josh/code/meigma/yacd/.wt/feat-d2-primary-pvc-deletion`. The unrelated `fix/manager-build-embed` worktree is dirty and will be left untouched. `moon run root:dev-up` succeeded from the D2 worktree; the Kind/Tilt dev stack is ready and Tilt is available on port 10350.
Plan: Add terminating-child and create-gate hooks to `ctrlkit/apply`, surface `ChildBeingDeleted` in both controllers, refuse primary PVC recreation after accepted CardanoNetwork identity, then validate with focused tests, broad checks, and the D2 manual Kind proof.

## 2026-05-29 14:20 — D2 validation
Goal for this checkpoint: Record the completed implementation and validation proof for D2.
Current state of the world: The D2 branch adds `ValidateCreate` and `ObjectDeleting` hooks to `ctrlkit/apply`, a shared owned-child deletion status helper, `ChildBeingDeleted` in both controllers, and `PrimaryStateLost` refusal for CardanoNetwork primary PVC loss after accepted runtime identity. Focused tests passed for `internal/ctrlkit/apply`, `internal/controller/children`, `internal/controller/cardanonetwork`, and `internal/controller/cardanodbsync`; `moon run root:test --summary minimal`, `moon run root:check --summary minimal`, and `git diff --check` all passed.
Manual proof: The local example reached Ready in namespace `yacd-smoke`. Holding primary PVC `phase4-smoke-node-state` deletion with finalizer `test.example.io/never-removed` produced `Degraded=True`, `Ready=False`, and `NodeReady=False` with reason `ChildBeingDeleted` and a message naming the PVC/finalizer. Removing the finalizer let the PVC disappear, after which the controller reported `PrimaryStateLost` and did not recreate the PVC. Deleting and redeploying only the `CardanoNetwork` created fresh state: old PVC UID `4731c727-e2d8-4353-ab9a-0527a6889612`, new PVC UID `95c76d1a-1482-4ed0-ac89-3b1fe9f40a17`, and the network returned to Ready.
Plan: Commit the implementation branch and leave the dev stack running for review unless the user asks to close the session.
