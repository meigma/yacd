# YACD

YACD is a Kubernetes-native development environment manager for Cardano. It is
aimed at builders who need repeatable local or shared development networks, not
validators, stake pool operators, or production network operators.

The repository currently contains the operator foundation only. It has no
custom resource definitions yet; the first Cardano environment API will land in
a later prototype slice.

## Current State

- Controller-runtime manager startup with structured logging.
- Health and readiness probes.
- Secure metrics serving with Kubernetes authn/authz filters.
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
LOCAL_IMAGE=true IMG=example.com/yacd:v0.0.1 moon run root:deploy
```

Uninstall the local deployment:

```sh
moon run root:undeploy
```

## Release Shape

The repository keeps the normal operator release shape: manager binary,
container image, and OCI Helm chart. Release Please owns versioning, while the
release workflow publishes artifacts under `ghcr.io/meigma/yacd`.

## Design

See [DESIGN.md](DESIGN.md) for the current architecture direction. The first
working slice should stay narrow and let the CRD and controller shape evolve
from prototype work.
