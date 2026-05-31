---
id: 045
title: CardanoNetwork node sync status
date: 2026-05-31
status: complete
repos_touched: [yacd]
related_sessions: [015, 027, 041, 044]
---

## Goal
Add first-pass, non-external `CardanoNetwork` node sync visibility so cluster
operators can see whether the primary node appears synchronized, how far it is
behind the inferred network slot, and whether it appears stuck.

## Outcome
Met. PR #74 merged `CardanoNetwork.status.sync` plus the
`NodeSynchronized`/`NodeProgressing` conditions, all derived from in-cluster
sources only: the verified network artifact ConfigMap and the owned Ogmios
`/health` endpoint. The new visibility does not feed aggregate `Ready`.

## Key Decisions
- Use Ogmios + verified `shelley-genesis.json` only -> gives local and public
  networks a shared non-external first slice while leaving public remote-tip
  comparison for a future enhancement.
- Keep `NodeSynchronized` visibility-only -> operators can see sync state
  immediately without changing existing readiness semantics.
- Accept Ogmios HTTP 500 health bodies -> current Ogmios uses that status for
  disconnected health, so parsing the body is required to publish
  `connectionStatus=disconnected`.
- Treat nullable Ogmios progress fields as unknown-state health data -> startup
  and no-tip states should not collapse into `OgmiosHealthUnavailable` when the
  endpoint itself is reachable.

## Changes
- `api/v1alpha1/cardanonetwork_types.go` - added `status.sync`, tip fields,
  lag fields, probe timestamps, Ogmios synchronization, and condition types.
- `internal/controller/cardanonetwork/sync_probe.go` - added the sync probe,
  Ogmios `/health` parsing, genesis timing parsing, inferred slot/lag math, and
  condition projection.
- `internal/controller/cardanonetwork/controller.go` and `status.go` - wired sync
  probing into reconcile/status patching and the 1m refresh while Ogmios is
  enabled.
- `internal/controller/cardanonetwork/*_test.go` - covered parsing variants,
  nullable Ogmios fields, disconnected health, lag math, synchronized,
  catching-up, stalled, and unavailable paths.
- `config/yacd/crds/yacd.meigma.io_cardanonetworks.yaml` and
  `api/v1alpha1/zz_generated.deepcopy.go` - regenerated API artifacts.
- `moon.yml` - allowed controller-gen dangerous float types for the status
  synchronization estimate.

## Open Threads
- External public-tip comparison remains intentionally out of scope.
- `NodeSynchronized` does not participate in aggregate `Ready`; revisit only
  after operators have lived with the visibility surface.
- No separate manual Kind proof was run for the final merged PR; local
  `root:test`, `root:check`, `git diff --check`, and GitHub CI/e2e passed.
- Session 046 / `feat/f0-public-profile-pvc` is unrelated and still active.

## References
- PR #74: https://github.com/meigma/yacd/pull/74
- Merge commit: `bfadcf6 feat(cardanonetwork): publish node sync status (#74)`
- Ogmios HTTP API: https://ogmios.dev/http-api/
- Ogmios monitoring docs: https://ogmios.dev/getting-started/monitoring/
- Prior db-sync sync probe context: `.journal/015/SUMMARY.md`
