---
id: 006
title: New YACD session
started: 2026-05-21
---

## 2026-05-21 14:26 — Kickoff
Goal for the session: start a fresh YACD journal session and wait for the concrete implementation or research request.
Current state of the world: the personal journal worktree for `journal/jmgilman` is present, clean, and up to date. Recent closed work landed the local-mode `CardanoNetwork` primary node workload as an owned singleton Deployment plus explicit PVC on `master` at `044d441`.
Plan: keep this session open for the next user request; before implementation work, select or create the implementation Worktrunk worktree and run `moon run root:dev-up` from that worktree unless the human waives the session startup step.

## 2026-05-21 14:31 — Phase 2 assessment
Goal for the checkpoint: assess what is realistically left before `.journal/PLAN.md` phase 2 can be called complete.
Current state of the world: phase 2 already has the `CardanoNetwork` CRD, local-mode planner, init-container fragment, primary node Deployment, explicit PVC, identity/storage drift protection, generated CRDs, envtest coverage, and prior manual Kind/Tilt smoke. The remaining phase-2 gaps are the primary node Service, endpoint publication in status, runtime-derived readiness conditions, and a cluster proof that a created `CardanoNetwork` reaches a running/queryable node.
Plan: propose a narrow completion slice rather than expanding into phase 3 Ogmios, phase 4 CLI, or broader docs hardening.

## 2026-05-21 14:35 — Phase 2 remaining-work list
Goal for the checkpoint: preserve the phase-2 completion assessment so it can be copied into `SUMMARY.md` at session close.
Current state of the world: the realistic remaining phase-2 work is:

- Add the primary node Service, exposing node-to-node TCP for the primary node using the existing selector labels and `spec.node.port`.
- Publish node connection info in `status.endpoints.nodeToNode`, using the existing endpoint status API shape.
- Add runtime-derived readiness conditions. The current `Degraded=False`/`Progressing=False` state only means PVC and Deployment specs were applied; phase 2 needs at least pragmatic `NodeReady` or `Ready` status from Deployment availability plus Service existence, without blocking on Cardano protocol-level health.
- Add a Kind/Tilt or Chainsaw smoke that applies a representative `CardanoNetwork` and proves the installed operator creates the primary resources and reaches a running pod; if full protocol query is too slow or flaky, record the limitation explicitly.
- Clean current-state docs drift, especially README text that still says the first reconciler will land later.

Plan: include this list in the session closeout summary, preferably under Open Threads or Next Steps, unless later implementation in this session completes some of these items.
