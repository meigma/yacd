---
name: k8s-operator
description: Use when building, reviewing, or testing Kubernetes operators in this repository, especially Kubebuilder/controller-runtime APIs, CRDs, reconcile loops, owned child resources, status conditions, envtest specs, Moon tasks, or Kind-backed e2e smoke tests.
---

# Kubernetes Operator Work

Use this skill to keep YACD operator work prototype-friendly but correct enough
to teach the right patterns. Prefer the smallest working slice that proves the
workflow, then tighten behavior from what the prototype exposes.

## Current Repository State

YACD currently has an operator foundation with no custom APIs and no
reconcilers. Do not add placeholder CRDs or fake controllers just to exercise
Kubebuilder. The first API should be product-shaped and grounded in
`DESIGN.md`.

## Testing Boundary

Keep envtest and Chainsaw from growing into two copies of the same suite.

Use envtest for controller/API behavior: reconciler output, CRD validation,
default handling, status transitions, owner references, selectors, rollout
hashes, event routing, predicates, field indexes, and watch wiring. Direct
`Reconcile` calls are fine for most behavior cases, but each controller should
also keep a small manager-backed envtest case that proves `SetupWithManager`
actually wires parent and owned-child events.

Use Chainsaw for Kind-backed install/runtime smoke: chart install or upgrade
wiring, manager readiness, RBAC/auth that only fails in-cluster, metrics
exposure, and a representative custom resource only after one exists.

Do not port the envtest matrix into Chainsaw. Add Chainsaw coverage only when
the assertion requires the packaged operator, Kubernetes workload controllers,
cluster networking, multiple deployed controller instances, or another real
cluster behavior envtest cannot model.

## Observability Boundary

Keep operator-specific metrics and events focused on behavior controller-runtime
cannot infer. Use controller-runtime's built-in reconcile, workqueue, REST
client, process, and Go runtime metrics for generic controller health.

Operator metrics must use finite labels. Do not add namespace, name, UID,
image, or arbitrary spec values as labels. Keep object-specific state in
Kubernetes status, and prefer counters for meaningful controller actions such
as child resources created or corrected and status condition transitions.

Emit Kubernetes Events for user-visible state changes, not for every reconcile.
Aggregate child resource create/update results into one event per successful
reconcile, and emit condition events only after the status patch succeeds and
the persisted condition status or reason changes.

Use controller-runtime's context logger in reconcile loops. Log actual
controller side effects and persisted user-visible status transitions at info
level. Put start/finish messages, deleted-object ignores, no-op child applies,
and status patches that do not change condition status or reason behind `V(1)`.

## Controller Practices

Use controller-runtime ownership and watches deliberately:

- owned children should have controller references and `.Owns(...)` watches
- status should use `metav1.Condition`
- parent availability must not trust stale child status
- inline data copied into Kubernetes objects must be bounded or moved behind a
  reference
- RBAC markers should match the reconciler's actual reads and writes

Extract patch/status helpers once a controller owns multiple conditions or once
a second controller repeats the same status flow. Keep the first prototype
direct if a helper would hide the behavior being taught.

Use field indexes when a controller watches a referenced object and needs to
find all custom resources that point at it. Do not add indexes for simple owned
children handled by `.Owns(...)`.

Use predicates to keep reconcile queues focused. A good default is to ignore
parent updates that only changed status, while still allowing owned child
events to enqueue the parent. Keep predicates narrow and documented.

Use metadata-only watches for high-cardinality resources where the reconciler
only needs name, namespace, labels, annotations, owner references, or
resourceVersion. Do not use metadata-only watches when the reconciler derives
desired state from the watched object's spec or status.

## Configuration And Artifacts

Add typed manager configuration only after the operator has real runtime knobs:
namespace scope, leader election, cache options, feature gates, controller
concurrency, metrics, health, or webhook TLS.

Generated files are part of the source tree, but should be produced by tools:

- run `moon run root:generate` after API type changes
- run `moon run root:generate` after API marker, CRD, webhook, or manifest
  changes
- do not hand-edit `zz_generated.deepcopy.go` or generated CRDs except to
  diagnose generator output
- keep operator deployment manifests in the Helm chart; do not restore a
  second manifest tree
