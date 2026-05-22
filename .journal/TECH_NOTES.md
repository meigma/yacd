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
- The faucet/topup path should stay narrow and use Ogmios for chain
  interaction. Avoid turning it into a general wallet platform.
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
- The first published corrected tools image is
  `ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.1`. Future packaging-only
  fixes should bump `yacd.N`; future upstream Cardano bumps should move the
  base version and reset the YACD packaging revision.
- The `cardano-testnet` init-container fragment belongs in
  `internal/controller/cardanonetwork`, not `internal/cardano/localnet`. It
  calls the image-owned `/opt/yacd/bin/yacd-cardano-testnet-init` wrapper,
  passes the compact plan manifest through env, and expects a writable
  `localnet-state` volume mounted at the plan state directory.
- A `CardanoNetwork` localnet is stable for its lifetime. The accepted localnet
  fingerprint is stored on the owned PVC and in CR status; if localnet inputs
  drift after acceptance, reconcile stops before Deployment updates and sets a
  degraded condition. Delete and recreate the CR/PVC to change localnet
  parameters.
- Primary PVC reconciliation allows storage expansion when the accepted
  fingerprint matches, rejects storage shrink and requested storage class
  drift, and refuses unowned or foreign-owned same-name children rather than
  adopting them silently.
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
  publishes node-to-node and Ogmios endpoints, reaches `Ready=True`, returns a
  real Ogmios `queryNetwork/tip` result through the Service, then disables
  Ogmios and verifies the owned Service is deleted and the endpoint is cleared.
- The repo-local development stack is managed by `moon run root:dev-up` and
  `moon run root:dev-down`. The stack uses `.dev/` tooling, shared
  `.run/yacd-dev` runtime state, Kind context `kind-yacd-dev`, and Tilt port
  `10350`; implementation sessions should start it once after selecting an
  implementation worktree, keep it running across ordinary turns and review
  pauses, and stop it at explicit session close/end-of-session unless the human
  asks otherwise.
