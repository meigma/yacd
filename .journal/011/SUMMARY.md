---
id: 011
title: Phase 6 db-sync supporting service
date: 2026-05-23
status: complete
repos_touched: [yacd]
related_sessions: ["009", "010"]
---

## Goal
Start phase 6 by defining the first db-sync supporting-service CRD shape. Keep the slice limited to Kubernetes API surface and generated artifacts so the controller/runtime design can follow the prototype path.

## Outcome
The goal was met. PR #17 added the namespaced `CardanoDBSync` CRD, generated API artifacts, scheme registration, and a lightweight API registration test, then merged cleanly after CI and review.

## Key Decisions
- Keep this as an API-only slice -> avoids locking controller, workload, RBAC, CLI, or devconfig behavior before the managed db-sync prototype proves the runtime path.
- Use `spec.networkRef.name` as the only network reference -> the db-sync resource owns the follower node, and the future controller will derive follower-node join material from the same-namespace `CardanoNetwork`.
- Flatten managed Postgres under `spec.database.*` -> matches the reviewed public API shape and defers external database support until after the local managed prototype.
- Use small local reference structs instead of `corev1.LocalObjectReference` for db-sync references -> the CRD now requires non-empty `networkRef.name` and, when present, `authSecretRef.name`.

## Changes
- `api/v1alpha1/cardanodbsync_types.go` - added `CardanoDBSync`, `CardanoDBSyncList`, typed db-sync spec/config/status structs, validation markers, defaults, print columns, and status subresource markers.
- `api/v1alpha1/groupversion_info.go` - registered `CardanoDBSync` and `CardanoDBSyncList` in the v1alpha1 scheme.
- `api/v1alpha1/groupversion_info_test.go` - added scheme registration coverage for `CardanoNetwork`, `CardanoNetworkList`, `CardanoDBSync`, and `CardanoDBSyncList`.
- `api/v1alpha1/zz_generated.deepcopy.go` - regenerated deepcopy output.
- `charts/yacd/crds/yacd.meigma.io_cardanodbsyncs.yaml` - generated the Helm-packaged CRD.
- `PROJECT` - added the `CardanoDBSync` resource metadata.

## Open Threads
- Implement the first `CardanoDBSync` controller and reconcile managed Postgres, a dedicated follower node, db-sync workload, Services, status, and conditions.
- Decide during controller prototyping whether `CardanoNetwork` needs an explicit status reference for generated network artifacts, or whether deterministic child names/internal render paths are sufficient.
- Add RBAC, Helm workload wiring, envtest coverage, CLI/devconfig support, and any Chainsaw runtime smoke only when the controller slice exists.
- External Postgres, raw db-sync passthrough, rollback/stop controls, socket/schema path overrides, and arbitrary extra args remain intentionally deferred.

## References
- PR #17: https://github.com/meigma/yacd/pull/17
- Merged commit: `c25390355e73080c731705cc810508aad4fe444d`
- Reviewed head commit: `57643077eae2ae378be48c1d4e15945aa13ea15a`
