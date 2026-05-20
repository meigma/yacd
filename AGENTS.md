# Agent Instructions

<!-- BEGIN ai-protocol -->
This repository's operating protocol lives in `.session.md`.

Before doing substantive work, read `.session.md` in full and follow it. It
covers startup context loading, session setup, session lifecycle, skill loading,
Worktrunk branching, session journaling, file schemas, architecture, and process
expectations.

If `.session.md` is missing, stop and tell the user the session protocol is not
installed correctly.
<!-- END ai-protocol -->

## Repository Overview

This is a Go Kubernetes operator built with Kubebuilder and
controller-runtime. API types live in `api/`, reconciliation logic lives in
`internal/controller/`, manager startup lives in `cmd/`, the Helm chart lives
in `charts/template-k8s/`, and e2e smoke tests live in `test/chainsaw/`.

The current API is `example.meigma.io/v1alpha1` `NginxDeployment`. It owns a
same-named ConfigMap, Deployment, and ClusterIP Service, and projects fresh
Deployment readiness into status conditions.

## Local Skills

Load `.agents/skills/k8s-operator/SKILL.md` before changing APIs,
controllers, RBAC markers, envtest coverage, Chainsaw tests, or operator task
wiring. That skill captures the local operator practices this repository
expects agents to follow.

## Development Workflow

Moon is the task front door. Do not add or restore Makefile-driven workflows.
If upstream Kubebuilder docs say to run `make`, translate the step to the
matching Moon task.

Keep the Moon task surface small. Prefer extending `root:check`, `root:test`,
or an existing script over adding narrowly scoped recipes. Add a new Moon task
only when it is a durable workflow a maintainer should remember and run
directly; avoid command sprawl that creates recipe fatigue.

Common tasks:

```sh
moon run root:generate
moon run root:check
moon run root:test
moon run root:test-e2e
git diff --check
```

Use `root:test` for Go tests because it sets `KUBEBUILDER_ASSETS` through
`setup-envtest`. Do not rely on plain `go test ./...` unless envtest assets are
already configured.

## Local Development Stack

Use the local dev stack when you need a fast operator feedback loop in Kind.
Run it through Moon from the repo root:

```sh
direnv allow
moon run root:dev-up
moon run root:dev-down
```

`ctlptl` owns the Kind cluster and local registry described in
`dev/ctlptl.yaml`; do not create or delete that cluster from the `Tiltfile`.
`Tiltfile` assumes the current Kubernetes context is `kind-template-k8s-dev`,
renders the Helm chart, and redeploys changes. `ko` builds the manager image
from `./cmd` using `.ko.yaml`, and Tilt injects the built image into the
Helm-rendered Deployment.

## Manager Startup

Manager configuration uses Kong in `cmd/options.go`. Add new command-line
flags by extending `managerOptions`, and cover parser/default behavior in
`cmd/options_test.go`.

Logging is built from Go `slog` handlers and bridged into controller-runtime's
`logr` logger. Preserve the existing `--log-format=json|text` and
`--log-level=debug|info|warn|error` behavior when changing startup code.

Keep health and readiness checks registered through controller-runtime in
`cmd/setup.go`. Keep metrics/webhook TLS and HTTP/2 behavior in `cmd/manager.go`
unless the operator's runtime contract intentionally changes.

## Operator Practices

Use controller-runtime ownership and watches deliberately:

- owned children should have controller references and `.Owns(...)` watches
- status should use `metav1.Condition`
- parent availability must not trust stale child status
- inline data copied into Kubernetes objects must be bounded or moved behind a
  reference
- RBAC markers should match the reconciler's actual reads and writes

Generated files are part of the source tree, but should be produced by tools:

- run `moon run root:generate` after API type changes
- run `moon run root:generate` after API marker, CRD, webhook, or manifest
  changes; generated CRDs are written to `charts/template-k8s/crds`
- do not hand-edit `zz_generated.deepcopy.go` or generated CRDs except to
  diagnose generator output
- keep operator deployment manifests in the Helm chart; do not restore a
  second manifest tree

## Testing

Keep the test layers intentionally separate.

Use envtest for the controller/API behavior matrix. Cover owner references,
labels/selectors, default handling, status freshness, restricted-compatible pod
settings, rollout hashes, API validation, and update paths near the controller
code. Include at least one manager-backed envtest case for each controller so
`.For(...)`, `.Owns(...)`, watches, predicates, and field indexes are exercised
through controller-runtime rather than only by direct `Reconcile` calls.

Use Chainsaw for the Kind-backed install and runtime smoke path. Keep e2e
coverage focused on chart install/upgrade wiring, manager readiness, RBAC or
auth paths that only fail in a real cluster, metrics exposure, one
representative custom resource, the parent condition, and the owned
workload/service becoming available.

Do not duplicate the envtest behavior matrix in Chainsaw. Add a Chainsaw case
only when the assertion depends on the packaged operator running in a real
cluster, multiple deployed controllers, Kubernetes workload controllers, or
cluster networking. Add or extend envtest when the assertion is about
reconciler output, API validation, status transitions, event routing,
predicates, indexes, or object ownership.
