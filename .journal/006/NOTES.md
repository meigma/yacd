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

## 2026-05-21 15:15 — Primary Service implementation start
Goal for the checkpoint: implement phase-2 remaining-work item 1 by adding the primary node Service only.
Current state of the world: created Worktrunk implementation branch `feat/cardanonetwork-primary-service` at `/Users/josh/code/meigma/yacd/.wt/feat-cardanonetwork-primary-service` from current `master`. Ran `moon run root:dev-up` from that worktree; it created/connected the Kind/Tilt dev stack and reported the controller ready with Tilt UI at `http://localhost:10350/`.
Plan: add an owned ClusterIP Service and named node container port, wire Service ownership/RBAC/apply behavior, extend builder/reconciler/envtest coverage, then run generation/check/test/diff verification.

## 2026-05-21 15:20 — Primary Service implemented
Goal for the checkpoint: finish phase-2 remaining-work item 1.
Current state of the world: branch `feat/cardanonetwork-primary-service` now has commit `fa9f9db` (`feat(cardanonetwork): expose primary node service`). The controller builds an owned ClusterIP Service named with the primary workload name, exposes `node-to-node` TCP through a named cardano-node container port, reconciles Service drift while preserving foreign metadata and assigned cluster IP fields, watches owned Services, and updates manager RBAC for Services. This completes the first item from the phase-2 remaining-work list.
Validation: ran `moon run root:generate`, focused `KUBEBUILDER_ASSETS="$(setup-envtest use 1.35.x -p path)" go test ./internal/controller/cardanonetwork`, `moon run root:check`, `moon run root:test`, and `git diff --check`; all passed. The Kind/Tilt dev stack remains running for the active session.
Plan: next phase-2 item remains endpoint publication in `status.endpoints.nodeToNode`.

## 2026-05-21 16:35 — Node endpoint status implemented
Goal for the checkpoint: finish phase-2 remaining-work item 2 by publishing primary node connection info in `status.endpoints.nodeToNode`.
Current state of the world: branch `feat/cardanonetwork-primary-service` now has commit `4ab784d` (`feat(cardanonetwork): publish node endpoint status`) on top of the primary Service commit. Successful reconciles publish `serviceName`, `port`, and a fully qualified `tcp://<service>.<namespace>.svc.cluster.local:<port>` URL for the node-to-node Service. Ogmios status remains unset, no ConfigMap publication was added, and readiness conditions were left for the next slice.
Validation: ran focused `KUBEBUILDER_ASSETS="$(setup-envtest use 1.35.x -p path)" go test ./internal/controller/cardanonetwork`, `moon run root:generate`, `moon run root:check`, `moon run root:test`, and `git diff --check`; all passed. The Kind/Tilt dev stack remains running for the active session.
Plan: next phase-2 item is runtime-derived readiness conditions.

## 2026-05-21 16:50 — Runtime node readiness implemented
Goal for the checkpoint: finish phase-2 remaining-work item 3 by deriving `NodeReady` from Kubernetes runtime state.
Current state of the world: branch `feat/cardanonetwork-primary-service` now has commit `8289693` (`feat(cardanonetwork): derive node readiness from deployment`) on top of the Service and endpoint-status commits. Successful reconciles now set `NodeReady=False` and `Progressing=True` while the owned Deployment is still unobserved or unavailable, then transition to `NodeReady=True` and `Progressing=False` once the owned PVC and Service exist and the Deployment has observed the current generation with ready/available replicas and `Available=True`. Unsupported specs and resource conflicts now also set `NodeReady=False` so a previous ready state is not preserved across blocked reconciles. `Degraded=False` remains tied to successful apply rather than availability lag, and aggregate `Ready` remains intentionally unset.
Validation: ran focused `KUBEBUILDER_ASSETS="$(setup-envtest use 1.35.x -p path)" go test ./internal/controller/cardanonetwork`, `moon run root:generate`, `moon run root:check`, `moon run root:test`, and `git diff --check`; all passed. After a final comment-only cleanup, reran `git diff --check`; it passed. The Kind/Tilt dev stack remains running for the active session.
Plan: next phase-2 item is a Kind/Tilt or Chainsaw smoke that applies a representative `CardanoNetwork` and proves the installed operator creates the primary resources and reaches a running pod, with protocol query proof recorded if it is practical.
