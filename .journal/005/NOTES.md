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
