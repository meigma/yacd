---
id: 003
title: First YACD environment prototype
date: 2026-05-20
status: complete
repos_touched: [yacd]
related_sessions: [001, 002]
---

## Goal
Define the first real YACD network API shape, then start proving the local
Cardano development environment path with the smallest useful controller and
runtime planning slices.

## Outcome
The goal was met. PR #3 landed the first `CardanoNetwork` CRD draft. PR #4
landed the pure localnet planning package, the read-only controller adapter
from `CardanoNetwork` to a `cardano-testnet create-env` plan, a live Kind/Tilt
smoke path that proves the controller logs the expected plan, and the
stateful dev-stack lifecycle expected by future human and agent sessions.

## Key Decisions
- The first primary CRD is `CardanoNetwork` rather than a single-node API,
  because YACD is modeling an environment/network that will grow supporting
  services around the chain.
- The CRD keeps `spec.mode` as `local|public`; public networks use a
  `profile` such as `preprod`, `preview`, `mainnet`, or `custom`.
- Custom public profiles are in-cluster only for now and use
  `corev1.LocalObjectReference` to a ConfigMap or Secret. OCI and HTTP bundle
  sources were deferred because they assume infrastructure not guaranteed in
  the target cluster.
- The first runtime implementation is local-mode only. Public mode remains in
  the API draft but is intentionally rejected by the current adapter.
- `internal/cardano/localnet` is Kubernetes-free and API-free. It owns only the
  `cardano-testnet create-env` input boundary, deterministic invocation shape,
  output layout, fingerprint, and manifest data.
- Controller reconciliation remains read-only in this slice. It fetches the
  CR, builds a localnet plan for supported local input, and logs the result
  without creating children, writing status, emitting Events, or producing
  workload resources.
- The development stack is now a managed Moon contract. `root:dev-up` starts
  Kind and background Tilt, records shared runtime state, waits for readiness,
  and exits; `root:dev-down` is the authoritative cleanup path.

## Changes
- `api/v1alpha1` - added the first `CardanoNetwork` API, validation markers,
  scheme registration, and generated deepcopy/CRD output.
- `charts/yacd/crds` - added the packaged `CardanoNetwork` CRD.
- `internal/cardano/localnet` - added deterministic `cardano-testnet
  create-env` planning with defaults, validation, fingerprinting, manifest
  data, and unit coverage.
- `internal/controller/cardanonetwork` - added the controller scaffold,
  local-mode adapter, read-only reconcile loop, and fake-client/unit tests.
- `cmd/setup.go` and `charts/yacd/templates/rbac-manager.yaml` - registered
  the controller and aligned read RBAC for `cardanonetworks`.
- `Tiltfile`, `.dev/`, `moon.yml`, `.gitignore`, `.tiltignore`, and
  `AGENTS.md` - moved development tooling under `.dev`, set controller debug
  logging in Tilt, and converted dev stack tasks to managed background Tilt.
- `.session.md` - added explicit development stack startup and shutdown
  requirements for future sessions.
- `README.md` and test files - refreshed current-state docs and registration
  coverage around the new API/controller surface.

## Open Threads
- Map more CRD fields into runtime behavior only after the local node workload
  layer proves which knobs are actually consumed.
- Build the next Kubernetes layer: init container construction from
  `localnet.Plan`, PVC/shared-state mounting, node container shape, and then
  full StatefulSet/Service assembly.
- Add Ogmios as its own workload fragment after the node runtime paths and
  socket mount contract are concrete.
- Add status conditions and Events once reconciliation begins creating or
  validating child resources.
- Revisit public/custom profile handling after local mode is operational.
- Release Please on `master` still fails before project code runs because the
  release-app token inputs are not configured for this repository.

## References
- PR #3: <https://github.com/meigma/yacd/pull/3>
- PR #4: <https://github.com/meigma/yacd/pull/4>
- `.journal/001/SUMMARY.md`
- `.journal/002/SUMMARY.md`
