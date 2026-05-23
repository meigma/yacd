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
  as the default chain API, Kupo as the default chain index API, and a
  token-protected faucet as the default local top-up endpoint.
- Developer CLI under `cli/` with `deploy`, `info`, and `topup` commands.
- Helm chart packaging for the manager deployment.
- Moon tasks for generation, checks, tests, local deployment, and Kind smoke
  testing.
- Local Kind/Tilt development stack wiring.

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
go run ./cli/cmd/yacd deploy -f examples/local/yacd.yaml --dry-run
```

Deploy the example environment and wait for the operator to report readiness:

```sh
kubectl create namespace yacd-smoke --dry-run=client -o yaml | kubectl apply -f -
go run ./cli/cmd/yacd deploy -f examples/local/yacd.yaml --namespace yacd-smoke --wait
go run ./cli/cmd/yacd info phase4-smoke --namespace yacd-smoke
go run ./cli/cmd/yacd topup phase4-smoke --namespace yacd-smoke --address addr_test... --lovelace 1000000
```

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
