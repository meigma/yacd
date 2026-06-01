# PR-B1 — Remove the network-artifacts ConfigMap and custom-public entirely (F0)

## Context

F0: a public **mainnet** `CardanoNetwork` can't be created because the controller
builds a `<net>-network-artifacts` ConfigMap (mounted at `/profile` for the node to
read) that exceeds etcd's ~1 MiB cap for mainnet. The F0 redesign makes the manager
stop being an authoritative config source: configs are generated (local) or fetched
(curated-public) onto the node PVC, `cardano-node` reads from the PVC, and other
consumers fetch over HTTP from the serve sidecar (PR-A added serve + the staged
`/state/artifacts`; PR-C moved db-sync to HTTP).

**Scope decision (with the user): also remove "custom-public" now.** Custom-public
(`spec.public.profile: custom`, a user-supplied full genesis/config via ConfigMap/
Secret refs) is the *only* remaining consumer of the ConfigMap once local + curated-
public read from the PVC. Removing it lets us **delete the ConfigMap concept
entirely** instead of building mode-aware machinery to preserve it — net *less* work
and far simpler (mostly deletions). After this PR YACD supports only **local** and
**curated-public** (preview/preprod/mainnet).

Per the agreed split: this is **PR-B1 (operator logic + API, no image rebuild)**.
The publisher binary/module + Dockerfile deletion is **PR-B2** (needs a new
cardano-testnet image); after PR-B1 the publisher is simply never invoked.

**The ConfigMap is the controller's only window into artifact *content* for three
signals that must be re-sourced (now single-path, no ConfigMap):**
1. **`ArtifactsReady`** — db-sync gates workload apply on it; today it's `True` only
   when `ProducerConfigMap(ConfigMap)` succeeds. Re-source from served-artifacts
   readiness.
2. **`Status.Sync` timing** — `primaryNodeSyncStatusConditions` derives Shelley
   `SystemStart`/`SlotLength` from the ConfigMap. Re-source from the plan into
   `Status.Network`.
3. **`DataHash`** — db-sync identity + pod-roll; PR-C also dereferences
   `Status.Artifacts.*` unconditionally (nil-panic once gone). Re-source identity
   from `Status.Network` fingerprint.

**Identity churn (accepted, documented):** db-sync's `NetworkArtifactHash` moves to
`Status.Network.NetworkFingerprint` (`LocalnetFingerprint` for local). One-time
`UnsupportedDatabaseIdentityChange` for any db-sync created before the upgrade
(Deployment scales to 0 until recreated) — pre-1.0 fresh-build; no zero-churn option
exists. Document in PR body/CHANGELOG.

## The model (after PR-B1)

There is no `<net>-network-artifacts` ConfigMap and no `/profile` mount for any
network. **Single path for all networks:**
- **local**: create-env generates artifacts on the node PVC; node reads them there.
- **curated-public**: `cardano-tools fetch` writes the flat bundle to `/state/artifacts`
  on the PVC; node + ogmios read there.
- All consumers (db-sync, CLI, out-of-cluster) fetch over HTTP from the serve sidecar.
- `ArtifactsReady`, db-sync identity, and sync-timing come from controller-held data
  (the plan) or already-published status (`Status.Network`, `Status.Endpoints.Artifacts`).

## Implementation (one PR-B1)

### 1. Remove custom-public (API + machinery)
- `api/v1alpha1/cardanonetwork_types.go`: drop `PublicNetworkProfileCustom` from the
  profile enum, the `NetworkConfigSource` struct, the `ConfigSource` field on
  `PublicNetworkSpec`, and the custom CEL `XValidation`. `moon run root:generate`
  (CRD + deepcopy).
- Delete `internal/controller/cardanonetwork/public_profile_source.go` (whole file:
  custom bundle loading + ConfigMap/Secret indexers + watches), and the
  `indexCustomProfileSources`/`publicCustomProfileBundle` wiring + `.Owns()`/watch
  registration in `controller.go`, and the `publicCustomBundle` builder field +
  branch in `builder.go`.
- `internal/cardano/publicnet/`: delete `CustomBundle`, `customArtifacts`, the custom
  branch in `BuildPlan`, `SupportedCustomProfileKeys`, and custom validation in
  `validateBootstrapProfile`.
- Delete `examples/public-custom/`.

### 2. Delete the network-artifacts ConfigMap concept (no gating)
`internal/controller/cardanonetwork/`
- Delete `networkArtifactsConfigMap()` (artifacts.go), `applyNetworkArtifactsConfigMap`
  + the recovery/UID-stamp machinery (apply.go + artifacts.go + controller.go ~362-408),
  `publicProfileVolume()`/`publicProfileVolumeName` (resources.go), and
  `networkArtifactsConfigMapName` (names.go).
- Delete `localArtifactPublisherResources` + the `artifactPublisher{SA,Role,RoleBinding,
  ProjectedVolume,VolumeMount}` builders + `apply*` + name helpers + result fields/log
  keys; keep the local **create-env** init container (move it out of the deleted
  publisher path).
- `init_container.go`: drop the artifact-publisher env vars + token volume from the
  create-env init; keep create-env.
- **RBAC**: drop the `serviceaccounts`/`roles`/`rolebindings` `+kubebuilder:rbac`
  markers AND the matching `.Owns(ServiceAccount/Role/RoleBinding)` watches (dropping
  markers without watches makes the manager fail on forbidden list/watch). Drop the
  `configmaps` marker too (nothing creates a ConfigMap now — verify no other ConfigMap
  is owned; if none, remove it entirely). Plus the custom-source ConfigMap/Secret
  watch markers from step 1.
- `internal/controller/networkartifacts/`: delete `ProducerConfigMap`,
  `ProducerConfigMapNeedsRecovery`, `dataContract`, `artifactNetworkFingerprint`, AND
  the now-unused `ConsumerStatus`/`ConsumerConfigMap`/`ConsumerConnection` (db-sync no
  longer has a ConfigMap path — step 4). Keep the `Manifest`/served primitives.

### 3. Repoint curated-public node + ogmios to the PVC
`internal/controller/cardanonetwork/{containers.go,plan.go}`
- Curated-public `plan.ConfigFile`/`TopologyFile`/working dir → `/state/artifacts`
  (`servedArtifactsDir`). Replace the `public-profile` mount with a
  `localnetStateVolumeName` PVC mount on the **node**, and **add a `/state/artifacts`
  read-only PVC mount to ogmios** (today public ogmios mounts state only for local).
- `stagesServedArtifacts`/`isCuratedPublicProfile` simplify: all public is now
  served (no custom exception). Verify the fetched `configuration.yaml` references
  genesis by bare relative filenames co-located in the flat dir.

### 4. db-sync becomes single-path serve (delete the ConfigMap fallback)
`internal/controller/cardanodbsync/` + `internal/controller/cardanonetwork/dbsync_sidecar.go`
- Delete the `artifactSource{configMap}` branch + the ConfigMap-path body in
  `controller.go`, the `ConsumerConnection`/`networkConnection` parse, and collapse the
  3-way switch in `dbsync_sidecar.go` (`primaryDBSyncAttachment`) to serve / pending.
- `NetworkArtifactHash` (builder.go `dbSyncPlanSpec`), the serve-path guard
  (controller.go), and `dbSyncArtifactDataHashAnno` (resources.go) all source from
  `Status.Network.NetworkFingerprint`/`LocalnetFingerprint` — no `Status.Artifacts`
  reads (fixes the nil-derefs). Keep the `networkArtifactHash` wire field +
  `TestDatabaseIdentityFingerprintIsFrozenAgainstLegacyWire` (value untouched).
- Drop the `PublicNetworkProfileCustom` case from `validatePublicDBSyncSupport` +
  `ValidatePrimarySidecarNetwork`.

### 5. Re-source ArtifactsReady + sync-timing (single-path)
`internal/controller/cardanonetwork/{status.go,sync_probe.go}` + `api/v1alpha1`
- `ArtifactsReady`: derive from served-artifacts availability (owned serve container
  readiness / `status.endpoints.artifacts` published), not `ProducerConfigMap`.
- Sync-timing: add `SystemStart` + `SlotLengthSeconds` to `Status.Network`
  (`CardanoNetworkIdentityStatus`), published from the in-memory plan; have
  `primaryNodeSyncStatusConditions` use it. `root:generate`.
- `Status.Artifacts` (`CardanoNetworkArtifactsStatus`: NetworkConfigMapName/SchemaVersion/
  DataHash) is now unused → remove the struct + field. `Status.Endpoints.Artifacts`
  (the serve Service endpoint, PR-A) STAYS.

### 6. Chart RBAC mirror + chainsaw + tests
- `charts/yacd/templates/rbac-manager.yaml`: remove the serviceaccounts/roles/
  rolebindings (and configmaps, if dropped) rules to match controller-gen
  (`test/chart/rbac_test.go` enforces byte-equivalence). `root:generate` first.
- `test/chainsaw/manager-smoke/chainsaw-test.yaml`: remove the `status.artifacts`/
  ConfigMap/artifact-publisher assertions (local phase4-smoke now has none); assert
  the served-artifacts shape (artifacts Service, serve sidecar,
  `status.endpoints.artifacts.url`). KEEP the PR-C db-sync HTTP assertion.
- Delete the custom-public tests (`controller_test.go` 4 cases + helpers,
  `controller_envtest_test.go` HandlesCustomProfileSources, `builder_test.go`
  customPublicProfileBundle). Add envtest: a mainnet-profile + a curated (preprod)
  network render with NO ConfigMap, read `/state/artifacts`, `ArtifactsReady=True`,
  `Status.Sync` timing present; db-sync identity from fingerprint.

## Critical files
- `api/v1alpha1/cardanonetwork_types.go` — drop custom profile + `NetworkConfigSource` +
  `CardanoNetworkArtifactsStatus`; add `Status.Network` timing.
- `internal/controller/cardanonetwork/{public_profile_source.go(delete),artifacts.go,apply.go,controller.go,builder.go,init_container.go,names.go,resources.go,containers.go,plan.go,status.go,sync_probe.go}`.
- `internal/cardano/publicnet/{types.go,plan.go}` — drop custom.
- `internal/controller/networkartifacts/artifacts.go` — delete Producer*/Consumer* (keep Manifest).
- `internal/controller/cardanodbsync/{controller.go,builder.go,resources.go,public_network.go,primary_sidecar.go}` + `cardanonetwork/dbsync_sidecar.go` — single-path serve; fingerprint identity.
- `charts/yacd/templates/rbac-manager.yaml`, `test/chainsaw/manager-smoke/chainsaw-test.yaml`, envtest/unit tests, `examples/public-custom/`(delete).

## Invariants
- **No `<net>-network-artifacts` ConfigMap is created for any network**; the concept,
  `/profile` mount, producer/consumer helpers, recovery/UID machinery, and
  `Status.Artifacts` are gone. `Status.Endpoints.Artifacts` (serve endpoint) stays.
- **db-sync is single-path serve**; identity always from the network fingerprint;
  the frozen wire test stays green (field + value untouched).
- **No new in-pod→controller channel** — re-sourcing uses the plan / `Status.Network`.
- **Mainnet renders with zero ConfigMap** — the literal unblock.
- Custom-public is fully removed (API breaking, pre-1.0, intended).
- PR-B1 does **not** delete the publisher binary/module or edit the Dockerfile (PR-B2).

## Verification
1. `moon run root:generate` (CRD + deepcopy: custom removed, `Status.Network` timing
   added, `Status.Artifacts` removed), `root:check`, `root:test` (envtest + frozen test).
2. New envtest: mainnet-profile + preprod render with NO ConfigMap, node+ogmios read
   `/state/artifacts`, `ArtifactsReady=True`, `Status.Sync` timing present; a
   `profile: custom` spec is now rejected by the CRD.
3. `moon run root:test-e2e` (chainsaw local): no ConfigMap, db-sync over HTTP reaches
   `Synced`, node-sync status populated, cleanup. Full CI green (the PR-C-de-flaked
   sidecar envtest stays green).
4. Squash-merge, e.g. `feat(cardanonetwork)!: remove the network-artifacts ConfigMap and custom-public; read node config from the PVC (F0 PR-B1)`. PR body documents the API removal + the one-time db-sync identity churn.

## Out of scope (later F0 slices)
- **PR-B2**: delete `containers/cardano-testnet/publisher/**` + the dispatcher +
  `internal/artifactpublisher`; remove the Dockerfile publisher stage + the init
  wrapper's `publish_artifacts`; cut a new cardano-testnet image.
- **PR-D**: remove the cardano-tools `report` verb; pin the manager cardano-tools image
  to a published digest; drop the e2e build+load hack; DESIGN.md rewrite.
