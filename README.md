# YACD

YACD is a Kubernetes-native development environment manager for Cardano. It is
aimed at builders who need repeatable local or shared development networks, not
validators, stake pool operators, or production network operators.

The repository currently contains the operator foundation, the initial
`CardanoNetwork` API and reconciler, and a first developer CLI for deploying a
local YACD environment from a checked-in config file.

## Current State

- Controller-runtime manager startup with structured logging.
- Health and readiness probes.
- Secure metrics serving with Kubernetes authn/authz filters.
- Initial `CardanoNetwork` CRD shape for local and public network modes.
- Local-mode `CardanoNetwork` reconciliation for one primary node with Ogmios
  as the default chain API, Kupo as the default chain index API, and an opt-in
  token-protected faucet for local top-ups.
- Developer CLI under `cli/` with `up`, `down`, `list`, `info`, and `topup`
  lifecycle commands, plus `run`, `connect`, and `exec` host-access verbs that
  bridge the network's chain APIs to your tests through the `YACD_*` environment
  contract (see [docs/host-access.md](docs/host-access.md)).
- Helm chart packaging for the manager deployment.
- Moon tasks for generation, checks, tests, local deployment, and Kind smoke
  testing.
- Local Kind/Tilt development stack wiring.

## Security Model

YACD is currently a development-environment operator. The Helm chart installs
the manager with a cluster-scoped role so it can watch `CardanoNetwork`
resources and owned runtime children across namespaces. Treat the manager
ServiceAccount as trusted cluster automation for YACD-managed namespaces.
Per-network artifact publisher ServiceAccounts are narrower: each publisher can
only get and patch its own network artifact ConfigMap.

Namespace-scoped manager mode is a future hardening path, not part of the first
localnet/db-sync prototype.

## Development

Enable the pinned local toolchain:

```sh
direnv allow
proto status
```

Run the normal local checks and tests:

```sh
moon run root:check
moon run root:test
git diff --check
```

Render the example local environment without changing the cluster:

```sh
go run ./cli/cmd/yacd up phase4-smoke -f examples/local/yacd.yaml --dry-run
```

Bring the environment up and wait for readiness, then inspect and tear it down.
The environment name is a command-line argument; the namespace defaults to the
name and is auto-created, so one spec deploys under any name:

```sh
go run ./cli/cmd/yacd up phase4-smoke -f examples/local/yacd.yaml
go run ./cli/cmd/yacd list
go run ./cli/cmd/yacd info phase4-smoke
```

Reach the network from the host through the `run`, `connect`, and `exec` verbs,
which forward the chain APIs and wire the `YACD_*` environment so your tests
read ordinary env vars instead of a YACD file:

```sh
# Run any command with the environment wired in (the primary test/CI path):
go run ./cli/cmd/yacd run phase4-smoke -- go test ./e2e/...

# Or hold the forwards open in one terminal and work in another:
go run ./cli/cmd/yacd connect phase4-smoke

# Fund a checked-in address and wait for on-chain confirmation:
go run ./cli/cmd/yacd run phase4-smoke -- sh -c \
  'yacd topup phase4-smoke --address addr_test... --lovelace 1000000 --faucet-url "$YACD_FAUCET_URL" --await'

# cardano-cli reaches the node over its local socket, so use exec (in-pod):
go run ./cli/cmd/yacd exec phase4-smoke -- cardano-cli query tip --testnet-magic 42

go run ./cli/cmd/yacd down phase4-smoke
```

The checked-in local example opts into the faucet. A minimal `CardanoNetwork`
does not expose the faucet unless `spec.chainAPI.faucet.enabled` is set. The
published chain-API endpoints are in-cluster Service URLs; the host-access verbs
forward them to loopback so the `YACD_*` URLs are reachable from your tests, and
the loopback faucet URL is exempt from the `topup` trust gate. Targeting the
published in-cluster faucet URL directly from the host instead requires an
externally routable address: custom non-loopback `--faucet-url` values require
`--trust-faucet-url`, and custom non-loopback `http://` values also require
`--allow-insecure-faucet-url`. See [docs/host-access.md](docs/host-access.md)
for the full `YACD_*` contract and the `run`-versus-`exec` rule.

Run the local development stack with Kind, ctlptl, Tilt, and ko:

```sh
moon run root:dev-up
```

Stop and delete the local development stack:

```sh
moon run root:dev-down
```

## Local Install

Install from a local checkout:

```sh
moon run root:deploy
```

Useful local deployment overrides:

```sh
HELM_RELEASE=yacd HELM_NAMESPACE=yacd-system moon run root:deploy
IMG=ghcr.io/meigma/yacd:<tag> moon run root:deploy
FAUCET_IMG=ghcr.io/meigma/yacd/faucet:<tag> moon run root:deploy
LOCAL_IMAGE=true IMG=example.com/yacd:v0.0.1 moon run root:deploy
```

`spec.chainAPI.faucet.image` may select a different tag or digest from the
operator-configured faucet image repository. Custom faucet repositories require
installing the operator with that repository as the default faucet image and, if
Kyverno image verification is enabled, matching `imageReferences`.

Uninstall the local deployment:

```sh
moon run root:undeploy
```

## Release Shape

The repository publishes the developer CLI as the `yacd` release binary, plus
the controller manager container image and OCI Helm chart. Release Please owns
versioning, while the release workflow publishes artifacts under
`ghcr.io/meigma/yacd`.

## Design

See [DESIGN.md](DESIGN.md) for the current architecture direction. The first
working slice should stay narrow and let the CRD and controller shape evolve
from prototype work.
