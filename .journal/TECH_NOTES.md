# Technical Notes

- YACD is intended as a Kubernetes-native Cardano development environment
  manager for builders, not validators or stake pool operators. The first
  prototype should stay local-first and Kind/Tilt-friendly.
- The primary CRD should represent a Cardano environment/network rather than a
  single node. The first runtime can be one primary `cardano-node` StatefulSet
  with Ogmios as a default sidecar and ClusterIP chain API.
- Supporting services should be separate CRDs/controllers. Network-only
  services can run as independent workloads; heavy IPC services such as db-sync
  should prefer a dedicated follower-node Pod so they do not mutate or restart
  the primary node.
- db-sync is the first supporting-service priority. Yaci Store is a later
  optional Blockfrost-like/indexer candidate after the supporting-service model
  is proven.
- The faucet/topup path should stay narrow and use Ogmios for chain
  interaction. Avoid turning it into a general wallet platform.
- The companion CLI should compile one developer-facing config into Kubernetes
  CRDs and own imperative operations such as topup, wait, status, and connection
  info.
- Root `DESIGN.md` captures the current high-level architecture; `.journal/PLAN.md`
  captures the rough component sequence for the initial prototype.
- PR #3 introduced the first real API group/version with
  `yacd.meigma.io/v1alpha1` and the namespaced `CardanoNetwork` CRD. The draft
  uses `spec.mode: local|public`; public networks use `profile:
  preprod|preview|mainnet|custom`, and custom public profile data is limited to
  same-namespace ConfigMap/Secret refs through `corev1.LocalObjectReference`.
- The first runtime path is local-mode only. The controller adapter maps
  network magic, pool count, slot/epoch timing, and node version into
  `internal/cardano/localnet.Spec`; it rejects public mode and unsupported local
  genesis/era/pool-default inputs until later slices implement those contracts.
- `internal/cardano/localnet` is the pure Go, Kubernetes-free boundary for
  `cardano-testnet create-env` inputs. It returns a deterministic invocation,
  expected output layout, fingerprint, and JSON-serializable manifest for later
  init-container idempotency.
- The repo-local development stack is managed by `moon run root:dev-up` and
  `moon run root:dev-down`. The stack uses `.dev/` tooling, shared
  `.run/yacd-dev` runtime state, Kind context `kind-yacd-dev`, and Tilt port
  `10350`; future sessions should start it after selecting an implementation
  worktree and stop it before pause or closeout.
