---
id: 005
title: Session 005
started: 2026-05-21
---

## 2026-05-21 09:42 — Kickoff
Goal for the session: start a new YACD journal session and wait for the user's actual implementation or research request.
Current state of the world: journal branch `journal/jmgilman` is clean and up to date. Sessions 002-004 closed the YACD foundation branding pass, introduced the first `CardanoNetwork` API/localnet planning path, and added the `cardano-testnet` tools image plus init-container fragment. The next likely implementation thread is the Kubernetes workload layer around localnet state, node container shape, StatefulSet, and Services, but no substantive work has been requested yet.
Plan: keep this session idle until the user gives the next task. For implementation work, select or create an implementation Worktrunk worktree first, run `moon run root:dev-up` from that worktree, and keep notes updated at meaningful checkpoints.

## 2026-05-21 09:51 — Workload builder proposal
Goal for the checkpoint: review the current phase 2 boundary and propose the shape of a new type that turns a `CardanoNetwork` into the primary node workload.
What was reviewed: `.journal/PLAN.md`, `.journal/TECH_NOTES.md`, `DESIGN.md`, the `CardanoNetwork` API, the read-only `CardanoNetworkReconciler`, `localnetSpecFromCardanoNetwork`, `localnet.BuildPlan`, and the existing `localnetCreateEnvInitContainer` helper.
Current proposal: keep `internal/cardano/localnet` as the pure `cardano-testnet create-env` planner, add a package-local builder in `internal/controller/cardanonetwork` as the single Kubernetes resource construction entrypoint, and have it return a small resource set with the StatefulSet first and Services/PVC/status wiring added as the prototype proves each piece.

## 2026-05-21 09:57 — Adapter folded into builder
Decision update: the CRD-to-`localnet.Spec` adapter should not remain as a standalone `localnet_adapter.go` helper. It is small enough to become a method on the new primary workload builder, keeping the full CRD-to-workload path inside one cohesive type while preserving `internal/cardano/localnet` as the Kubernetes-free planning package.

## 2026-05-21 10:06 — Implementation start
Started implementation on Worktrunk branch `feat/primary-statefulset-builder` at `.wt/feat-primary-statefulset-builder`.
The required local dev stack was started with `moon run root:dev-up` from the implementation worktree. It created/connected the `kind-yacd-dev` cluster and reported the YACD dev stack ready with Tilt UI on port 10350.

## 2026-05-21 10:14 — StatefulSet builder implemented
Implemented the primary StatefulSet builder on `feat/primary-statefulset-builder` and committed it as `2a86a61` (`feat(cardanonetwork): build primary node statefulset`).
The slice folds `localnet_adapter.go` into `primaryWorkloadBuilder`, returns a StatefulSet without Ogmios or apply/status side effects, keeps the reconciler read-only, and adds focused builder coverage for validation, owner references, init container, node container, PVC, IPC volume, labels, fingerprint annotation, resources, and security context.
Validation passed with `go test ./internal/controller/cardanonetwork`, `moon run root:test`, `git diff --check`, and `moon run root:check`. The local dev stack was shut down with `moon run root:dev-down`, which reported `YACD dev stack is down`.

## 2026-05-21 10:18 — Filename cleanup
Renamed the CardanoNetwork controller files on `feat/primary-statefulset-builder` to shorter package-local names: `controller.go`, `controller_test.go`, `init_container.go`, `init_container_test.go`, `workload_builder.go`, and `workload_builder_test.go`.
Committed the rename-only cleanup as `310c3a3` (`refactor(cardanonetwork): shorten controller filenames`). Validation passed with `go test ./internal/controller/cardanonetwork`, `moon run root:test`, and `git diff --check`.

## 2026-05-21 10:35 — Review fixes
Applied the review feedback that was worth addressing inside the current StatefulSet-only boundary on `feat/primary-statefulset-builder`.
Commit `61bca07` (`fix(cardanonetwork): harden primary workload metadata`) disables pod ServiceAccount token automounting, derives bounded child names and selector labels with a stable hash for long CR names, and sets explicit PVC retention (`WhenDeleted: Delete`, `WhenScaled: Retain`).
Validation passed with `go test ./internal/controller/cardanonetwork`, `moon run root:test`, `git diff --check`, and `moon run root:check`. The local dev stack was started before implementation and shut down afterward with `moon run root:dev-down`.

## 2026-05-21 10:40 — Dev stack lifecycle correction
Clarified the intended dev stack lifecycle after the user pointed out that start/stop per work item wastes local resources.
Commit `89d3e3b` (`docs(session): clarify dev stack lifecycle`) updates `.session.md` and `AGENTS.md` on `feat/primary-statefulset-builder` so `root:dev-up` is a one-time implementation-session startup step and `root:dev-down` is reserved for explicit session close/end-of-session, user request, or stack repair/cleanup.
Also corrected `.journal/TECH_NOTES.md` so future session startup context does not reintroduce the stale "stop before pause" guidance. Validation passed with `git diff --check` in both the feature and journal worktrees. The dev stack was intentionally not started for this docs-only correction.

## 2026-05-21 10:45 — PR opened and CI verified
Pushed `feat/primary-statefulset-builder` and opened draft PR #10: https://github.com/meigma/yacd/pull/10.
GitHub checks completed successfully for the branch head `89d3e3b`: `ci` passed and `Kusari Inspector` passed. Release dry-run jobs were reported as skipped for this PR.

## 2026-05-21 11:36 — Deployment and explicit PVC refactor
Implemented the next primary workload slice on `feat/primary-statefulset-builder` and committed it as `e861145` (`refactor(cardanonetwork): run primary node as deployment`).
The builder now returns an owned singleton `Deployment` plus explicit owned PVC instead of a StatefulSet with `volumeClaimTemplates`; the reconciler applies the PVC first, then the Deployment, owns both child types, and writes minimal `Progressing`/`Degraded` status conditions.
PVC updates allow storage expansion, reject shrink and storage class drift without destructive recreation, and Deployment updates reject selector drift while patching the pod template. Validation passed with `moon run root:generate`, `moon run root:test`, `moon run root:check`, and `git diff --check`. The dev stack was started once for the implementation session and intentionally left running.

## 2026-05-21 11:37 — PR updated and CI verified
Pushed `e861145` to draft PR #10.
GitHub checks completed successfully on the new head: `ci` passed and `Kusari Inspector` passed. Release dry-run jobs were skipped for the draft PR.

## 2026-05-21 12:15 — Localnet identity protected
Implemented controller-side protection for stable `CardanoNetwork` localnet identity on `feat/primary-statefulset-builder` and committed it as `6a4dd87` (`feat(cardanonetwork): protect localnet identity`).
The owned PVC now stores the accepted localnet fingerprint, the Deployment pod template keeps the same fingerprint, and PVC apply rejects fingerprint drift before storage or Deployment mutations. Drifted localnet inputs set `Degraded=True` and `Progressing=False` with `UnsupportedLocalnetChange`; unannotated existing PVCs set `MissingLocalnetFingerprint`.
Validation passed with `moon run root:test`, `moon run root:check`, and `git diff --check`. Pushed the branch to draft PR #10, where `ci` and `Kusari Inspector` passed on head `6a4dd87`; release dry-run jobs were skipped. The dev stack remains running as intended for the ongoing implementation session.

## 2026-05-21 12:47 — Review collision fixes
Addressed the review findings worth fixing on `feat/primary-statefulset-builder` and committed them as `09c464a` (`fix(cardanonetwork): guard primary child identity`).
The reconciler now refuses same-name PVC/Deployment collisions unless the existing child is already controlled by the current `CardanoNetwork` UID, persists the accepted localnet fingerprint in CR status so deleted PVCs cannot reset network identity, and annotates PVCs with the originally requested storage class so explicit storage class removal/drift is rejected while cluster defaulting remains tolerated.
Validation passed with `moon run root:generate`, `moon run root:test`, `moon run root:check`, `git diff --check`, and `git diff --cached --check`. Pushed the branch to draft PR #10, where `ci` and `Kusari Inspector` passed on head `09c464a`; release dry-run jobs were skipped. The dev stack remains running for the active implementation session.

## 2026-05-21 13:06 — Primary reconciliation tightened
Applied the next review fixes on `feat/primary-statefulset-builder` and committed them as `92424b8` (`fix(cardanonetwork): tighten primary reconciliation`).
PVC and Deployment metadata updates now preserve unrelated labels/annotations while patching only YACD-owned keys, owned Deployments have `spec.paused` corrected back to the desired value, sanitized child names include a short hash when sanitization changes the parent name to avoid `foo.bar`/`foo-bar` collisions, and builder errors now distinguish user-facing unsupported specs from internal controller failures.
Validation passed with focused reconciler/builder tests, `moon run root:test`, `moon run root:check`, `git diff --check`, and `git diff --cached --check`. Pushed the branch to draft PR #10, where `ci` and `Kusari Inspector` passed on head `92424b8`; release dry-run jobs were skipped. The dev stack remains running for the active implementation session.
