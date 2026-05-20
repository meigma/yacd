---
name: k8s-operator
description: Use when building, reviewing, or testing Kubernetes operators in this repository, especially Kubebuilder/controller-runtime APIs, CRDs, reconcile loops, owned child resources, status conditions, envtest specs, Moon tasks, or Kind-backed e2e smoke tests.
---

# Kubernetes Operator Work

Use this skill to keep operator work prototype-friendly but correct enough to
teach the right patterns. Prefer the smallest working slice that proves the
workflow, then tighten behavior from what the prototype exposes.

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
exposure, one representative custom resource, and the owned workload becoming
available.

Do not port the envtest matrix into Chainsaw. Add Chainsaw coverage only when
the assertion requires the packaged operator, Kubernetes workload controllers,
cluster networking, multiple deployed controller instances, or another real
cluster behavior envtest cannot model.

## Observability Boundary

Keep operator-specific metrics and events focused on behavior controller-runtime
cannot infer. Use controller-runtime's built-in reconcile, workqueue, REST
client, process, and Go runtime metrics for generic controller health.

Template metrics must use finite labels. Do not add namespace, name, UID, image,
or arbitrary spec values as metric labels. Keep object-specific state in
Kubernetes status, and prefer counters for meaningful controller actions such as
child resources created or corrected and status condition transitions.

Emit Kubernetes Events for user-visible state changes, not for every reconcile.
Aggregate child resource create/update results into one event per successful
reconcile, and emit condition events only after the status patch succeeds and
the persisted condition status or reason changes.

Use controller-runtime's context logger in reconcile loops. Log actual
controller side effects and persisted user-visible status transitions at info
level. Put start/finish messages, deleted-object ignores, no-op child applies,
and status patches that do not change condition status or reason behind `V(1)`.

## Best Practices From Mature Operators

These patterns are worth carrying into this template as examples and decision
rules, not as mandatory boilerplate. Add them when the sample API has a real
need for the behavior.

### Patch And Status Helpers

Extract patch/status helpers once a controller owns multiple conditions or once
a second controller repeats the same status flow. Keep the first prototype
direct if a helper would hide the behavior being taught.

Use the helper to make the status intent obvious and to avoid no-op status
patches:

```go
func (r *NginxDeploymentReconciler) patchStatusIfChanged(
	ctx context.Context,
	instance *examplev1alpha1.NginxDeployment,
	mutate func(),
) error {
	original := instance.DeepCopy()
	mutate()
	if equality.Semantic.DeepEqual(original.Status, instance.Status) {
		return nil
	}
	return r.Status().Patch(ctx, instance, client.MergeFrom(original))
}

func (r *NginxDeploymentReconciler) reconcileStatus(
	ctx context.Context,
	instance *examplev1alpha1.NginxDeployment,
	deployment *appsv1.Deployment,
) error {
	return r.patchStatusIfChanged(ctx, instance, func() {
		instance.Status.ReadyReplicas = deployment.Status.ReadyReplicas
		meta.SetStatusCondition(&instance.Status.Conditions, availableCondition(instance, deployment))
	})
}
```

If several controllers need owned conditions, move the helper into a small local
package such as `internal/controller/statuspatch` instead of copying it.

### Field Indexes For Reverse Lookups

Use field indexes when a controller watches a referenced object and needs to
find all custom resources that point at it. Do not add indexes for simple owned
children handled by `.Owns(...)`.

For example, if inline config becomes a referenced ConfigMap:

```go
const configRefIndex = ".spec.configRef.name"

func (r *NginxDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&examplev1alpha1.NginxDeployment{},
		configRefIndex,
		func(obj client.Object) []string {
			instance := obj.(*examplev1alpha1.NginxDeployment)
			if instance.Spec.ConfigRef == nil {
				return nil
			}
			return []string{instance.Spec.ConfigRef.Name}
		},
	); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&examplev1alpha1.NginxDeployment{}).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.findByConfigRef)).
		Complete(r)
}

func (r *NginxDeploymentReconciler) findByConfigRef(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	var list examplev1alpha1.NginxDeploymentList
	if err := r.List(ctx, &list,
		client.InNamespace(obj.GetNamespace()),
		client.MatchingFields{configRefIndex: obj.GetName()},
	); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&list.Items[i]),
		})
	}
	return requests
}
```

Always test the index function directly and with envtest. Index bugs usually
show up as missed reconciles, not obvious failures.

### Event Predicates

Use predicates to keep reconcile queues focused. A good default is to ignore
parent updates that only changed status, while still allowing owned child
events to enqueue the parent.

```go
return ctrl.NewControllerManagedBy(mgr).
	For(&examplev1alpha1.NginxDeployment{},
		builder.WithPredicates(predicate.GenerationChangedPredicate{}),
	).
	Owns(&appsv1.Deployment{}).
	Complete(r)
```

If operators need a manual "try again now" path without changing spec, add a
small annotation predicate instead of teaching users to make no-op spec edits:

```go
const reconcileAtAnnotation = "example.meigma.io/reconcile-at"

func reconcileRequested(oldObj, newObj client.Object) bool {
	return oldObj.GetAnnotations()[reconcileAtAnnotation] !=
		newObj.GetAnnotations()[reconcileAtAnnotation]
}
```

Keep predicates narrow and documented. Over-filtering is a common cause of
controllers that look idle while the cluster has changed.

### Partial Metadata Watches

Use metadata-only watches for high-cardinality resources where the reconciler
only needs name, namespace, labels, annotations, owner references, or
resourceVersion. This is especially useful for Secrets because caching full
Secret data can be expensive and unnecessary.

```go
return ctrl.NewControllerManagedBy(mgr).
	For(&examplev1alpha1.NginxDeployment{}).
	WatchesMetadata(
		&corev1.Secret{},
		handler.EnqueueRequestsFromMapFunc(r.findByTLSSecretRef),
		builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
	).
	Complete(r)
```

Do not use metadata-only watches when the reconciler derives desired state from
the watched object's spec or status. For example, this template should keep a
full Deployment watch because parent availability depends on Deployment status.

### Typed Manager Configuration

Add typed manager configuration only after the operator has real runtime knobs:
namespace scope, leader election, cache options, feature gates, controller
concurrency, metrics, health, or webhook TLS.

Prefer a versioned config object over many unrelated flags:

```yaml
apiVersion: config.template-k8s.meigma.io/v1alpha1
kind: ControllerConfiguration
leaderElection:
  enabled: true
metrics:
  bindAddress: ":8080"
controller:
  groupKindConcurrency:
    example.meigma.io/NginxDeployment: 2
```

The Go side should load defaults, validate the config, then translate it into
`ctrl.Options` close to manager construction. Keep Moon tasks as the developer
front door; do not reintroduce generated Makefile paths for config workflows.

### Controller Class And Sharding

Use a controller class when multiple installations of the same operator may
share a cluster and should own disjoint resources. Do not add it to the starter
sample unless the template is explicitly demonstrating multi-controller
sharding.

```go
type NginxDeploymentSpec struct {
	// ControllerClassName selects which controller instance may reconcile this object.
	// Empty means the default controller handles it.
	// +optional
	ControllerClassName string `json:"controllerClassName,omitempty"`
}

func (r *NginxDeploymentReconciler) shouldReconcile(instance *examplev1alpha1.NginxDeployment) bool {
	return instance.Spec.ControllerClassName == "" ||
		instance.Spec.ControllerClassName == r.ControllerClassName
}
```

Apply the class check early in `Reconcile`, before mutating children or status.
If class selection becomes part of the public API, add e2e coverage with two
controller instances so ownership boundaries are proven.

### Desired Object Builders And Pruning

Keep inline `CreateOrPatch` for a fixed set of simple children. Move to desired
object builders when child resources become optional, generated from a list, or
spread across several Kubernetes kinds.

```go
func desiredObjects(instance *examplev1alpha1.NginxDeployment, config string) []client.Object {
	objects := []client.Object{
		desiredConfigMap(instance, config),
		desiredDeployment(instance, config),
		desiredService(instance),
	}
	if instance.Spec.Ingress != nil {
		objects = append(objects, desiredIngress(instance))
	}
	return objects
}
```

A reconciler can then apply every desired object and prune previously owned
objects that are no longer desired:

```go
for _, desired := range desiredObjects(instance, config) {
	if err := ctrl.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return err
	}
	if _, err := controllerutil.CreateOrPatch(ctx, r.Client, desired, mutateFrom(desired)); err != nil {
		return err
	}
	delete(staleOwned, client.ObjectKeyFromObject(desired))
}
```

Be conservative with pruning. Namespace-scoped owned children are usually safe
through owner references. Cluster-scoped resources need explicit finalizers,
careful labels, and tests that prove another resource is not accidentally
deleted.

### Webhooks For Cross-Field Invariants

Use CRD schema validation first. Add webhooks only when validation requires
parsing, external knowledge, warnings, complex defaults, or cross-field logic
that is too awkward for CEL.

For example, if the API supports either inline config or a config reference:

```go
func (w *NginxDeploymentWebhook) ValidateCreate(
	ctx context.Context,
	instance *examplev1alpha1.NginxDeployment,
) (admission.Warnings, error) {
	if instance.Spec.Config != "" && instance.Spec.ConfigRef != nil {
		return nil, field.Invalid(
			field.NewPath("spec", "configRef"),
			instance.Spec.ConfigRef,
			"config and configRef are mutually exclusive",
		)
	}
	return nil, nil
}
```

Webhook-backed rules need envtest coverage and should remain compatible with
the generated CRD schema. If a rule can be expressed clearly as Kubebuilder
validation markers, prefer the marker.

## API Shape

- If `0` is a valid value, do not combine a scalar field with `omitempty` and a
  CRD default. Use a pointer field and a helper that supplies the in-controller
  default when the pointer is nil.
- Keep defaults close to the reconciler too. API-server defaulting helps cluster
  objects, but tests and typed clients may construct objects directly.
- If child resources are named after the custom resource or the custom resource
  name is reused in labels, intentionally validate every downstream constraint
  or derive child names and selector labels from a stable label-safe hash. For
  same-named Services, length-only validation is insufficient because custom
  resource names can be DNS subdomains with dots, while Service names must be
  DNS labels.
- Bound inline strings that are copied into Kubernetes objects. For
  ConfigMap-backed fields, use a small `MaxLength` or a reference pattern; do
  not let the CR accept payloads the reconciler cannot materialize under the
  ConfigMap size limit.
- After changing API field types, regenerate deepcopy code and manifests. A Go
  pointer change may mainly show up in `zz_generated.deepcopy.go`.

## Reconcile Ownership

- Name simple owned children after the custom resource unless there is a real
  reason not to. It keeps smoke tests and cleanup obvious.
- Put stable labels on every owned child and use a narrower selector label set
  for pods/services when needed.
- Always set controller references for owned resources and register `.Owns(...)`
  watches in `SetupWithManager`.
- In `CreateOrPatch`, set the fields the controller owns instead of replacing
  entire specs by default. Broad spec replacement can fight API defaults and
  makes idempotence harder to reason about.
- When config changes should restart pods, hash the effective config into a pod
  template annotation.
- If status depends on a just-created or patched child, refetch the child before
  deriving status so generation and defaulted fields are current.
- Demo workloads should be compatible with Restricted Pod Security by default:
  use an unprivileged image and high port, set pod/container security contexts,
  drop Linux capabilities, disable privilege escalation, set seccomp, and add
  resource requests.
- RBAC markers should match current behavior. Do not leave generated primary-CR
  write verbs or finalizer verbs unless the reconciler actually uses them.

## Status

- Never mark a parent resource available from stale child status. For a managed
  Deployment, require `deployment.Status.ObservedGeneration >= deployment.Generation`
  before trusting ready replica counts.
- For positive availability, require the child availability condition as well
  as the desired ready replica count. Ready counts alone can be stale or
  incomplete.
- Set the parent condition `observedGeneration` to the custom resource
  generation.
- Patch status only when it changed. Noisy status writes create avoidable
  reconciles and hide meaningful transitions.
- Treat scale-to-zero as available when it is explicitly supported and the child
  status is fresh.

## Tests

- Envtest should prove owned child creation, owner refs, labels/selectors,
  desired images/ports/replicas, mounted config, restricted-compatible pod
  settings, resource requests, rollout hash annotations, and update behavior.
- Add stale-status tests when parent status depends on child status. Make the
  child available, change the parent spec, reconcile, and assert the parent does
  not stay available until the child observes the new generation.
- When manually setting Deployment status in envtest, set both
  `Status.ObservedGeneration = Generation` and a `DeploymentAvailable=True`
  condition before expecting the parent to report `Available=True`.
- Add a scale-to-zero test when zero replicas are supported, especially when the
  API field is optional or defaulted.

## E2E And Moon

- Moon is the task front door. Do not reintroduce generated Makefile paths into
  template tests or docs.
- Keep one runnable Chainsaw smoke path that installs the CRD/controller,
  applies the sample custom resource in a Restricted-enforced namespace, waits
  for the parent condition, and verifies the owned workload/service exist.
- For Kind-backed tests with locally loaded images, ensure the Deployment uses
  the exact loaded tag and `imagePullPolicy: IfNotPresent` before readiness
  waits. Default `:latest` behavior can force remote pulls.
- Prefer e2e task cleanup that removes only a Kind cluster the task created; do
  not delete a pre-existing developer cluster with the same name.
- Cluster-scoped e2e resources such as `ClusterRoleBinding` must be created
  idempotently or cleared before creation, then cleaned up in suite teardown.

## Verification

For ordinary controller/API changes, run:

```sh
moon run root:generate
moon run root:manifests
moon run root:test
moon run root:lint
moon ci --summary minimal
git diff --check
```

`root:test` wraps the envtest asset setup:
`KUBEBUILDER_ASSETS="$(setup-envtest use 1.35.x -p path)" go test ./...`.
Do not use plain `go test ./...` unless `KUBEBUILDER_ASSETS` or
`../../bin/k8s` is already populated.

When changing e2e wiring, also run:

```sh
chainsaw version
moon run root:chainsaw-lint
moon run root:test-e2e
```
