---
id: 047
title: F0 redesign PR-C — db-sync consumes configs over HTTP
started: 2026-05-31
---

## 2026-05-31 18:21 — Kickoff
Goal for the session: continue the F0 redesign series. PR-A is merged
(session 046, PR #75, `c61e0a6`). The next slice per the agreed order
(A → C → B → D) is **PR-C: CardanoDBSync consumes network configs over HTTP**.
Replace the CardanoDBSync ConfigMap mount with a cardano-tools `fetch` init
→ emptyDir + manifest verify, pointed at the primary network's serve endpoint
(`status.endpoints.artifacts.url`). PR-C MUST land before PR-B, which deletes
the `<net>-network-artifacts` ConfigMap that db-sync currently GETs by name.

Current state of the world:
- `master` at `c61e0a6` (PR-A merged, additive: serve sidecar + producer +
  `<net>-artifacts` Service + `status.endpoints.artifacts` alongside the
  existing ConfigMap path; node/ogmios/faucet containers unchanged).
- PR-C reworks ~6 `internal/controller/cardanodbsync` files plus a
  cross-controller edit to
  `internal/controller/cardanonetwork/dbsync_sidecar.go` (~lines 103, 113).
- Each F0 PR must keep the chainsaw e2e green (runs on every PR). Branch fresh
  off master.
- Carried non-F0 threads: TEST_REPORT F2/F4 open; test-harness Phases 3–5
  remain; KNOWN FLAKE
  `TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync`
  (load-sensitive envtest); e2e Docker Hub 429 jitter on ogmios/kupo.
- Dev stack was left UP but ORPHANED on `kind-yacd-dev` at the end of session
  046 (the F0 worktree that owned it was removed at merge; `.run/yacd-dev`
  ownership dangles). Tear down with `moon run root:dev-down` from the main
  checkout, or repair before starting implementation.

Plan (rough):
1. Wait for the user's actual request / confirm PR-C is the target.
2. Create a fresh implementation worktree off master via `wt`.
3. Start the dev stack (`moon run root:dev-up`) once after selecting the
   worktree — note the orphaned-stack cleanup may be needed first.
4. Implement PR-C, keep `root:check`/`root:test`/chainsaw green, PR + squash.

## 2026-05-31 19:57 — PR-C implemented; e2e in flight
Plan approved (cardano-tools command named `sync`; one bundled PR-C). Work done
on branch `feat/f0-dbsync-http` (off master `c61e0a6`), worktree
`.wt/feat-f0-dbsync-http`. Commits so far:
- `6052f4b` feat(cardano-tools): add `sync` command (package `artifactsync` to
  avoid shadowing stdlib `sync`; CLI verb stays `sync`). GET manifest.json →
  fetch+verify each listed file → write manifest verbatim. httptest tests.
- `dbdcf3b` feat(cardanodbsync): consume artifacts over HTTP on the serve path.
  Transport discriminator in `controller.go` (`servedArtifactsURL`); serve path
  skips ConfigMap GET + ConsumerConnection, keeps `Status.Artifacts.DataHash`
  for the identity fingerprint (zero churn), derives identity from status
  (networkConnection nil). `public_network.go` reads mode/profile from spec.
  dedicatedFollower: emptyDir + `network-artifacts-sync` init container
  (`syncInitContainer`) before pgpass init. primarySidecar: `artifactsMount()`
  override mounts the primary state PVC subPath `artifacts`. New builder fields:
  defaultCardanoToolsImage, servedArtifactsURL, artifactsVolumeOverride,
  artifactsSubPath. `artifactSource{serveURL, configMap}` threaded through
  reconcileReadyDBSync/reconcileWorkloads/reconcilePrimarySidecarWorkloads.
- `98d6534` fix(cardanonetwork): KEY DISCOVERY — the primary-sidecar attachment
  is built in the SAME reconcile that later publishes
  `status.endpoints.artifacts`, so that status field is nil at attachment-build
  time. Discriminate on the SPEC (`stagesServedArtifacts` = isLocal ||
  curated-public), not status, in `dbsync_sidecar.go`. (The db-sync controller
  is fine reading the already-reconciled referenced network's status + needs the
  URL anyway.) Caught by an attachment test I added.
- `c284ce5` lint; `b34dea4` chainsaw assert (sync init present, no ConfigMap
  volume on phase6-managed-dbsync).

Tests added (all green): cardanodbsync `artifacts_transport_test.go` (4 builder
cases: dedicatedFollower serve emptyDir+sync-init, dedicatedFollower configmap,
primarySidecar serve PVC-subPath, primarySidecar configmap) + controller
decoupling test (Artifacts endpoint set, ConfigMap absent → still reconciles);
cardanonetwork attachment test now asserts the staged-PVC subPath mount, and the
former "skip when ConfigMap missing" local-network test is flipped to assert
serve-path attaches WITHOUT a ConfigMap (the PR-B enablement).

Gates green: `root:generate` (idempotent, no API change), `root:check`,
`root:test` (full suite, envtest incl. frozen fingerprint untouched).
Custom-public KEEPS the ConfigMap path (no regression). NOW: `root:test-e2e`
running in background (chainsaw on real Kind, builds cardano-tools from source
so it carries `sync`). After green: PR, then journal close.
