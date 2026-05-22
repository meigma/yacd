---
id: 006
title: Primary node service, status, and readiness
date: 2026-05-21
status: complete
repos_touched: [yacd]
related_sessions: [005]
---

## Goal
Assess what was realistically left in `.journal/PLAN.md` phase 2, preserve that assessment for future work, and finish the primary node runtime gaps that could be handled in one narrow implementation branch.

## Outcome
The runtime portion of phase 2 was completed and merged in PR #11. The operator now exposes the singleton primary `cardano-node` through an owned ClusterIP Service, publishes node-to-node connection details in `CardanoNetwork` status, derives `NodeReady` from Kubernetes runtime state, and proves the installed operator path with a Kind/Chainsaw smoke. The only phase-2 item intentionally left open is current-state documentation drift, especially README text that still describes the reconciler as future work.

## Key Decisions
- Keep status as the canonical discovery contract instead of adding a ConfigMap, because other controllers and future CLI/status consumers can read one API surface directly.
- Treat `Degraded=False` as successful reconcile/apply, not Deployment availability, because normal rollout delay is progress rather than degradation.
- Add `NodeReady` but not aggregate `Ready`, because future Ogmios and additional endpoints need their own readiness semantics before an aggregate condition is useful.
- Keep the installed-operator smoke at Kubernetes workload readiness only, because protocol-level `cardano-cli` or socket health is still a separate contract.

## Changes
- `internal/controller/cardanonetwork` - added primary Service reconciliation, named node-to-node container port, endpoint status publication, and runtime-derived `NodeReady`/`Progressing` behavior.
- `charts/yacd/templates/rbac-manager.yaml` - added Service RBAC needed by the packaged controller.
- `test/chainsaw/manager-smoke/chainsaw-test.yaml` - extended the installed-operator smoke to apply a representative `CardanoNetwork`, wait for primary workload readiness, and assert endpoint/status conditions.
- Controller and builder tests - covered Service creation/drift/collisions, endpoint status mutation, readiness transitions, and manager-backed owned-child watch behavior.

## Open Threads
- Refresh README/current-state docs so they no longer say the first reconciler will land later.
- Protocol-level node health remains future work; the smoke proves Kubernetes readiness, not Cardano query/block-production health.
- Ogmios, CLI/status UX, aggregate `Ready`, and supporting services remain later phases.

## References
- PR #11: https://github.com/meigma/yacd/pull/11
- Merge commit: `c415f3e` (`feat(cardanonetwork): publish primary node readiness (#11)`)
- Prior session: `.journal/005/SUMMARY.md`
