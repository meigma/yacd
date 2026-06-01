---
id: 047
title: F0 redesign PR-C (merged) + PR-B planned; custom-public removal decided
date: 2026-06-01
status: complete
repos_touched: [yacd]
related_sessions: [046, 043, 029, 045]
---

## Goal
Continue the F0 redesign (public **mainnet** `CardanoNetwork` can't be created
because the `<net>-network-artifacts` ConfigMap exceeds etcd's ~1 MiB cap). Pick
up after PR-A (serve sidecar + PVC-staged artifacts, merged session 046) and land
**PR-C** (CardanoDBSync consumes artifacts over HTTP), then proceed to **PR-B**
(node reads config from the PVC; delete the ConfigMap — the mainnet unblock).

## Outcome
**PR-C: met and MERGED** (PR #77, squash `231ccde`). **PR-B: planned, approved,
and started; deliberately deferred to a fresh session** (the user chose to stop
rather than push a large change to exhaustion). The PR-B WIP branch was discarded
clean; everything to resume in one focused pass is banked here (see Open Threads +
`PR-B1-PLAN.md`).

## Key Decisions
- **PR-C `cardano-tools` command named `sync`** (user choice via AskUserQuestion);
  Go package `artifactsync` to avoid shadowing stdlib `sync`. Bundled into one PR-C
  (not split).
- **PR-C transport discriminator = `network.Status.Endpoints.Artifacts != nil`**:
  local + curated-public fetch over HTTP; custom-public kept the ConfigMap. db-sync
  identity stayed on `Status.Artifacts.DataHash` (zero churn) — PR-C was transport-only.
- **PR-C cross-controller fix (`98d6534`)**: the primary-sidecar attachment is built
  in the SAME reconcile that later publishes `status.endpoints.artifacts`, so that
  status field is nil at attachment-build time. Discriminate on the network **spec**
  (`stagesServedArtifacts`), not status. Caught by a test I added.
- **PR-C genesis-hash enrichment (`b2dc24e`)**: the e2e exposed that the served
  `configuration.yaml` lacked genesis hashes (`cardano-db-sync: "key ByronGenesisHash
  not found"`) — create-env writes them only in-memory via the legacy publisher.
  `cardano-tools stage` now enriches via the shared cardano-cli hasher.
- **De-flake bundled into PR-C (`ee06df5`)**: `TestCardanoNetworkControllerManager
  AttachesPrimarySidecarDBSync` failed CI 3x (exceeding the documented 2x-then-green
  flake pattern); bumped its tight 10s `Eventually` waits to 1m (user chose "bundle").
- **PR-B scope EXPANDED — remove "custom-public" entirely** (user direction). It's
  the ConfigMap's last consumer once local+curated read from the PVC, so removing it
  deletes the ConfigMap **concept** entirely instead of building mode-aware machinery
  to preserve it — net *less* work (~650-750 LOC deleted, mostly deletions). YACD then
  supports only local + curated-public (preview/preprod/mainnet).
- **PR-B split** (user choice): **PR-B1** = operator logic + API (no image rebuild);
  **PR-B2** = delete the publisher binary/nested-module + Dockerfile stage + new
  cardano-testnet image release.
- **db-sync identity churn accepted** (user choice): on PR-B the identity input moves
  from the deleted ConfigMap `DataHash` to `Status.Network.NetworkFingerprint`. One-time
  `UnsupportedDatabaseIdentityChange` for any db-sync created before the upgrade
  (Deployment scales to 0 until recreated). No zero-churn option exists; pre-1.0
  fresh-build; document in the PR.

## Changes
All merged via PR #77 (`231ccde`), on `yacd`:
- `containers/cardano-tools/internal/artifactsync/*` + `cli/sync.go` + `cli/root.go`
  — new `sync` command (GET manifest → fetch+verify each file → write manifest verbatim).
- `containers/cardano-tools/internal/stage/*` — enrich `configuration.yaml` genesis
  hashes (injectable hasher; cardano-cli in the image).
- `internal/controller/cardanodbsync/*` — serve-path HTTP transport (emptyDir + `sync`
  init for dedicatedFollower; staged-PVC subPath mount for primarySidecar); status-derived
  identity; custom-public keeps the ConfigMap path.
- `internal/controller/cardanonetwork/dbsync_sidecar.go` — spec-based sidecar artifact
  discriminator; `internal/controller/cardanonetwork/controller_envtest_test.go` — de-flake.
- `test/chainsaw/manager-smoke/chainsaw-test.yaml` — assert db-sync fetches over HTTP.

## Open Threads — RESUME PR-B1 HERE (fresh session)
**PR-B1 is fully planned and APPROVED. The plan is banked at
`.journal/047/PR-B1-PLAN.md` (durable copy of the approved plan).** Branch fresh off
master (`231ccde`); the prior WIP branch `feat/f0-delete-network-configmap` was
discarded (no commits). Execute the plan in one pass — it is a large, compile-coupled
~23-file change with no intermediate checkpoint, so do the whole spine then compile.

PR-B1 = **remove the network-artifacts ConfigMap and custom-public entirely**:
1. **API** (`api/v1alpha1/cardanonetwork_types.go`): drop `PublicNetworkProfileCustom`,
   `NetworkConfigSource`, `PublicNetworkSpec.ConfigSource` + its CEL, and `Status.Artifacts`
   (`CardanoNetworkArtifactsStatus`). NO new fields. `moon run root:generate`.
2. **Remove custom-public**: delete `cardanonetwork/public_profile_source.go` + its
   watches/indexers/builder field; `publicnet` `CustomBundle`/`customArtifacts`/custom
   branch/`SupportedCustomProfileKeys`/`validateCustomFile`/consts; `examples/public-custom/`;
   the custom cases in `cli/internal/devconfig/config.go` and db-sync validation.
3. **Delete the ConfigMap concept**: `networkArtifactsConfigMap`/`applyNetworkArtifactsConfigMap`/
   recovery+UID machinery/`publicProfileVolume`/`artifactPublisher{SA,Role,RoleBinding,...}`;
   `networkartifacts` `Producer*`/`Consumer*` (keep `Manifest`). KEEP the create-env init.
4. **RBAC**: drop the `serviceaccounts`/`roles`/`rolebindings` (and `configmaps` if no
   owned ConfigMap remains) `+kubebuilder:rbac` markers AND the matching `.Owns()` watches
   (dropping markers without watches → manager fails on forbidden list/watch). Mirror in
   `charts/yacd/templates/rbac-manager.yaml` (byte-equivalence test).
5. **Repoint curated-public** node + **ogmios** to `/state/artifacts` (PVC). Ogmios needs
   a NEW `/state/artifacts` read-only mount (today public ogmios mounts state only for local).
6. **Re-source the 3 ConfigMap-derived signals (single-path)**:
   - `ArtifactsReady` (status.go): from served-artifacts/serve readiness, not `ProducerConfigMap`.
   - **Sync timing (sync_probe.go): FETCH `shelley-genesis.json` from the serve endpoint**
     (`status.endpoints.artifacts.url`) and reuse `parseShelleyGenesisTiming` — NO new API
     field, and it avoids the local-systemStart-unknown problem (create-env sets systemStart
     in-pod). [This refines the plan's "add timing to Status.Network" — use the serve-fetch.]
   - db-sync identity + pod-roll + serve-path guard: from `Status.Network.NetworkFingerprint`
     (`LocalnetFingerprint` for local); fixes PR-C's `Status.Artifacts` nil-derefs.
7. **db-sync single-path serve**: delete the ConfigMap fallback / `ConsumerConnection` /
   `networkConnection` / 3-way sidecar switch → serve-only.
8. **Tests**: delete custom-public tests; add envtest proving a mainnet-profile + preprod
   render with NO ConfigMap, node+ogmios read `/state/artifacts`, `ArtifactsReady=True`,
   `Status.Sync` timing present, db-sync identity from fingerprint; rewrite chainsaw.
9. Verify: `root:generate`/`check`/`test` + `test-e2e`; PR title
   `feat(cardanonetwork)!: ...`; document the API removal + db-sync identity churn.

Then **PR-B2** (publisher module/Dockerfile/new image) and **PR-D** (remove `report` verb;
pin manager cardano-tools image to a published digest; drop the e2e build+load hack; DESIGN.md).

Carried (pre-existing): TEST_REPORT F2/F4; test-harness Phases 3-5.

## References
- Merged: PR #77 `https://github.com/meigma/yacd/pull/77` (squash `231ccde`).
- Approved PR-B1 plan: `.journal/047/PR-B1-PLAN.md`.
- Prior: `.journal/046/SUMMARY.md` (PR-A), `.journal/043/`, `.journal/029/` TEST_REPORT (F0 origin).
- Release-please PR opened by PR-A: #76 (`release cardano-tools 11.1.0-yacd.4`).

## Lessons
- **The e2e earns its keep.** It caught the served-config genesis-hash gap that every
  unit test missed — a latent PR-A defect only exposed once PR-C made a consumer actually
  use the served config. Run `root:test-e2e` before declaring an F0 slice done.
- **Verify CI "exit 0" against the log.** A `moon ... | tee | tail` and a `gh pr checks
  --watch; echo` both reported exit 0 while the real run had FAILED (the trailing command
  masked the exit). Read the log/`gh run view --log-failed`, not just the exit code.
- **`cd` in a compound Bash command doesn't reliably persist** — a stray `git commit`
  landed on local master instead of the feature branch. Caught with `git -C <abs>` plumbing
  and recovered (cherry-pick + `reset --hard origin/master`; never pushed). Use `git -C`
  and verify `pwd`/branch before committing.
- **Local `test/chart` RBAC test fails with phantom `nginxdeployments` markers** (the
  proto-go-shim `{{context.Compiler}}` bug degrading controller-gen). PROVEN environmental:
  fails identically on clean master; passes in CI. Do not chase it locally.
- **Adversarial Plan-agent review before a big slice pays off.** It found the 3 hidden
  ConfigMap-coupled signals (ArtifactsReady, sync-timing, DataHash) that would have
  silently bricked PR-B — none were obvious from the surface description.
