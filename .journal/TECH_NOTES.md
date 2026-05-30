# Technical Notes

- YACD is intended as a Kubernetes-native Cardano development environment
  manager for builders, not validators or stake pool operators. The first
  prototype should stay local-first and Kind/Tilt-friendly.
- The primary CRD should represent a Cardano environment/network rather than a
  single node. The first runtime is now an owned singleton primary
  `cardano-node` Deployment, explicit owned PVC, owned ClusterIP Service
  exposing node-to-node TCP, and an Ogmios sidecar plus owned ClusterIP Service
  as the default chain API.
- Supporting services should be separate CRDs/controllers. Network-only
  services can run as independent workloads; heavy IPC services such as db-sync
  should prefer a dedicated follower-node Pod so they do not mutate or restart
  the primary node.
- db-sync is the first supporting-service priority. Yaci Store is a later
  optional Blockfrost-like/indexer candidate after the supporting-service model
  is proven.
- `CardanoDBSync` is the first supporting-service CRD/controller. It uses a
  required same-namespace `spec.networkRef.name`, consumes fresh verified
  `CardanoNetwork.status.artifacts.networkConfigMapName`, and currently
  supports external Postgres by reference and managed local/dev Postgres
  through `spec.database.managed`.
- The `CardanoDBSync` controller renders an owned config ConfigMap, pgpass
  Secret, db-sync state PVC, follower-node state PVC, two-container
  follower/db-sync Deployment, and metrics Service. It validates live network
  artifact ConfigMap data/hash before applying workloads, scales the Deployment
  to zero on hard prerequisite failure, and uses owned-child watches rather than
  placeholder resources.
- A `CardanoDBSync` database identity is accepted from owned runtime material:
  the db-sync state PVC annotation
  `yacd.meigma.io/dbsync-database-identity` is authoritative, while
  `status.database.acceptedIdentityFingerprint` is controller-published derived
  status. Parent reconciles intentionally enqueue accepted-identity status-only
  changes so forged or cleared status self-heals from the PVC annotation without
  a spec bump. If desired identity-affecting inputs drift after acceptance,
  reconcile stops before workload mutation and sets
  `UnsupportedDatabaseIdentityChange`.
- `internal/cardano/dbsync` is the Kubernetes-free planner for db-sync config,
  topology, invocation args, environment, plan fingerprint, and database
  identity fingerprint. The accepted database identity includes network
  artifact hash, DB address/user, db-sync image, ledger backend, and insert
  options; changes to that identity are rejected until a recreate or migration
  story exists. The package is split into focused files mirroring the
  `internal/cardano/localnet` layout; `DefaultInsertOptions()` is the
  recommended construction baseline, and `Runtime.DisableCache` /
  `Runtime.DisableEpochTable` map directly to the db-sync CLI flags so the
  zero value leaves the feature active.
- The `DatabaseIdentityFingerprint` wire shape is frozen behind private
  legacy-shape structs (`insertIdentity`, `txOutIdentity`,
  `featureSelectionIdentity`) so the immutable identity check in the controller
  (`internal/controller/cardanodbsync/apply.go`) does not reject existing
  resources when public Spec types add or rename JSON tags. The pinned hash in
  `TestDatabaseIdentityFingerprintIsFrozenAgainstLegacyWire` catches drift —
  fix the wire shape rather than updating the expected value.
- Managed `CardanoDBSync` Postgres creates `<dbsync>-postgres-auth` when
  `managed.authSecretRef` is omitted, `<dbsync>-postgres-state`,
  `<dbsync>-postgres` Service, and `<dbsync>-postgres` Deployment. The
  generated password is create-once only; if the generated Secret is deleted
  after managed DB identity acceptance, the controller degrades instead of
  regenerating a random password for an initialized data directory. A plain
  unowned same-name generated auth Secret restored with the original
  `data.password` is the narrow adoption exception: the controller adopts it
  only when the password re-derives the accepted managed Postgres identity,
  while wrong restored passwords remain `UnsupportedDatabaseIdentityChange` and
  foreign-owned Secrets remain `ResourceConflict`. Provided managed auth Secret
  identity is based on password material, not Secret resourceVersion metadata.
- Managed Postgres bootstrap-affecting inputs are immutable after acceptance:
  image, database name, user, port/password key, auth Secret name, and password
  material are captured in the managed Postgres identity stored on the owned
  PVC/template. Drift is rejected before owned Postgres children are mutated.
- `CardanoDBSync` runtime status now includes bounded progress probes. The
  controller probes Postgres connectivity/latest `block` progress as soon as DB
  runtime inputs resolve, compares that progress with the referenced
  `CardanoNetwork.status.endpoints.ogmios.url` node tip once workloads are
  healthy, populates `status.sync`, sets `PostgresReady` from live DB
  connectivity, sets `Synced=True` only within the package-local lag threshold,
  and sets aggregate `Ready=True` only when follower node, db-sync container,
  Postgres, and sync status are all ready.
- `CardanoDBSync.spec.placement.mode` defaults to `dedicatedFollower`.
  `primarySidecar` is a real runtime path for local networks and non-mainnet
  public profiles: DB Sync owns database/config/pgpass/state/metrics/status and
  publishes
  `status.placement.primarySidecar` only when `SidecarMaterialReady=True`, while
  CardanoNetwork is the only controller that composes the primary Pod from that
  status contract. Multiple primary-sidecar claims for one CardanoNetwork use
  deterministic incumbent selection: oldest non-deleting `primarySidecar` claim
  by creation timestamp, then UID, then namespace/name remains attachable; later
  peers report `PlacementConflict` on their own CardanoDBSync status and do not
  detach the incumbent. Once db-sync state accepts a placement, later
  `primarySidecar` <-> `dedicatedFollower` changes are rejected with
  `UnsupportedDatabaseIdentityChange`; the old pod-drain handoff guards remain
  to prevent duplicate processes during pre-acceptance and cleanup paths.
- Public `CardanoDBSync` supports `dedicatedFollower` and `primarySidecar` for
  preview, preprod, and custom public profiles. Public mainnet db-sync remains
  rejected until a follower-node Mithril bootstrap or public mainnet
  `primarySidecar` sizing/bootstrap slice is implemented.
- Public mainnet db-sync should likely start as `primarySidecar` plus managed
  Postgres db-sync snapshot restore, not as a dedicated follower. Upstream
  db-sync snapshots restore both PostgreSQL and db-sync ledger state via
  `postgresql-setup.sh --restore-snapshot`; they are schema/version and
  architecture sensitive, so restore metadata must become part of YACD's
  accepted database identity. As of session 028, official mainnet 13.7 snapshots
  were about 79GB compressed before expanded Postgres data, db-sync state,
  scratch space, and growth, so the current 10Gi db-sync/Postgres defaults are
  not mainnet-safe. Re-check current upstream release and snapshot details
  before implementing.
- `CardanoNetwork` publishes `DBSyncAttachmentReady` only to explain primary Pod
  impact from an attached/requested db-sync sidecar. Detailed DB Sync health
  remains on `CardanoDBSync`. Shared primary Pod names, selector labels, port
  defaults, port names, and port ownership rules live in
  `internal/cardano/primarypod`; do not duplicate that vocabulary inside either
  controller.
- The faucet/topup path should stay narrow and use Ogmios for chain
  interaction. Avoid turning it into a general wallet platform.
- `CardanoNetwork` owns the primary faucet auth Secret when faucet is enabled
  and watches those owned Secrets directly. The reconciler still uses live
  Secret reads for faucet auth apply/readiness, then stamps
  `yacd.meigma.io/faucet-auth-token-hash` on the primary Deployment pod
  template from `data.token` so Secret repair or valid token rotation rolls the
  primary Pod. Keep this side-effecting behavior out of the pure
  `primaryWorkloadBuilder`.
- The local dev stack builds the faucet image through the `faucet-image` Tilt
  local resource, which runs the ko helper and loads
  `ghcr.io/meigma/yacd/faucet:tilt` into `kind-yacd-dev`. Keep this explicit:
  the faucet image appears as a manager default flag, not as a Kubernetes image
  reference that Tilt can discover from rendered YAML.
- Faucet workload containers should leave `command` empty and rely on the image
  entrypoint. This keeps ko-built development images and release Dockerfile
  images compatible.
- The companion CLI now lives under `cli/`. It uses Cobra/Viper, builds the
  release binary from `./cli/cmd/yacd`, and keeps the operator manager image
  entrypoint on `./cmd`.
- Test-harness Phase 1 (session 036, PR #58) made environment identity a
  command-line concern. The verbs are keyed on `NAME [-n ns]`, with `-n`/
  `--namespace` defaulting to `NAME` (DNS-1123-validated via
  `cli/internal/cli/identity.go:resolveIdentity`): `yacd up NAME -f yacd.yaml`
  (auto-creates + ownership-stamps the namespace via SSA `EnsureNamespace`,
  applies, waits Ready — `--wait` defaults TRUE, `--timeout` 12m; replaces the
  old `deploy`), `yacd down NAME` (delete + `WaitGone`, idempotent on NotFound,
  `--timeout` 5m), `yacd list [-A|-n] [--json]` (projects name/namespace/mode/
  ready/endpoints). `info`/`topup` adopt the same NAME-default-namespace model.
- BREAKING (safe pre-1.0): the developer `Environment` document DROPPED its
  `metadata` block — identity comes only from the CLI. `Load` uses
  `yaml.UnmarshalStrict`, so any spec still carrying `metadata:` fails to parse.
  `render.CardanoNetwork(env, name, namespace)` takes identity as params. All
  `examples/*/yacd.yaml` Environments and the Chainsaw e2e use the new `up` form.
- The CLI's `kube.Client` port (`cli/internal/kube`) carries
  `ApplyCardanoNetwork`, `GetCardanoNetwork`, `GetSecretValue`,
  `DeleteCardanoNetwork`, `ListCardanoNetworks`, `EnsureNamespace`, and
  `DefaultNamespace`; package helpers `WaitReady`/`WaitGone` poll through the
  port, and `ErrNotFound`+`IsNotFound` are the single source of not-found
  semantics (`GetCardanoNetwork` wraps `ErrNotFound`). `EnsureNamespace` stamps
  `app.kubernetes.io/managed-by=yacd` + `yacd.meigma.io/created-by=yacd-cli`.
- `up` rejects real applies of `spec.network.public.profile: mainnet` unless
  `--allow-mainnet`; `--dry-run` may render mainnet without the flag but warns.
  The developer config is `apiVersion: yacd.meigma.io/devconfig/v1alpha1`,
  `kind: Environment`, `spec.network` shaped as `api/v1alpha1.CardanoNetworkSpec`
  decoded into the concrete API type (so omitted CRD-defaulted fields are
  rejected, not zero-rendered).
- `up --wait`/readiness must only trust `Ready`/`Degraded` conditions whose
  `observedGeneration` is at least the current generation (`FreshCondition`);
  otherwise an updated already-ready resource reports stale success.
- The test-harness design docs live at the `.journal/` root (moved out of
  `.journal/030/`): `TEST_HARNESS_PROPOSAL.md` (decided design — fresh-build
  lifecycle, identity-as-CLI-arg, the `up/down/list/connect/run/exec` verb set,
  the `YACD_*` env-var contract, and a `yacd-env` GitHub Action),
  `TEST_HARNESS_PLAN.md` (phased work), `TEST_HARNESS_DESIGN.md` (the
  adversarial-workflow analysis and rejected alternatives, incl. why a bespoke
  snapshot format was deferred in favor of fresh-build), and
  `TEST_HARNESS_PHASE0_RESULTS.md` (the Phase 0 go/no-go evidence). Phases 0–2
  are done; Phases 3 (release), 4 (the `yacd-env` Action), and 5 (examples +
  how-to) remain.
- Test-harness Phase 2 (session 041) added the host-access verbs and the
  `YACD_*` contract. `yacd run NAME -- cmd` (scoped client-go port-forwards +
  inject `YACD_*` + host exec, propagates the child exit code; no cmd ⇒
  `$SHELL`), `yacd connect NAME` (foreground supervised forwards, writes
  token-free `.yacd/<network>/endpoints.json` at 0600, re-establishes on the
  next use after a drop), and `yacd exec NAME -- cmd` (in-pod, argv-only via an
  `env KEY=VAL` prefix — never a shell, so `$VAR` is not expanded — for
  socket-bound `cardano-cli`). `yacd topup --await` polls Kupo (vendored `kugo`)
  for the funded UTxO; it requires `--kupo-url` or `YACD_KUPO_URL` and does not
  self-forward. The verb docs + the versioned `YACD_*` table live in
  `docs/host-access.md`. Key contracts: the CLI resolves the primary Pod from
  the published node-to-node Service selector (no `internal/...` import) and
  pins the node container name + `/ipc/node.socket` as CLI-local constants;
  host URL schemes are parsed from the published status URL (Ogmios stays
  `ws://`); `YACD_FAUCET_TOKEN` is host-only and never set in-pod or written to
  `endpoints.json`. The host-access methods (`PrimaryPodName`/`Forward`/`Exec`)
  hang off the existing `kube.Client` port; `Forward`/`Exec` need a live kubelet
  so they are proven by manual/e2e, not envtest.
- KNOWN FLAKE: `TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync`
  (`internal/controller/cardanonetwork/controller_envtest_test.go`) is a
  load-sensitive manager-backed envtest whose `Eventually` ("Condition never
  satisfied") intermittently fails under CI load — it blocked merges on PR #61
  (1×) and PR #67 (2× in a row, green on the 3rd). It is unrelated to the change
  under test; rerun the `ci` job, and consider a de-flake (longer wait / sturdier
  condition) as a standalone PR.
- Test-harness Phase 0 is **done — GO** (session 036). A throwaway hosted-runner
  spike proved KinD + operator + a representative local `CardanoNetwork`
  (Ogmios+Kupo+faucet) cold-starts to `Ready` in ~27s (full pipeline ~112s) vs
  the 10–12m budget; `delete cardanonetwork` GC's all 11 owner-referenced
  children in ~3s with no finalizers; and the `run` (host port-forward) and
  `exec` (in-pod `cardano-cli` over `/ipc/node.socket`) host-access paths both
  work and agree on the chain tip. Measured on a 4 vCPU/16 GB `ubuntu-latest`
  (public-repo runners were upgraded from 2 vCPU/7 GB); the 2-core private tier
  is untested. The e2e now runs in CI (see the e2e-job bullet below); when
  Phase 4 builds the dedicated `yacd-env` action, also preload Ogmios/Kupo
  (Docker Hub rate-limit jitter) and validate on a consumer's 2-core tier.
- The KinD/Chainsaw e2e smoke now runs in CI as the `e2e` job in
  `.github/workflows/ci.yml` (`moon run root:test-e2e`, ~8m on `ubuntu-latest`,
  full smoke incl. CardanoDBSync managed Postgres). Landed in PR #55
  (`0bb852d`). It had NEVER run on Linux before (only macOS locally), which
  hid three CI-only issues, each masking the next:
  1. The root `.dockerignore` ignored everything and re-included only
     `**/*.go` + `go.{mod,sum}`, stripping the embedded
     `internal/cardano/publicnet/profiles/**` assets, so the manager
     `docker build` failed `//go:embed profiles/mainnet/*`
     (`no matching files found`). Fixed by re-including the profiles. This same
     bug also broke the manager image build in `release.yml` / release-dry-run
     (the `release 1.0.0` dry-run was red on both arches) — so keep the
     `.dockerignore` re-includes in sync with any new `//go:embed` the manager
     gains.
  2. The chainsaw test shells out to `moon run root:deploy`/`undeploy`, which
     were `runInCI:false`; Moon filters those under `CI=true` ("No tasks
     found"). Both are now `runInCI:true` (dev-up/dev-down stay false).
  3. Chainsaw runs script `content` via `/usr/bin/sh` — dash on Linux, bash on
     macOS — so `set -euo pipefail` was rejected. All inline scripts are now
     `set -eu` (POSIX). Keep new chainsaw script steps dash-portable.
  IMPORTANT: the manager image's PRODUCTION build path is `docker build .` (root
  `Dockerfile` via `docker/build-push-action` in `release.yml`), NOT ko — ko
  (`.dev/ko-build.sh`) is only the dev-stack/Tilt build. A `.dockerignore`/embed
  regression breaks releases, not just the e2e.
- Root `DESIGN.md` captures the current high-level architecture; `.journal/PLAN.md`
  captures the rough component sequence for the initial prototype.
- PR #3 introduced the first real API group/version with
  `yacd.meigma.io/v1alpha1` and the namespaced `CardanoNetwork` CRD. The draft
  uses `spec.mode: local|public`; public networks use `profile:
  preprod|preview|mainnet|custom`, and custom public profile data is limited to
  same-namespace ConfigMap/Secret refs through `corev1.LocalObjectReference`.
- Local-mode `primaryWorkloadBuilder` maps network magic, pool count,
  slot/epoch timing, and node version into `internal/cardano/localnet.Spec`.
  Public-mode `primaryWorkloadBuilder` resolves `internal/cardano/publicnet`
  profiles and renders a passive public node plus Ogmios, with public Kupo and
  faucet still rejected. Curated public profiles are embedded for preview,
  preprod, and mainnet; custom profiles come from same-namespace ConfigMap or
  Secret bundles.
- TEST_REPORT F0 is still open: public mainnet cannot currently be created
  because the public profile artifacts are copied into one raw
  `<network>-network-artifacts` ConfigMap and mounted directly as `/profile`,
  while the mainnet bundle exceeds Kubernetes' 1 MiB ConfigMap data cap. A
  session-040 size check showed the checked-in mainnet profile files gzip below
  the cap, but the preferred next direction is not a naive compressed ConfigMap:
  make public networks materialize profile files in an init path closer to the
  localnet artifact publisher, then publish only compact/non-secret artifact
  metadata for consumers.
- Mainnet `CardanoNetwork` requires `spec.public.bootstrap.mithril` for this
  slice. The default Mithril client image is
  `ghcr.io/input-output-hk/mithril-client:main-2478748`, the default snapshot
  is `latest`, and the init container uses the release-mainnet aggregator plus
  vendored verification keys. Mainnet primary PVC storage defaults to `500Gi`,
  explicit mainnet storage below `300Gi` is rejected, and omitted primary node
  resource requests default to `cpu: 2` and `memory: 24Gi`.
- `internal/cardano/localnet` is the pure Go, Kubernetes-free boundary for
  `cardano-testnet create-env` inputs. It returns a deterministic invocation,
  expected output layout, fingerprint, and JSON-serializable manifest for later
  init-container idempotency.
- `containers/cardano-testnet` is the YACD tools image for official
  IntersectMBO `cardano-node` release artifacts. Its Release Please component
  uses tags like `cardano-testnet/v11.0.1-yacd.1`; the OCI image tag is the
  full `11.0.1-yacd.1`, while the release workflow strips the `-yacd.N` suffix
  to download upstream Cardano artifacts.
- The current published artifact-capable tools image is
  `ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.4`. Future packaging-only
  fixes should bump `yacd.N`; future upstream Cardano bumps should move the
  base version and reset the YACD packaging revision.
- The IOG cardano-node 11.0.1 Linux release binaries are **fully static musl**
  builds (GHC 9.6.7): `ldd` says "not a dynamic executable", no `PT_INTERP`/
  `PT_DYNAMIC`/`GLIBC_` symbols, and the tarball has zero `.so` files. So any
  image embedding them needs no glibc/loader/libsodium/secp256k1/blst/etc., and
  `gcr.io/distroless/static-debian12:nonroot` is a valid base (it supplies the
  only remaining needs: a CA bundle for outbound HTTPS, a nonroot identity,
  `/tmp`, tzdata). `cardano-testnet create-env` runs shell-less (it direct-execs
  `cardano-cli` via the `CARDANO_CLI` env var, no `/bin/sh`). The release tarball
  ships ~14 binaries (~1.2GB); cardano-tools only needs `cardano-node`/
  `cardano-cli`/`cardano-testnet` (~370MB), so copy only those. `cardano-tools`'s
  Dockerfile (PR #64 follow-up) does exactly this: distroless/static base + 3
  binaries → ~442MB vs ~1.3GB. "Static release tarball" is a load-bearing
  assumption — re-run the ELF check on both arches on every cardano-node bump;
  if IOG ever ships glibc-dynamic, the base must change to distroless/cc. The
  existing `cardano-testnet` image is still debian-slim + all binaries and could
  get the same treatment later.
- `containers/cardano-tools` (binary `yacd-cardano-tools`, merged in PR #64) is
  the single utility for Cardano artifact operations and the foundation for the
  F0 fix. It is part of the ROOT module (no `go.mod`), so it imports the shared
  contract directly (`internal/cardano/{networkartifacts,localnet,publicnet}`,
  `internal/ctrlkit/artifacts`, `internal/controller/annotations`) instead of
  duplicating it like `cardano-testnet/publisher`. Subcommands: `generate`
  (shim cardano-testnet create-env, idempotent: match→re-enrich, conflict→refuse),
  `fetch` (download public profiles from the pinned operations book + Mithril
  config; `config.json`+`topology.json` digest-pinned, genesis/checkpoints
  verified downstream, peer-snapshot unpinned; writes artifact-contract names
  config.json→configuration.yaml, topology.json→primary-topology.json; refuses
  HTTP redirects), `serve` (default-deny allowlist of networkartifacts keys over
  HTTP for out-of-cluster consumers), `report` (publish a localnet artifact dir
  to the network ConfigMap — localnet-only; rebased on the shared contract, its
  `report-dry-run` golden reproduces the publisher's sha256 to lock verifier
  compatibility), `version`. The controller does NOT yet use this image; wiring
  it (and removing the manager `//go:embed`) is the deferred F0 work — see
  `.journal/042/SUMMARY.md` Next Steps.
- The active `cardano-testnet` publisher enriches `configuration.yaml` with
  genesis hashes in `containers/cardano-testnet/publisher/internal/artifacts`.
  It shells out to the image-owned `cardano-cli` as a narrow adapter because
  that CLI is the canonical Cardano release tool already shipped in the tools
  image; keep the Cobra command layer thin.
- The `cardano-testnet` init-container fragment belongs in
  `internal/controller/cardanonetwork`, not `internal/cardano/localnet`. It
  calls the image-owned `/opt/yacd/bin/yacd-cardano-testnet-init` wrapper,
  passes the compact plan manifest through env, and expects a writable
  `localnet-state` volume mounted at the plan state directory.
- Local-mode `CardanoNetwork` now owns a same-namespace
  `<network>-network-artifacts` ConfigMap containing exact non-secret generated
  localnet files for follower controllers: node configuration, genesis files,
  primary topology, `yacd-localnet-plan.json`, and `connection.json`. The
  controller publishes `status.artifacts` only after it verifies the schema
  annotation, localnet fingerprint annotation, exact `sha256:<64 hex>` data
  hash, required keys, no `binaryData`, and no unsupported data keys beyond the
  optional `dijkstra-genesis.json`.
- The localnet init path publishes artifacts through a dedicated
  `<network>-artifact-publisher` ServiceAccount whose Role is limited by
  `resourceNames` to `get`/`patch` only the network artifact ConfigMap. The
  primary Deployment disables pod-level token automount; only the init container
  receives a projected token/CA/namespace volume.
- If a published owned artifact ConfigMap fails verification, the
  `CardanoNetwork` controller may delete and recreate it to force local
  artifact republish through the primary init container. That recovery roll is
  throttled by the Deployment metadata annotation
  `yacd.meigma.io/network-artifacts-recovery-rollout-at`; while cooldown is
  active the controller leaves the corrupted ConfigMap in place, preserves the
  previous pod-template ConfigMap UID, reports `ArtifactsReady=False`, and
  requeues for the remaining cooldown. If deletion is held by a finalizer,
  recreation is deferred until the object actually disappears.
- The manager Helm chart is intentionally cluster-scoped for the current
  local/dev operator. Treat the manager ServiceAccount as trusted cluster
  automation for YACD-managed namespaces; namespace-scoped manager mode is a
  future hardening path.
- A `CardanoNetwork` localnet is stable for its lifetime. The accepted network
  identity is read from owned runtime material: the primary node PVC is
  authoritative, with the primary Deployment pod-template annotations as a
  fallback only when the PVC is absent. `status.network.*Fingerprint` is
  derived display state and must not be used as an acceptance source. If
  localnet inputs drift after acceptance, reconcile stops before Deployment
  updates and sets a degraded condition. If the accepted primary state PVC is
  missing, reconcile reports `PrimaryStateLost` and does not recreate it; delete
  and recreate the CR/PVC to intentionally start fresh or change localnet
  parameters.
- Primary PVC reconciliation allows storage expansion when the accepted
  fingerprint matches, rejects storage shrink and requested storage class
  drift, and refuses unowned or foreign-owned same-name children rather than
  adopting them silently. If the live primary PVC is terminating, reconcile
  reports `ChildBeingDeleted` with blocking finalizers and does not mutate other
  primary children.
- Rejected PVC expansion from Kubernetes `Forbidden` / `Invalid` update errors
  is surfaced as `StorageExpansionRejected` rather than returned as a raw
  reconcile error. The shared mapper lives in `internal/controller/storage`,
  is invoked through `ctrlkit/apply.ApplyOwnedObject`'s persistence-error hook,
  and covers the `CardanoNetwork` primary PVC plus `CardanoDBSync` state,
  follower, primary-sidecar, and managed Postgres PVC paths.
- Shared controller mechanics now live in `internal/ctrlkit`: naming,
  metadata/ownership, owned-child apply, artifact data hash/key validation,
  readiness predicates, resource mutation helpers, status error/condition
  helpers, and storage drift detection. Keep `ctrlkit` domain-free; YACD
  annotation keys and condition-message mapping belong under `internal/controller`,
  while Cardano artifact schema/key contracts belong under `internal/cardano`.
- Owned-child reconciliation should prefer `ctrlkit/apply.ApplyOwnedObject` for
  create/read/controller-owner/validate/mutate/persist flows. Callbacks are the
  field-ownership boundary: create uses the defaulted desired object directly,
  while `Validate` and `Mutate` only run for existing owned objects.
  `ValidateCreate` is the hook for refusing unsafe recreation, and
  `ObjectDeleting` is the hook for fail-closed status when an owned child has a
  deletion timestamp.
- The primary node Service uses the same safe name as the Deployment
  (`<safe CardanoNetwork name>-node`), targets the named `node-to-node`
  container port, preserves Kubernetes-assigned cluster IP fields, and refuses
  unowned or foreign-owned same-name Services.
- `status.endpoints.nodeToNode` is the canonical in-cluster discovery contract
  for the primary node. It publishes `serviceName`, `port`, and a fully
  qualified `tcp://<service>.<namespace>.svc.cluster.local:<port>` URL.
- The Ogmios Service uses `<safe CardanoNetwork name>-ogmios`, selects the
  primary-node Pod labels, targets the named `ogmios` port, and is deleted when
  `spec.chainAPI.ogmios.enabled=false`. `status.endpoints.ogmios` publishes a
  fully qualified `ws://<service>.<namespace>.svc.cluster.local:<port>` URL.
- The Kupo Service uses `<safe CardanoNetwork name>-kupo`, selects the
  primary-node Pod labels, targets the named `kupo` port, and is deleted when
  `spec.chainAPI.kupo.enabled=false`. Kupo defaults to enabled when Ogmios is
  enabled, uses `cardanosolutions/kupo:v2.11.0`, runs with `--prune-utxo`,
  bounded ephemeral storage, and publishes
  `http://<service>.<namespace>.svc.cluster.local:<port>` through
  `status.endpoints.kupo`.
- The faucet is opt-in through `spec.chainAPI.faucet`, requires Ogmios and Kupo
  when enabled, and publishes `status.endpoints.faucet` plus
  `status.faucet.authSecretName`. The controller creates an owned
  `<network>-faucet-auth` Secret, mounts only `/state/env/utxo-keys` into the
  sidecar, and uses live API reads plus periodic requeues instead of Secret
  watches/list RBAC.
- `yacd topup` reads the faucet auth Secret and posts to the faucet endpoint.
  Custom non-loopback `--faucet-url` values require explicit trust flags before
  the CLI sends the Secret token outside the status-published destination.
- The faucet transaction path uses Apollo with Ogmios and Kupo today. This
  brings in `github.com/SundaeSwap-finance/ogmigo/v6`, which Kusari flags
  because it depends on the discontinued Gorilla WebSocket toolkit; no called
  vulnerabilities were reported by `govulncheck`, but replacing or upstreaming
  that Ogmios client dependency is a durable follow-up.
- `NodeReady` and `OgmiosReady` are Kubernetes-runtime conditions derived from
  live primary Pod container readiness. `NodeReady` is intentionally separate
  from the Ogmios sidecar, and aggregate `Ready` is true only when both are
  true. When Ogmios is explicitly disabled, `OgmiosReady=False` and aggregate
  `Ready=False` with reason `OgmiosDisabled`.
- Ogmios readiness uses `/bin/ogmios health-check --port <port>` for startup,
  readiness, and conservative liveness probes. The controller also enforces a
  package-local compatibility table for recognized Ogmios release tags against
  `spec.node.version`; the default `cardanosolutions/ogmios:v6.14.0` and
  `cardano-node` `11.0.1` pair is manually and Chainsaw-smoke validated with
  `queryNetwork/tip` on localnet.
- The Chainsaw manager smoke now includes an installed-operator proof that a
  representative local-mode `CardanoNetwork` creates primary resources,
  publishes node-to-node/Ogmios endpoints and artifact status, reaches
  `Ready=True`, returns a real Ogmios `queryNetwork/tip` result through the
  Service, then disables optional services and verifies owned resources and
  endpoint/status cleanup.
- The repo-local development stack is managed by `moon run root:dev-up` and
  `moon run root:dev-down`. The stack uses `.dev/` tooling, shared
  `.run/yacd-dev` runtime state, Kind context `kind-yacd-dev`, and Tilt port
  `10350`; implementation sessions should start it once after selecting an
  implementation worktree, keep it running across ordinary turns and review
  pauses, and stop it at explicit session close/end-of-session unless the human
  asks otherwise.
- The CLI lives under `cli/` and ships the `yacd` binary built from
  `./cli/cmd/yacd`. Its packages follow the same readability / hexagonal /
  typed-vocabulary discipline as the controller packages: each has a
  `doc.go` contract; `kube` carries the `Client` port + `Adapter`
  implementation (`kube.NewClient` returns `*Adapter` per Rule 7) plus
  the typed `ConditionType` vocabulary (`ConditionReady`,
  `ConditionDegraded`, `ConditionFaucetReady`); the `cli` package
  decomposes into per-command files (`deploy.go`, `info.go` +
  `info_print.go`, `topup.go` + `topup_trust.go` + `topup_transport.go`)
  plus `options.go` / `config.go` / `root.go`.
- `topup_trust.go` is security-load-bearing: `validateFaucetURLTrust`
  defends three attack vectors (token exfiltration to attacker-supplied
  URL, accidental non-loopback exposure, plaintext eavesdropping) and
  carries paragraph + per-check comments. Tests preserve the invariant
  via `mock.AssertNotCalled(t, "GetSecretValue", ...)` — do not delete
  this assertion when touching the trust gate.
- `devconfig.Load` runs a two-pass validation. Pass 1 (`Validate`)
  checks the decoded Go envelope; pass 2 (`validateExplicitFields`)
  re-decodes the raw YAML into a map and enforces that
  surprising-when-defaulted fields are spelled out explicitly. Both
  are required because the typed decoder cannot distinguish "absent"
  from "zero" on the strongly-typed API value.
- Mockery + Testify are the test stack. Mockery v3 is pinned via proto
  at `.moon/proto/mockery.toml` and `.prototools`; `.mockery.yml` at
  the repo root drives generation. Mocks live in `cli/internal/mocks`
  for the cli ports (`Client`, `HTTPDoer`). Regeneration goes through
  `moon run root:generate`. The Moon task prepends the direct Go
  toolchain bin to PATH because the proto `go` shim word-splits the
  templated `-f "{{context.GOARCH}} {{context.Compiler}}"` argument
  `golang.org/x/tools/go/packages` passes to `go list`; without the
  workaround mockery (and any other x/tools-based generator) errors
  with `malformed import path "{{context.GOARCH}}"`.
- The `cardano-testnet` tools image has an override seam for the
  primary cardano-node, create-env init, faucet source-address init,
  and CardanoDBSync follower-node containers. The manager flag
  `--default-cardano-testnet-image` (chart value
  `cardanoTestnet.image.{repository,tag,digest}`) overrides the
  computed `<repo>:<toolVersion>-<revision>` reference on all four
  containers in both controllers. Empty leaves the built-in
  `yacd.N` revision in place. The dev stack uses this seam to rebuild
  the tools image from local source through `.dev/build-cardano-testnet.sh`
  and load it as `:tilt`. Use this whenever the published cardano-testnet
  tag lags publisher code downstream controllers depend on (notably
  PR #31's `EnrichGenesisHashes`, which is required by db-sync but was
  not published in `yacd.4`).
- Faucet auth Secret repair is governed by
  `faucetSecretRepairRequeueAfter = 10 * time.Minute` in
  `internal/controller/cardanonetwork/controller.go`. The controller
  does not watch Secrets directly (avoiding list RBAC), so externally
  deleted faucet auth Secrets are only repaired on the next periodic
  requeue. Practical recovery for the dev loop is to restart the
  manager pod; the regenerated Secret carries a new token, which
  silently invalidates any previously cached topup credentials.
- `revokePrimaryFaucetExposure` (`internal/controller/cardanonetwork/delete.go`)
  is invoked on the `UnsupportedSpec` rejection path
  (`controller.go:93`) and tears down the faucet Service, faucet auth
  Secret, and the faucet container/init-container/volumes from the
  live primary Deployment. This is intentional security behavior: when
  the controller cannot validate the spec, it refuses to leave a
  published auth token in flight. Disabling `kupo` while `faucet` is
  enabled is the most common path that triggers this; the clean
  cascade is to disable both in a single patch.
- Generated managed-Postgres auth Secret recovery now has a narrow adoption
  path: after accepted managed Postgres identity exists, a plain unowned
  same-name Secret restored with the original `data.password` is adopted and
  annotated; wrong password material is rejected as
  `UnsupportedDatabaseIdentityChange`, and foreign-owned same-name Secrets
  remain `ResourceConflict`.
- Known-issues catalog from the session-029 break-pass lives in
  `.journal/TEST_REPORT.md`. A3, A4, B1, B2, B6, D1, D2, and D6 have been fixed
  in later sessions. Remaining findings with concrete reproductions and
  suggested fixes include F0 and F2/F4; consult the report before touching the
  relevant code paths.
