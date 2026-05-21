---
id: 005
title: Primary CardanoNetwork workload
date: 2026-05-21
status: complete
repos_touched: [yacd]
related_sessions: [003, 004]
---

## Goal
Continue phase 2 by turning a supported local-mode `CardanoNetwork` into the primary Kubernetes workload that can generate and run a local Cardano node environment.

## Outcome
The goal was met. PR #10 was squash-merged into `master` as `044d441` and the local default checkout was fast-forwarded. The controller now builds and applies an owned singleton primary node `Deployment` plus explicit owned PVC, protects localnet identity for the lifetime of the CR, handles core drift/conflict cases, and has envtest plus manual Kind/Tilt coverage.

## Key Decisions
- Use a package-local `primaryWorkloadBuilder` as the CR-to-workload construction boundary so Kubernetes object shape stays cohesive while `internal/cardano/localnet` remains Kubernetes-free.
- Move from StatefulSet `volumeClaimTemplates` to a singleton Deployment plus explicit PVC because stable ordinal DNS was not useful for the planned connection-publication model and explicit PVC ownership gives the operator clearer lifecycle control.
- Lock this phase to one primary node and reject local pool counts above one so storage and scaling semantics do not leak into the prototype before the single-node path is proven.
- Treat localnet inputs, including `spec.node.version`, as immutable once accepted. The PVC and CR status store the accepted fingerprint, and the controller rejects drift until the CR/PVC is deleted and recreated.
- Patch owned Deployment fields opportunistically instead of replacing the full template so API defaults and unrelated metadata do not create reconcile churn.

## Changes
- `internal/controller/cardanonetwork/workload_builder.go` - added the primary workload builder that returns an owned `Deployment` and PVC with bounded names, stable labels, localnet fingerprint annotations, security context, init container, node container, volumes, and validation.
- `internal/controller/cardanonetwork/apply.go` - added PVC and Deployment apply logic, owner/collision checks, localnet fingerprint protection, storage mutation guards, selector drift rejection, conflict requeue support, and merge-patch Deployment updates.
- `internal/controller/cardanonetwork/controller.go` and `status.go` - updated reconcile to build/apply children, own Deployments/PVCs, skip terminating parents, set initial `Degraded`/`Progressing` conditions, and filter parent watches to generation changes.
- `api/v1alpha1/cardanonetwork_types.go`, `charts/yacd/templates/rbac-manager.yaml`, and generated CRD manifests - added status/RBAC support needed for the controller-owned child resources.
- `Tiltfile` - expanded custom build dependencies so controller/API/internal Go changes rebuild the local manager image automatically.
- `internal/controller/cardanonetwork/*_test.go` - added builder, reconciler, and manager-backed envtest coverage for workload shape, identity immutability, storage rules, resource conflicts, owned child recreation, defaults, and patch behavior.
- `.session.md`, `AGENTS.md`, and `.journal/TECH_NOTES.md` - clarified that the dev stack starts once for an implementation session and shuts down at session close, not after every work item.

## Open Threads
- Add the Service and published connection details for the primary node.
- Add the Ogmios sidecar or companion workload in the next slice.
- Add runtime readiness/status conditions such as `Ready` and `NodeReady`; current status only means workload specs were applied.
- Decide the future multi-node model explicitly. This session intentionally kept one primary node and did not expose Deployment scaling.
- Consider API-server-side immutability later if controller-enforced localnet identity is not enough.

## References
- PR #10: https://github.com/meigma/yacd/pull/10
- Merge commit: `044d441d65052122ef162c55806c0cbacba2c0a1`
- Prior session 003: `.journal/003/SUMMARY.md`
- Prior session 004: `.journal/004/SUMMARY.md`
