package ssa

import (
	"context"
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// fieldOwner identifies the CLI in server-side apply field-ownership
	// records. It matches the kube adapter's owner so the CLI presents one
	// field manager across network applies and operator installs.
	fieldOwner = "yacd-cli"

	// pruneLabelKey/pruneLabelValue mark every object the installer applies so
	// the prune pass can find managed objects to remove. The key is yacd-owned
	// so it never collides with the chart's own app.kubernetes.io/managed-by.
	pruneLabelKey   = "yacd.meigma.io/install"
	pruneLabelValue = "operator"

	// managerNameLabel/managerNameValue and controlPlaneLabel/controlPlaneValue
	// select the operator manager Deployment, which the chart stamps on it.
	managerNameLabel  = "app.kubernetes.io/name"
	managerNameValue  = "yacd"
	controlPlaneLabel = "control-plane"
	controlPlaneValue = "controller-manager"

	// versionLabel holds the installed operator version (the chart's
	// appVersion), stamped on every object including the manager Deployment.
	versionLabel = "app.kubernetes.io/version"
)

// crdGVK is the GroupVersionKind of a CustomResourceDefinition; it orders first
// on apply and is excluded from the prune delete pass.
var crdGVK = schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"}

// managedGVKs are the kinds the operator chart emits that the prune pass may
// delete. CustomResourceDefinition is intentionally absent: deleting a CRD
// cascades to user custom resources, so CRDs are apply/upgrade-only.
var managedGVKs = []schema.GroupVersionKind{
	{Group: "", Version: "v1", Kind: "ServiceAccount"},
	{Group: "", Version: "v1", Kind: "Service"},
	{Group: "apps", Version: "v1", Kind: "Deployment"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
}

// applyKindRank gives a deterministic, dependency-friendly apply order for the
// non-CRD objects: identity and permissions before the workload that uses them.
var applyKindRank = map[string]int{
	"ServiceAccount":     0,
	"ClusterRole":        1,
	"Role":               2,
	"ClusterRoleBinding": 3,
	"RoleBinding":        4,
	"Service":            5,
	"Deployment":         6,
}

// partitionCRDs splits objects into CustomResourceDefinitions and everything
// else, returning the rest sorted by applyKindRank then name for determinism.
func partitionCRDs(objs []*unstructured.Unstructured) (crds, rest []*unstructured.Unstructured) {
	for _, obj := range objs {
		if obj.GroupVersionKind() == crdGVK {
			crds = append(crds, obj)
			continue
		}
		rest = append(rest, obj)
	}

	sort.SliceStable(rest, func(i, j int) bool {
		ri, rj := applyKindRank[rest[i].GetKind()], applyKindRank[rest[j].GetKind()]
		if ri != rj {
			return ri < rj
		}
		return rest[i].GetName() < rest[j].GetName()
	})

	return crds, rest
}

// stampPruneLabel adds the install marker label without dropping labels the
// chart already set.
func stampPruneLabel(obj *unstructured.Unstructured) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[pruneLabelKey] = pruneLabelValue
	obj.SetLabels(labels)
}

// isNamespaced reports whether the object's kind is namespace-scoped, using the
// cluster's RESTMapper.
func isNamespaced(mapper apimeta.RESTMapper, gvk schema.GroupVersionKind) (bool, error) {
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return false, fmt.Errorf("map %s: %w", gvk, err)
	}
	return mapping.Scope.Name() == apimeta.RESTScopeNameNamespace, nil
}

// applyObject server-side applies one object under the CLI field owner. It
// stamps the prune label and defaults a namespace onto namespaced objects that
// the chart left unqualified. It returns the object's identity key for the
// applied set.
func applyObject(
	ctx context.Context,
	c client.Client,
	mapper apimeta.RESTMapper,
	obj *unstructured.Unstructured,
	namespace string,
) (objectKey, error) {
	stampPruneLabel(obj)

	gvk := obj.GroupVersionKind()
	namespaced, err := isNamespaced(mapper, gvk)
	if err != nil {
		return objectKey{}, err
	}
	if namespaced && obj.GetNamespace() == "" {
		obj.SetNamespace(namespace)
	}
	if !namespaced {
		obj.SetNamespace("")
	}

	//nolint:staticcheck // client.Apply is still the practical SSA path for unstructured objects without generated apply configurations.
	if err := c.Patch(ctx, obj, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return objectKey{}, fmt.Errorf("apply %s %s/%s: %w", gvk.Kind, obj.GetNamespace(), obj.GetName(), err)
	}

	return objectKey{gvk: gvk, namespace: obj.GetNamespace(), name: obj.GetName()}, nil
}

// objectKey identifies an applied object for prune comparison.
type objectKey struct {
	gvk       schema.GroupVersionKind
	namespace string
	name      string
}

// prune deletes managed objects carrying the install label that are not in the
// applied set. CustomResourceDefinitions are never deleted.
func prune(
	ctx context.Context,
	c client.Client,
	mapper apimeta.RESTMapper,
	namespace string,
	applied map[objectKey]struct{},
) error {
	for _, gvk := range managedGVKs {
		namespaced, err := isNamespaced(mapper, gvk)
		if err != nil {
			return err
		}

		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)
		opts := []client.ListOption{client.MatchingLabels{pruneLabelKey: pruneLabelValue}}
		if namespaced {
			opts = append(opts, client.InNamespace(namespace))
		}
		if err := c.List(ctx, list, opts...); err != nil {
			return fmt.Errorf("list %s for prune: %w", gvk.Kind, err)
		}

		for i := range list.Items {
			item := &list.Items[i]
			key := objectKey{gvk: gvk, namespace: item.GetNamespace(), name: item.GetName()}
			if _, kept := applied[key]; kept {
				continue
			}
			if err := c.Delete(ctx, item); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("prune %s %s/%s: %w", gvk.Kind, item.GetNamespace(), item.GetName(), err)
			}
		}
	}

	return nil
}

// versionFromObjects reads the operator version (app.kubernetes.io/version) off
// the manager Deployment in a set of parsed objects. It is how the adapter
// learns the version it is about to install from its own embedded manifests.
func versionFromObjects(objs []*unstructured.Unstructured) (string, error) {
	for _, obj := range objs {
		if obj.GetKind() != "Deployment" {
			continue
		}
		labels := obj.GetLabels()
		if labels[managerNameLabel] != managerNameValue || labels[controlPlaneLabel] != controlPlaneValue {
			continue
		}
		version := labels[versionLabel]
		if version == "" {
			return "", fmt.Errorf("manager Deployment has no %s label", versionLabel)
		}
		return version, nil
	}

	return "", fmt.Errorf("manager Deployment not found in embedded manifests")
}

// deploymentAvailable reports whether a Deployment is Available for its current
// generation with at least one available replica. It is pure so the readiness
// rule is unit-testable without a cluster.
func deploymentAvailable(deployment *appsv1.Deployment) bool {
	if deployment.Status.ObservedGeneration < deployment.Generation {
		return false
	}
	if deployment.Status.AvailableReplicas < 1 {
		return false
	}
	for _, cond := range deployment.Status.Conditions {
		if cond.Type == appsv1.DeploymentAvailable {
			return cond.Status == corev1.ConditionTrue
		}
	}

	return false
}

// namespaceObject builds the install Namespace object to apply before the chart
// resources land in it.
func namespaceObject(name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("v1")
	obj.SetKind("Namespace")
	obj.SetName(name)
	return obj
}

// crdNames returns the metadata names of the CRD objects, for the Established
// wait.
func crdNames(crds []*unstructured.Unstructured) []string {
	names := make([]string, 0, len(crds))
	for _, crd := range crds {
		names = append(names, crd.GetName())
	}
	return names
}

// establishedTrue reports whether a CRD carries Established=True.
func establishedTrue(crd *apiextensionsv1.CustomResourceDefinition) bool {
	for _, cond := range crd.Status.Conditions {
		if cond.Type == apiextensionsv1.Established {
			return cond.Status == apiextensionsv1.ConditionTrue
		}
	}
	return false
}
