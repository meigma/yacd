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
