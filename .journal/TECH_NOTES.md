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
- `CardanoNetwork.status.sync` is the non-external primary-node sync visibility
  surface. It is derived from the verified network artifact ConfigMap's
  `shelley-genesis.json` timing plus the owned Ogmios `/health` endpoint, and
  publishes source/connection status, tip, last tip update, observedAt, Ogmios
  `networkSynchronization`, inferred tip slot, and lag slots/seconds.
  `NodeSynchronized` and `NodeProgressing` are visibility-only for now and do
  not feed aggregate `Ready`. Ogmios health HTTP 500 can still carry a useful
  disconnected body; `lastKnownTip`, `lastTipUpdate`, and
  `networkSynchronization` may be null during startup or unknown-tip states.
- **FAUCET REMOVAL COMPLETE + v0.2.0 (sessions 059–061) — this SUPERSEDES the
  faucet-service, developer-wallet, and v0.1.x release/digest-pin bullets
  elsewhere in this file; treat those as historical.** The in-cluster faucet HTTP
  SERVICE is gone: no `spec.chainAPI.faucet`, no `status.faucet`/faucet endpoint,
  no `FaucetReady`, no `<net>-faucet-auth` Secret, no faucet sidecar / source-
  address init, no `--default-faucet-image` / faucet image / faucet-image Tilt
  resource, no `revokePrimaryFaucetExposure` / `faucetSecretRepairRequeueAfter`.
  Also removed: the old `yacd topup` + `topup_trust.go` trust gate +
  `YACD_FAUCET_URL/TOKEN` funding role + `kube.ConditionFaucetReady`, and the
  `spec.chainAPI.wallet` developer wallet (`status.wallet`, `WalletReady`). NEW
  model: the controller generates a genesis-funded `faucet` WALLET (owned Secret
  `<net>-wallet-faucet`: ed25519 `payment.skey`/`payment.vkey` + `address`,
  funded at genesis via a `cardano-tools fund-genesis` init container) for LOCAL
  networks only (gated on `Spec.Mode == Local` alone). It is create-once (deleting
  it strands genesis funds) and intentionally NOT watched. Host-side funding is
  `yacd wallet {add,topup,list,export,remove}`, building/signing/submitting txns
  directly against Ogmios/Kupo (`internal/cardano/tx`), default source
  `--from faucet`; the faucet wallet is shown in `devnet`/`info` but excluded from
  `wallet list`. Released as **v0.2.0** (PRs #107/#108/#109/#87). The CLI now
  installs the operator by **appVersion tag** (`ghcr.io/meigma/yacd:vX`), NOT a
  pinned digest, because operator+CLI are one coupled release-please root
  component — `cli/internal/operator/values.go` `Default()` sets only the
  repository; `--set image.digest` still pins, `--set image.tag` repoints. The
  ogmigo/Apollo ws-1006 genesis-config warning during funding is non-fatal
  (issue #110). See `.journal/061/SUMMARY.md`.
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
  `--timeout` 5m), `yacd list [-n] [--json]` (projects name/namespace/mode/
  ready/endpoints). `info`/`topup` adopt the same NAME-default-namespace model.
  UPDATE (session 057, PR #93): `yacd list` now defaults to **all namespaces**
  (`-A` removed; `-n` scopes to one). Rationale: the one-env-per-namespace model
  made the namespace-scoped default routinely empty (`yacd devnet`→`yacd list`
  showed nothing). Empty namespace was already the adapter's all-namespaces
  convention (`kube/client.go`); `list` no longer consults `DefaultNamespace()`.
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
  token-free endpoint state at 0600 under `.yacd/<network>/endpoints.json` for
  the default `namespace == name` case or `.yacd/<namespace>/<network>/endpoints.json`
  for namespace overrides, removes stale state on clean disconnect/drop, then
  re-establishes with fresh ports after a drop), and `yacd exec NAME -- cmd`
  (in-pod, argv-only via an
  `env KEY=VAL` prefix — never a shell, so `$VAR` is not expanded — for
  socket-bound `cardano-cli`). `yacd topup --await` polls Kupo (vendored `kugo`)
  for the funded UTxO; malformed Kupo URLs fail before cluster reads or faucet
  submission. UPDATE (session 057, PR #93): topup now **self-forwards** —
  see the dedicated topup bullet below; `--await` no longer requires `--kupo-url`
  standalone. The verb docs + the versioned `YACD_*` table live in
  `docs/host-access.md`. Key contracts: the CLI resolves the primary Pod from
  the published node-to-node Service selector (no `internal/...` import) and
  pins the node container name + `/ipc/node.socket` as CLI-local constants;
  host URL schemes are parsed from the published status URL (Ogmios stays
  `ws://`); `YACD_FAUCET_TOKEN` is host-only and never set in-pod or written to
  `endpoints.json`. The host-access methods (`PrimaryPodName`/`Forward`/`Exec`)
  hang off the existing `kube.Client` port; `Forward`/`Exec` need a live kubelet
  so they are proven by manual/e2e, not envtest.
- DE-FLAKED (session 047, PR #77): `TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync`
  (`internal/controller/cardanonetwork/controller_envtest_test.go`) was a
  load-sensitive manager-backed envtest whose `Eventually` ("Condition never
  satisfied") failed under CI load (PR #61 1×, #67 2×, #77 3× in a row). Its tight
  10s `Eventually` waits + its two deployment-assertion helpers were bumped to 1m
  (matching the test's own teardown timeout); other tests' 10s waits were left
  alone. If a sibling manager-backed envtest starts flaking the same way under CI
  load, apply the same bump.
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
- TEST_REPORT F0 (public mainnet cannot be created: the raw
  `<network>-network-artifacts` ConfigMap exceeds Kubernetes' 1 MiB cap) is being
  fixed by a redesign where the manager is NOT an authoritative config source: local
  generates / public fetches configs onto the node PVC, `cardano-node` reads from the
  PVC, and every OTHER consumer fetches over HTTP from an always-on cardano-tools
  `serve` sidecar; integrity/discovery via a served `manifest.json`. Lands as
  **PR-A → PR-C → PR-B → PR-D** (order matters — A→B→C→D bricks db-sync). PR-A
  (session 046, PR #75, `c61e0a6`) + **PR-C (session 047, PR #77, `231ccde`) are
  DONE+merged.** PR-A: cardano-tools `stage`/`fetch` produce a flat served dir
  (`/state/artifacts` on the node PVC), `servedArtifactsInitContainer` populates it,
  an always-on `serveContainer` (:8090, `/manifest.json` readiness) exposes it, owned
  `<net>-artifacts` Service publishes `status.endpoints.artifacts`. PR-C: CardanoDBSync
  fetches over HTTP via a new `cardano-tools sync` verb (pkg `artifactsync`) — a
  `network-artifacts-sync` init (emptyDir) for dedicatedFollower, a staged-PVC subPath
  mount for primarySidecar; `cardano-tools stage` now enriches `configuration.yaml`
  genesis hashes (db-sync needs them); custom-public still used the ConfigMap.
  **PR-B1 (session 048, PR #78, `606d800`) + PR-B2 (session 048, PR #79, `22a5e8f`) are
  DONE+merged.** PR-B1 removed the `<net>-network-artifacts` ConfigMap concept and
  custom-public ENTIRELY: dropped API `profile: custom` + `NetworkConfigSource` +
  `Status.Artifacts`; deleted the ConfigMap producer/publisher/artifact-publisher RBAC +
  `public_profile_source.go` + publicnet `CustomBundle`; repointed curated-public node
  **and ogmios** to read `/state/artifacts`; re-sourced `ArtifactsReady` (serve-container
  readiness via `primaryArtifactsReadyCondition`), **sync-timing (the probe HTTP-fetches
  shelley-genesis.json from `status.endpoints.artifacts.url`** via a `cardanoNetworkTimingProber`
  seam — no new API field), and db-sync identity (→ `Status.Network` fingerprint selected
  by Mode via `networkIdentityFingerprint`, a one-time accepted
  `UnsupportedDatabaseIdentityChange`); db-sync is single-path serve. YACD now supports
  only local + curated-public. PR-B2 deleted the dead `cardano-testnet` artifact publisher
  (nested module + `yacd-cardano-testnet-publisher` wrapper cmd + `internal/artifactpublisher`
  + the Dockerfile publisher stage + the init-wrapper `publish_artifacts`) and bumped the
  manager default cardano-testnet revision yacd.4→yacd.5; a follow-up (PR #80, `7c13d14`)
  pinned the next release to `11.0.1-yacd.5` (see the cardano-testnet versioning bullet).
  **PR-D (session 050, PRs #81 + #76 + #82) is DONE+merged — the F0 series is COMPLETE.**
  #81 removed the dead cardano-tools `report` verb (+ its `internal/kube`/config loader/golden);
  #76 released `cardano-tools 11.0.1-yacd.5` (publisher-/report-free, with stage/serve/sync);
  #82 pinned the manager's `toolsimage` default to that published digest
  (`@sha256:d3283ca5fc925f6ec01f61a54371e5ad1934088614b7cde1e1e1915424662fc4`) and dropped the
  cardano-tools e2e build+load (Kind now pulls both tools images). DESIGN.md needed no rewrite
  (no stale ConfigMap prose). Remaining/optional: digest-pin the cardano-testnet image for parity;
  the root operator release PR #7 (`yacd 1.0.0`) is open awaiting a deliberate release decision.
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
- The cardano-testnet image's artifact PUBLISHER is GONE (session 048, PR-B2):
  no more `publisher` nested module / `yacd-cardano-testnet-publisher` binary /
  `internal/artifactpublisher` / Dockerfile publisher stage / init-wrapper
  `publish_artifacts`. The image now ships only cardano-node/cli/testnet + the
  `yacd-cardano-testnet-init` create-env wrapper. The last PUBLISHED tag is still
  `11.0.1-yacd.4` (with the publisher); the manager default is now `yacd.5`
  (publisher-free), and release-please PR #34 (`cardano-testnet 11.0.1-yacd.5`) is
  the pending release that, when merged, cuts+publishes the slimmer image.
- **release-please `Release-As:` MUST be component-scoped when the commit spans
  components** (session 050 mistake). The root `yacd` component
  (`release-please-config.json`) includes everything EXCEPT
  `containers/cardano-testnet` + `containers/cardano-tools` (its only
  `exclude-paths`). So a commit that touches BOTH a container dir and any other
  path (e.g. `.dev/scripts/test-e2e.sh`) counts toward the container component
  AND root. An **unscoped** `Release-As: <ver>` footer then applies to EVERY
  component the commit touches — in session 050 it leaked `11.0.1-yacd.5` into the
  root release PR #7 (which was heading to `1.0.0`). #80 got away with unscoped
  only because it touched cardano-testnet paths exclusively. **Fix/use the
  component-scoped form `Release-As: <package-name>@<version>`** (proven in repo
  history: `Release-As: cardano-testnet@11.0.1-yacd.4`). Package-names:
  `yacd` (root), `cardano-testnet`, `cardano-tools`. The latest commit's
  applicable Release-As wins for a component's pending window.
- **cardano-tools versioning convention** (documented in
  `containers/cardano-tools/README.md`, session 050; mirrors cardano-testnet):
  same contract — tag `<cardano-node-version>-yacd.<N>`, base = packaged
  cardano-node version, set each release via a `Release-As:` footer (scoped if
  the commit also touches root paths). When bumping `yacd.N`, update
  `Revision`/`Digest` in `internal/cardano/toolsimage/toolsimage.go` and the
  kind-loaded tag in `.dev/scripts/test-e2e.sh`.
- **cardano-testnet versioning convention** (documented in
  `containers/cardano-testnet/README.md`, session 048): the image tag is
  `<cardano-node-version>-yacd.<N>`. The base MUST equal the packaged cardano-node
  version (the release workflow downloads cardano-node at the base; the manager
  computes the image ref as `<node version>-yacd.N`). release-please derives the
  next version from Conventional-Commit semver, which DRIFTS the base (a `feat`
  made it 11.1.0, a breaking `!` made it 12.0.0). So each cardano-testnet release
  MUST set its exact version with a `Release-As:` footer on the squash commit
  (packaging-only → `<same node version>-yacd.<N+1>`; cardano-node bump →
  `<new version>-yacd.0`); a footer on a commit touching only
  `containers/cardano-testnet/` scopes to that component (verified: PR #80's
  `Release-As: 11.0.1-yacd.5` fixed PR #34 without disturbing root #7 / cardano-tools
  #76). When bumping `yacd.N`, also update `cardanoTestnetImageRevision`
  (cardanonetwork/init_container.go), `defaultFollowerNodeImageRevision`
  (cardanodbsync/defaults.go), and the kind-loaded tag in `.dev/scripts/test-e2e.sh`.
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
  compatibility), `version`. As of session 043 the image SEAM is wired
  (PR #68): shared `internal/cardano/toolsimage` (Repository/Revision/Reference),
  a `--default-cardano-tools-image` manager flag + Helm `cardanoTools.image.*`
  value on both reconcilers, a single-arch `cardano-tools-image` PR-CI job, and a
  static-musl guard in the Dockerfile fetch stage. The first release shipped
  (PR #65): `ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.4`
  `@sha256:9ca9e03348c3f9d22408be36f1525c3ef518ab6e0b0053b0a05f2b8401a6039e`.
  The controllers still do NOT FETCH with it — the F0 transport redesign (PVC
  staging at `/state/profile`, manifest-only ConfigMap, dropping the manager
  `//go:embed`) is banked unmerged on branch `feat/f0-public-profile-pvc`
  (`internal/cardano/publicpins` + the fetch adapter) with the controller-rewrite
  slices deferred. See `.journal/043/SUMMARY.md` Open Threads + the session-043
  NOTES resume checklist; locked design: pin only config/topology/mithril (trust
  remote for genesis), fingerprint over pinned-digests+magic, custom profiles
  stay byte-based.
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
- External-access (session 060, PR #112): `spec.chainAPI.{ogmios,kupo}` gained an
  optional `service.{type,nodePort}` block (`type` ClusterIP default | NodePort;
  `nodePort` pins or 0=auto, CRD `Maximum=32767`, Go-enforced 30000 floor +
  NodePort-only coupling) and an `externalURL` string (operator-asserted reachable
  URL, lenient ws/wss/http/https + host). `externalURL` is mirrored ADDITIVELY into
  `status.endpoints.{ogmios,kupo}.externalURL` (a new field on the shared
  `ServiceEndpointStatus`, so it also appears unpopulated on db-sync
  metrics/postgres endpoints). The operator only ADVERTISES the URL; making it
  routable is the provisioner's job. Validation is Go→`UnsupportedSpec`/Degraded
  (`validateChainAPIServiceExposure`/`validateExternalURL` in
  `internal/controller/cardanonetwork/validate.go`). This is P1 of a 3-phase design
  (`.journal/060/EXTERNAL_ACCESS_DESIGN.md`); P2 (devnet k3d `--port` + pinned
  NodePort + localhost externalURL) and P3 (CLI resolver: flag > YACD_* env >
  probed status.externalURL > port-forward fallback) remain.
- `ctrlkit/resources.MutateService` now PRESERVES a Kubernetes-assigned NodePort
  (session 060): it matches desired ports to current by name and keeps the live
  NodePort when the desired Service is NodePort and the desired port leaves
  NodePort 0 (auto). A pinned desired NodePort wins; a non-NodePort (ClusterIP)
  Service takes desired verbatim, still stripping any tampered node ports.
  Previously it did `current.Spec.Ports = desired.Spec.Ports` wholesale, which
  thrashed auto-assigned NodePorts every reconcile. Keep the `desired.Type ==
  NodePort` guard — it is what keeps the cardanonetwork ClusterIP
  service-correction tests green and makes the change a no-op for db-sync.
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
- `devconfig.Load` runs validation in layers. `Validate` checks the decoded Go
  envelope, mode shape, and CLI-side runtime support for deterministic
  controller rejections knowable from the config; `validateExplicitFields`
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
- The yacd CLI's planned "all-in-one" local lifecycle (`yacd devnet`) is designed
  and phased in `.journal/049/LOCAL_LIFECYCLE_DESIGN.md` +
  `LOCAL_LIFECYCLE_PLAN.md` (session 049, design-only, approved; execution not yet
  started). Decided: single end-user runtime **k3d** (KinD stays for controller
  testing — see auto-memory `cli-local-runtime-k3d`); `yacd devnet` is a NEW verb
  (no name) ensuring one singleton managed cluster (`k3d-yacd`) + operator + a
  default local network, leaving `up`/`down` untouched to preserve the CI/Chainsaw
  contract (CI selects clusters via ambient `KUBECONFIG`, indistinguishable from a
  default at the CLI flag layer); operator installed by embedding a
  build-time-rendered chart and applying via SSA (not the Helm SDK, not
  ctlptl/k3d-as-library); pinned k3d binary auto-fetched to an XDG path with a
  build-time-embedded checksum; the kubectl context is switched **and** yacd
  tracks/pins its own managed context (runtime by fixed cluster name is the source
  of truth, `$XDG_STATE_HOME/yacd` is a supplementary record + flock); chain data
  ephemeral-only. New CLI ports (`cluster`, `toolbin`, `operator`, `clusterstate`)
  follow port-package + adapter-subpackage layout; `lifecycle.Manager`
  orchestrates; `kube.Client` reused. Plan **Phase 1 = cut operator `v0.1.0`**
  (chart + images) so the CLI has a reliable install target; the funded-wallet
  bootstrap (operator-side) lands before the devnet phase as `v0.2.0`. Docs
  deferred to a separate session.
- **Operator releases are live (session 053).** Plan Phase 1 shipped **`v0.1.0`**
  and Phase 4 shipped **`v0.1.1`** through release-please → `release.yml`. NOTE the
  plan said "v0.2.0" for the wallet, but the repo's `release-please-config.json`
  has `bump-patch-for-minor-pre-major: true`, so pre-1.0 `feat`s bump the PATCH
  digit (0.1.0 → 0.1.1, not 0.2.0). **Phase 5 embeds `0.1.1`** (the latest), not
  0.2.0. Published, attested refs: manager
  `ghcr.io/meigma/yacd:v0.1.1@sha256:5d53ca824dacad39c482dc93edfd2db4a65d5803f43dce5b18b1a7482b0f8e21`,
  faucet `ghcr.io/meigma/yacd/faucet:v0.1.1@sha256:826f8d52f0a4b0f607e2293cf72a8217de27700b5e5f1b35e1af86ef18fd3f66`,
  chart `oci://ghcr.io/meigma/yacd/chart:0.1.1@sha256:a8d24dfaa19a4af0279ed26654ff36a44e5cf50a05a5e0ffa02481688a5a049f`.
  The first-ever real root `release.yml` run worked; the GitHub draft releases are
  left for a human to Publish (GHCR artifacts publish immediately on the release-PR
  merge regardless).
- **Funded developer wallet (session 053, PR #84, in v0.1.1).** Opt-in
  `spec.chainAPI.wallet{enabled,fundingLovelace}` on LOCAL CardanoNetworks
  (requires faucet + kupo; rejected otherwise / on public). The controller
  generates an ed25519 payment key ONCE into an owned `<net>-wallet` Secret
  (`payment.skey`/`payment.vkey` cardano-cli text envelopes + `address`); the key
  is NEVER regenerated (would strand funds) — only an explicit `enabled:false`
  deletes the Secret (degrade preserves it). It publishes `status.wallet`
  (address/keySecretName/funded/fundedTxID), funds the address once via the faucet
  `/v1/topups` after Faucet+Kupo are ready, and confirms on-chain via a plain Kupo
  REST GET (on-chain balance is the source of truth → self-heals). `WalletReady`
  gates aggregate `Ready` when enabled; a definitive faucet 4xx rejection →
  `Degraded`, transient connectivity errors retry as pending. Default funding
  100,000 ADA (example faucet `maxTopUpLovelace` raised to fit). Address derivation
  is the shared pure pkg `internal/cardano/wallet` (reuses Apollo via the faucet's
  logic; `services/faucet/.../sources.go` now delegates to it), golden-tested vs a
  real `cardano-cli address build` vector. Funder/confirmer are injectable seams
  (`walletFunderOverride`/`walletConfirmerOverride`) using plain `net/http` — the
  MANAGER pulls NO ogmigo/Gorilla-WebSocket/kugo (verified via `go list -deps
  ./cmd`); keep it that way.
- **`yacd devnet` local-lifecycle ports — P5/P2/P3 IMPLEMENTED (session 054), P6/P7
  remain.** All new code is library-only under `cli/internal/` (port pkg = interface
  + types + doc.go; adapter = subpackage with `New(...)`); nothing is wired into
  `cli/internal/cli/options.go` yet (that is P6). Each port has a generated mock in
  `cli/internal/mocks` (`.mockery.yml`). Shipped:
  - **operator install (P5, PR #86):** `operator.Installer` port (`InstallSpec`/
    `State` + pure `Decide` semver reconcile) + `operator/ssa` adapter. Installs the
    operator by **server-side apply of a build-time-rendered, `//go:embed`'d copy of
    `charts/yacd`** — no Helm SDK / no network pull at runtime. `.dev/scripts/
    render-operator-chart.sh` renders `manifests/operator.yaml` (helm template
    `--include-crds --no-hooks`, manager+faucet **digest-pinned to v0.1.1**), run
    inside `root:generate` AFTER controller-gen and drift-guarded by `root:check`.
    Apply = CRDs-first + wait Established (typed apiextensionsv1) → workload in stable
    kind order, namespace-defaulted via RESTMapper, field owner `yacd-cli`. Install ns
    **pinned to `yacd-system`** (foreign ns rejected; the chart's RBAC subjects are
    baked to it). Version read from the manager Deployment's `app.kubernetes.io/
    version` label (no ConfigMap). Label-based prune over a fixed GVK set that
    **excludes CRDs**. Live `Ready` (Deployment Available) is NOT envtest-provable →
    P6 k3d e2e (already evidenced by session 053's published-chart smoke).
  - **toolbin (P2, PR #88):** `toolbin.Resolver` port + `toolbin/ghrelease` adapter
    resolves a pinned **k3d v5.9.0** binary: pre-staged (`YACD_K3D_PATH`) → digest
    cache hit → fetch+verify(embedded SHA256)+atomic install(chmod 0o755)+GC.
    **GitHub release assets 302 to `release-assets.githubusercontent.com`, so the
    fetch allow-lists GitHub download hosts and follows the redirect** (the embedded
    digest is the real guard). `DefaultK3dPin` (version+URL template+4 os/arch
    digests from the release `checksums.txt`) + `DefaultDir` ($XDG_DATA_HOME/yacd/bin).
  - **cluster + clusterstate (P3, PR #89):** `exec.Runner` seam (+`OS()`);
    `cluster.Provisioner` port (+`ManagedName`/`ManagedContext`/`K3sImage` consts; k3s
    pinned `v1.32.5-k3s1` = k3d v5.9.0's default) + `cluster/k3d` adapter
    (`EnsureCluster` state machine over the toolbin-resolved binary: absent→create /
    healthy→no-op / unhealthy→delete+recreate, partial-create rollback via
    `context.WithoutCancel`; injectable `/healthz` prober). **`k3d cluster list
    <name>` exits non-zero when absent → list WITHOUT a name + filter the JSON;
    `serversRunning>=1` = control plane up.** `clusterstate.Store` port (+`DefaultDir`
    $XDG_STATE_HOME/yacd) + `clusterstate/file` adapter (atomic JSON record 0600/0700
    + **gofrs/flock** ctx-aware `Lock(ctx)`). The lock is **composed by P6, NOT held
    inside cluster/k3d** (keeps the ports independently mockable). Gated live tests:
    `YACD_TOOLBIN_LIVE` / `YACD_CLUSTER_LIVE` (real network / real Docker).
  - **Maintenance:** on an operator release bump, update the digests in
    `render-operator-chart.sh` + re-render; on a k3d bump, update
    `ghrelease.DefaultK3dPin` + `cluster.K3sImage`. New deps this session (all
    direct): `golang.org/x/mod`, `k8s.io/apiextensions-apiserver`, `github.com/gofrs/flock`.
  - **P6/P7 hand-off:** see `.journal/054/SUMMARY.md` "Remaining Work" — P6 builds
    `lifecycle.Manager` + the `devnet` command subtree + a single shared targeting
    resolver (Design §6 precedence) wired into every verb + the `Options` factory
    fields, composing all of the above. The managed-context tier must only engage
    when a managed cluster exists so CI/Chainsaw (explicit KUBECONFIG) stay unaffected.
- **`yacd devnet` all-in-one — P6 DONE (session 055, PR #90 `db7887b`).** The plan's
  core sequence is complete (`P1✅ P4✅ P5✅ P2✅ P3✅ P6✅`); only **P7 (hardening &
  UX)** remains. `cli/internal/lifecycle.Manager` orchestrates `Up`/`Down`/`Status`
  under the clusterstate lock (composed at the command layer, not inside `cluster/k3d`);
  `Up` is bounded by `--timeout` (lock + all phases), captures the prior kubectl context
  before k3d switches it, and waits for operator Deployment readiness (SSA only applies).
  Durable contracts: (1) **`cli/internal/cli/target.go:ResolveTarget`** is the single
  precedence resolver (explicit `--kubeconfig`/`--context` or `YACD_*` > tracked managed
  context from the **cheap clusterstate record, no Docker probe** > ambient). It is a
  verified no-op for explicit-target (CI/Chainsaw) and never-ran (no record) callers, so
  all existing verb tests pass unedited. (2) Every network verb is wrapped by
  `withManagedReconcile` (root.go): a managed-targeted verb that FAILS probes
  `Provisioner.Status` and, on `!Exists`, clears the stale record + prints a notice
  (next call → ambient); `devnet status` reconciles likewise. Happy path never probes.
  (3) **`devnet` rejects explicit `--kubeconfig`/`--context`** (it owns its cluster; the
  isolate-kubeconfig feature is P7). (4) Cluster health is probed through the **recorded**
  `cluster.Spec.KubeconfigPath` (not ambient), so a `KUBECONFIG` change can't get a healthy
  cluster deleted; the `cluster/k3d` adapter reports the real default-kubeconfig target
  (`clientcmd...GetDefaultFilename`, honoring `KUBECONFIG`), not hardcoded `~/.kube/config`.
  (5) The default network is `cli/internal/cli/devnet.yaml`, a **byte copy** of
  `examples/local/yacd.yaml` (`go:embed` can't escape the package dir), drift-guarded by
  `TestDefaultDevnetEnvIsValid`. The gated live e2e is `YACD_DEVNET_LIVE`
  (`cli/internal/cli/devnet_live_test.go`); it isolates `KUBECONFIG`+`XDG_STATE_HOME` to
  temp dirs and is the regression test for the KUBECONFIG-honoring path.
- **`yacd devnet` KUBECONFIG-handling hardening (session 056, PR #92 `79761f2`).** A manual
  functional test found a HIGH-severity chain: switching `KUBECONFIG` between `devnet` runs
  could silently delete+recreate a healthy cluster. Durable contract now: the managed
  cluster's health/identity is keyed to the **recorded** kubeconfig (the clusterstate record's
  `kubeconfigPath`), never the ambient `KUBECONFIG`. (1) `k3d.infoFor(name, kubeconfigPath)` —
  the healthy no-op path returns `spec.KubeconfigPath` (where the probe reached it), only
  create/heal returns `defaultKubeconfigPath()` (where k3d's `--kubeconfig-update-default`
  wrote). (2) A kubeconfig/context **load** failure is a typed `probeConfigError`; `statusVia`
  returns it as a hard error so `EnsureCluster` aborts instead of treating it as `Healthy=false`
  and recreating — only a genuine `/healthz` failure marks the cluster unhealthy for healing.
  (3) `cluster.Provisioner.Status` now takes `kubeconfigPath`; `lifecycle.Manager.Status` and
  `cli/orphan.go` pass `record.KubeconfigPath` so reads probe the recorded file. (4)
  `lifecycle.Down` **clears** current-context when the captured prior was empty (calls
  `RestoreContext(path, "")` → `kube.SetCurrentContext(path,"")` blanks it) — otherwise k3d's
  `cluster delete` repoints kubectl to an arbitrary remaining (possibly prod) context. Minor:
  the duplicate `acquire cluster lock:` wrap was removed; create timeouts report
  `timed out after <d>`/`cancelled` instead of raw `signal: killed`. NOTE for recon: the Kupo
  endpoint port is **1442**; the `yacd.meigma.io/install=operator` label is stamped on CRDs but
  CRDs are excluded from the SSA prune GVK set, so they are still never pruned.
- **`yacd topup` self-forwards the faucet (session 057, PR #93).** topup no longer needs a
  `yacd run` wrapper. With no faucet-URL override it opens a short-lived port-forward to the
  faucet itself — reusing `forwardEndpoints` (extracted from `connectNetwork` in
  `cli/internal/cli/forward.go`; reads no Secret, so the trust gate runs before any token
  read) — POSTs, and closes. The same session forwards Kupo, so `topup --await` derives a
  loopback Kupo URL and works standalone (no `--kupo-url`). Precedence in
  `resolveFaucetTransport`: explicit `--faucet-url` OR ambient `YACD_FAUCET_URL` (inside
  `yacd run`) → use it as-is (`custom`, trust-gated); else self-forward (loopback, exempt).
  The `topup_trust.go` gate + the `AssertNotCalled(GetSecretValue)` no-token-leak invariant
  are unchanged. **Arg change (breaking, pre-1.0):** `yacd topup NAME LOVELACE` — LOVELACE is a
  required positional (the `--lovelace` flag is gone); `--address` stays a required flag
  (deliberately NOT defaulted to the wallet address). Live-validated on k3d.
- **`yacd init` (session 057, PR #93).** Prints a fully-commented developer `Environment`
  template to **stdout** (`yacd init > yacd.yaml`; no flags, `NoArgs`, no kube). The template is
  `//go:embed`'d at `cli/internal/cli/init.yaml` (var `defaultInitEnvYAML` in `embed.go`); its
  active (uncommented) portion is a batteries-included local devnet (faucet + funded wallet,
  mirrors `examples/local`), and commented blocks document the rest of the API. Because
  `devconfig.Load` uses UnmarshalStrict + validateExplicitFields, every uncommented section must
  be complete and optional sections are commented WHOLESALE. `init_test.go` drift-guards the
  active config by loading it through `devconfig.Load`. Registered in `root.go` without
  `withManagedReconcile`.
- **Docs follow-up owed on the `docs/mkdocs-site` branch (PR #91, session 052, still open).**
  Sessions 057's changes left stale references there that the docs session must fix before #91
  merges: `docs/reference/cli.md` + `docs/developer/{getting-started,networks}.md` still show
  `yacd list -A`; and any `topup`-under-`yacd run` / `--lovelace` examples need the new
  standalone `yacd topup NAME LOVELACE` form. Master-tracked docs (README, docs/host-access.md)
  were already updated in PR #93.
- **Operator install is now an in-memory Helm render of an IN-PLACE-embedded chart
  (session 058, PRs #94 + #96 — SUPERSEDES the session-054 build-time-render bullet
  above).** The `operator/ssa` adapter renders `charts/yacd` at install time via the lean
  Helm subset (`pkg/{chart,chart/loader,chartutil,engine}` + `CRDObjects()`; clientless,
  no OCI/registry — ~52 net-new pure-Go pkgs vs ~270 if `pkg/action` were used), then feeds
  the rendered objects to the unchanged SSA apply pipeline (CRD-first → wait-Established →
  kind-ordered → `yacd-cli` field owner → label-prune, CRDs never pruned). The chart is
  embedded **in place** by `charts/embed.go` (package `charts`, `//go:embed all:yacd` →
  `charts.OperatorChart fs.FS`; an *ancestor* dir, so no `..` and no copy). **There is no
  more pre-rendered `operator.yaml`, no `render-operator-chart.sh`/`sync-operator-chart.sh`,
  and no chart-copy drift guard** — `controller-gen` writes `charts/yacd/crds` and the embed
  reads it directly. On an operator release bump, update the two image digests in
  `operator.Default()` (`cli/internal/operator/values.go`) + re-embed (automatic via the
  source chart). `install.go` builds `InstallSpec{Namespace, Values: Default()}`; render
  validates the merged values against `charts/yacd/values.schema.json`.
- **`yacd install` (session 058, PR #96)** installs/upgrades the operator onto an ARBITRARY
  cluster. Targeting = explicit `--kubeconfig`/`--context` (or `YACD_*`) else AMBIENT
  current-context — it does NOT consult the managed-devnet record, is NOT wrapped in
  `withManagedReconcile`, and does NOT call `rejectExplicitTarget` (the opposite of devnet).
  Flags: `-n` (install namespace, default `yacd-system`, now a REAL render input →
  RBAC subjects + namespaced objects agree for any ns), `--wait`/`--timeout` (bounds the
  WHOLE op even with `--wait=false`), `--dry-run`, and `-f`/`--set`/`--set-string` Helm value
  overrides (helm `pkg/strvals`) merged into `operator.Values.Extra` (precedence `-f`<`--set`
  <`--set-string`; schema-validated, fail-fast under `--dry-run` too). **Model A:** the image
  stays digest-pinned (upgrade the CLI to change versions); deep-merge makes `--set image.tag`
  inert but `--set image.digest/repository` DO repoint it (documented in `--help`, not
  code-enforced). The `operator.Installer` port grew: `OperatorState(ctx, namespace)`,
  `Plan(ctx,spec)→Decision` (renders+reads+`Decide`, NO apply, backs `--dry-run`), and a
  shared `operator.WaitForReady(ctx,inst,ns,poll)` (extracted from lifecycle; devnet rewired,
  identical behavior). `operator.DefaultNamespace` is the single source. `Decide`
  (install/upgrade/noop/refuse, typed errors, major-mismatch BEFORE the newer/older compare)
  is unchanged. **PR3 (NOT done):** `yacd uninstall` (`Installer.Remove` + explicit
  CRD-deletion policy) + runtime version selection (OCI chart fetch from
  `ghcr.io/meigma/yacd/chart`). See `.journal/058/OPERATOR_INSTALL_PROPOSAL.md` §7/§8.
- **CLI-native wallets / faucet removal (session 059, P1–P3 shipped; P4–P5 remain).**
  Plan: `.journal/059/WALLET_REARCH_PLAN.md`. The goal is to delete the in-cluster faucet
  service and have the CLI own all wallet management + funding. SHIPPED so far:
  - `internal/cardano/tx` is the domain-pure chain-tx engine (Submitter port + `Apollo`
    adapter: build/validate/sign/submit one funding tx given key hex + addresses + lovelace
    + Ogmios/Kupo URLs). The MANAGER (`./cmd`) MUST stay free of ogmigo/kugo/`internal/
    cardano/tx`; keep `tx` out of its import graph and never make the controller fund.
  - On LOCAL faucet-enabled networks the controller GENERATES a `faucet` payment wallet once
    into `<net>-wallet-faucet` (labels `yacd.meigma.io/wallet-name=faucet` +
    `wallet-source=genesis-funded`; data payment.skey/vkey/address; ownerRef→network) and a
    PVC-only init container runs `yacd-cardano-tools fund-genesis --env-dir --address
    --lovelace` to add an `initialFunds` allocation to `/state/env/shelley-genesis.json`
    BEFORE the node boots (no hash recompute — the local node config carries no
    ShelleyGenesisHash). cardano-tools is pinned at **11.0.1-yacd.6** (`internal/cardano/
    toolsimage`). Funding amount default `defaultFaucetWalletFundingLovelace=1_000_000 ADA`.
  - `yacd wallet {list,add,topup,export,remove}` (`cli/internal/cli/wallet*.go` +
    `cli/internal/wallet` store). WALLET selector = name | pubkey-hex | bech32 address.
    Funding self-forwards Ogmios+Kupo (`forwardEndpoints`), reads the source (faucet or
    `--from`) Secret, decodes envelopes via `wallet.DecodePaymentKeyEnvelope` (manager-safe),
    submits via `internal/cardano/tx`, confirms via the kugo path. The standalone `yacd topup`
    is GONE (folded into `wallet topup`; faucet = default source). The dev wallet
    (`spec.chainAPI.wallet`) is UNTOUCHED + still faucet-HTTP-funded — P4 removes it.
  - **REMAINING: P4** = cut `devnet`/dev-wallet funding to the CLI + delete the faucet
    service / image / sidecar / Service / auth-Secret + `spec.chainAPI.{faucet,wallet}` +
    conditions + rewrite Chainsaw/examples; **P5** = faucet-free release + re-render the CLI's
    embedded chart + docs.
  - **KNOWN follow-up:** Apollo's `OgmiosChainContext.GenesisParams` fails its
    `ogmigo.GenesisConfig("shelley")` websocket read (close 1006) on every funding and
    `fmt.Printf`s to stdout (now redirected to stderr in `wallet_fund.go`). HARMLESS today —
    the empty `Base.GenesisParameters{}` fallback is unused (fees come from
    `LatestEpochParams`, which succeeds; TTL is slot-based) — but a latent trap if a tx
    feature ever needs genesis constants. Root cause = the SundaeSwap ogmigo client on the
    discontinued Gorilla WebSocket toolkit (Kusari-flagged); the durable fix is to move off
    ogmigo / use Ogmios HTTP queries, folded into P4/P5.
