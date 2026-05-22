---
id: 007
title: Ogmios chain API
date: 2026-05-21
status: complete
repos_touched: [yacd]
related_sessions: [003, 004, 005, 006]
---

## Goal
Implement phase 3 of the initial YACD prototype by adding Ogmios as the
default chain API for local-mode `CardanoNetwork` resources. Keep the slice
narrow: no new CRD, no generic supporting-service framework, just the Ogmios
sidecar, Service, status endpoint, and readiness behavior.

## Outcome
The goal was met. PR #12 was squash-merged into `master` as `fe8b4fd`, the
local default checkout was fast-forwarded, the development stack was stopped,
and the implementation worktree was removed. The final branch includes the
original phase 3 implementation plus review-driven hardening around RBAC,
readiness, compatibility enforcement, and installed-operator smoke coverage.

## Key Decisions
- Keep Ogmios inside the primary `CardanoNetwork` workload for this slice,
  because phase 3 is proving the default node-side API path rather than the
  later supporting-service CRD model.
- Default omitted `spec.chainAPI` and `spec.chainAPI.ogmios` in the controller
  builder, because Kubernetes CRD defaults do not materialize omitted nested
  objects reliably enough for reconcile logic.
- Use `ogmios health-check` for startup, readiness, and liveness probes,
  because HTTP 200 from `/health` can still report a disconnected Ogmios
  process.
- Derive `NodeReady` and `OgmiosReady` from live pod container readiness
  instead of Deployment availability, because the Ogmios sidecar readiness
  should not make the node condition lie.
- Enforce a package-local Ogmios/cardano-node compatibility table for
  recognized release tags, because arbitrary image tags cannot provide a
  defensible compatibility signal.

## Changes
- `internal/controller/cardanonetwork/workload_builder.go` - extended the
  primary workload model with resolved Ogmios settings, sidecar rendering,
  Ogmios Service rendering, health-check probes, and compatibility validation.
- `internal/controller/cardanonetwork/apply.go` - added deletion of the owned
  Ogmios Service when Ogmios is explicitly disabled.
- `internal/controller/cardanonetwork/status.go` - published the Ogmios
  endpoint, added `OgmiosReady` and aggregate `Ready`, and split node/Ogmios
  readiness using live pod container state.
- `internal/controller/cardanonetwork/controller.go` and `cmd/setup.go` -
  wired Ogmios Service apply/delete, status requeues, pod read RBAC, Service
  delete RBAC, owned Service watches, and the uncached API reader.
- `api/v1alpha1/cardanonetwork_types.go`, generated CRD manifests, and Helm
  RBAC - updated status docs and packaged permissions.
- `internal/controller/cardanonetwork/*_test.go` - added builder, reconciler,
  and manager-backed envtest coverage for default Ogmios, overrides, disable,
  Service repair/delete, owner conflicts, endpoint status, separate readiness,
  and compatibility failures.
- `test/chainsaw/manager-smoke/chainsaw-test.yaml` - extended the installed
  operator smoke to verify the Ogmios Service, publish status endpoints, run a
  real `queryNetwork/tip` through the Service, and disable Ogmios to prove
  Service deletion and endpoint clearing.

## Open Threads
- The Ogmios/cardano-node compatibility table is hardcoded. Future node or
  Ogmios bumps should refresh the table and prove the default pair with a real
  Ogmios protocol query.
- Current-state documentation drift from session 006 still exists, especially
  README text that describes the reconciler as future work.
- Public networks, db-sync/follower-node services, faucet/topup, and CLI
  connection/status UX remain later phases.
- Aggregate `Ready` intentionally remains false when Ogmios is explicitly
  disabled because phase 3 treats Ogmios as the default required chain API.

## References
- PR #12: https://github.com/meigma/yacd/pull/12
- Merge commit: `fe8b4fd` (`feat(cardanonetwork): expose ogmios chain api (#12)`)
- Prior session 006: `.journal/006/SUMMARY.md`
