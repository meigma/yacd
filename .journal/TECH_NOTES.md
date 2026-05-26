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
- `internal/cardano/dbsync` is the Kubernetes-free planner for db-sync config,
  topology, invocation args, environment, plan fingerprint, and database
  identity fingerprint. The accepted database identity includes network
  artifact hash, DB address/user, db-sync image, ledger backend, and insert
  options; changes to that identity are rejected until a recreate or migration
  story exists.
- Managed `CardanoDBSync` Postgres creates `<dbsync>-postgres-auth` when
  `managed.authSecretRef` is omitted, `<dbsync>-postgres-state`,
  `<dbsync>-postgres` Service, and `<dbsync>-postgres` Deployment. The
  generated password is create-once only; if the generated Secret is deleted
  after managed DB identity acceptance, the controller degrades instead of
  regenerating a random password for an initialized data directory. Provided
  managed auth Secret identity is based on password material, not Secret
  resourceVersion metadata.
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
- The faucet/topup path should stay narrow and use Ogmios for chain
  interaction. Avoid turning it into a general wallet platform.
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
- The first CLI surface is intentionally small: `yacd deploy -f yacd.yaml`
  renders and server-side-applies one `CardanoNetwork`, `--dry-run` prints the
  rendered manifest without applying, `--wait` polls readiness, and `yacd info
  NAME --json` returns a command-owned DTO with status, network identity, and
  node/Ogmios endpoints.
- The phase-4 developer config is
  `apiVersion: yacd.meigma.io/devconfig/v1alpha1`, `kind: Environment`, with
  `metadata.name`, optional `metadata.namespace`, and `spec.network` currently
  shaped as `api/v1alpha1.CardanoNetworkSpec`. Because the CLI decodes into the
  concrete API type, it rejects omitted CRD-defaulted concrete fields rather
  than rendering zero values.
- `yacd deploy --wait` must only trust `Ready` or `Degraded` conditions whose
  `observedGeneration` is at least the current object generation; otherwise an
  updated already-ready resource can report stale success.
- Root `DESIGN.md` captures the current high-level architecture; `.journal/PLAN.md`
  captures the rough component sequence for the initial prototype.
- PR #3 introduced the first real API group/version with
  `yacd.meigma.io/v1alpha1` and the namespaced `CardanoNetwork` CRD. The draft
  uses `spec.mode: local|public`; public networks use `profile:
  preprod|preview|mainnet|custom`, and custom public profile data is limited to
  same-namespace ConfigMap/Secret refs through `corev1.LocalObjectReference`.
- The first runtime path is local-mode only. `primaryWorkloadBuilder` maps
  network magic, pool count, slot/epoch timing, and node version into
  `internal/cardano/localnet.Spec`; it rejects public mode and unsupported local
  genesis/era/pool-default inputs until later slices implement those contracts.
  This phase supports exactly one pool/primary node.
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
  `CardanoNetwork` controller deletes it and waits for a later reconcile to
  recreate it. The new ConfigMap UID rolls the primary Deployment so the init
  publisher can republish exact files. This avoids same-reconcile delete/create
  races with Kubernetes asynchronous deletion or finalizers.
- The manager Helm chart is intentionally cluster-scoped for the current
  local/dev operator. Treat the manager ServiceAccount as trusted cluster
  automation for YACD-managed namespaces; namespace-scoped manager mode is a
  future hardening path.
- A `CardanoNetwork` localnet is stable for its lifetime. The accepted localnet
  fingerprint is stored on the owned PVC and in CR status; if localnet inputs
  drift after acceptance, reconcile stops before Deployment updates and sets a
  degraded condition. Delete and recreate the CR/PVC to change localnet
  parameters.
- Primary PVC reconciliation allows storage expansion when the accepted
  fingerprint matches, rejects storage shrink and requested storage class
  drift, and refuses unowned or foreign-owned same-name children rather than
  adopting them silently.
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
