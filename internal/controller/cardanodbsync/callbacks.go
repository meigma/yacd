package cardanodbsync

import (
	"fmt"
	"maps"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	controllerstorage "github.com/meigma/yacd/internal/controller/storage"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	ctrlresources "github.com/meigma/yacd/internal/ctrlkit/resources"
	ctrlstorage "github.com/meigma/yacd/internal/ctrlkit/storage"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Validate and Mutate callbacks passed to ctrlkit/apply.ApplyOwnedObject
// for every owned child the reconciler reconciles. Validate rejects
// unsupported drifts (Kubernetes-immutable fields, storage class drift)
// so the apply does not produce a confusing partial-update result;
// Mutate merges desired fields into the live object while preserving
// Kubernetes-assigned fields.

// mutateDBSyncConfigMap is the ApplyOwnedObject Mutate callback for the
// dbsync config ConfigMap. Replaces Data and BinaryData wholesale because
// the ConfigMap is controller-owned content.
func mutateDBSyncConfigMap(current *corev1.ConfigMap, desired *corev1.ConfigMap) error {
	current.Labels = ctrlmetadata.OverlayStringMap(current.Labels, desired.Labels)
	current.Annotations = mergeDBSyncOwnedAnnotations(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences
	current.Data = maps.Clone(desired.Data)
	current.BinaryData = maps.Clone(desired.BinaryData)

	return nil
}

// mutateDBSyncPGPassSecret is the ApplyOwnedObject Mutate callback for the
// pgpass Secret. The Type field is pinned to Opaque so a sniffed type
// cannot drift the Secret into a Kubernetes-managed type.
func mutateDBSyncPGPassSecret(current *corev1.Secret, desired *corev1.Secret) error {
	current.Labels = ctrlmetadata.OverlayStringMap(current.Labels, desired.Labels)
	current.Annotations = mergeDBSyncOwnedAnnotations(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences
	current.Type = corev1.SecretTypeOpaque
	current.Data = maps.Clone(desired.Data)
	current.StringData = nil

	return nil
}

// validateDBSyncPersistentVolumeClaim rejects a PVC apply that would change
// the storage class or shrink capacity. Kubernetes does not allow either
// in-place.
func validateDBSyncPersistentVolumeClaim(current *corev1.PersistentVolumeClaim, desired *corev1.PersistentVolumeClaim) error {
	if drift, changed := ctrlstorage.PersistentVolumeClaimDriftFor(current, desired, ctrlannotations.RequestedStorageClass); changed {
		return controllerstorage.UnsupportedPersistentVolumeClaimDrift(string(conditionReasonUnsupportedStorageChange), desired, drift)
	}

	return nil
}

// mutateDBSyncPersistentVolumeClaim is the ApplyOwnedObject Mutate callback
// for a CardanoDBSync-owned PVC. Delegates to the ctrlkit helper, which
// preserves Kubernetes-assigned spec fields and merges the cardanodbsync-
// owned annotation set.
func mutateDBSyncPersistentVolumeClaim(current *corev1.PersistentVolumeClaim, desired *corev1.PersistentVolumeClaim) error {
	ctrlresources.MutatePersistentVolumeClaim(current, desired, mergeDBSyncOwnedAnnotations)

	return nil
}

// validateDBSyncDeployment rejects a Deployment apply that would change the
// pod-template selector. Kubernetes does not allow selector changes on a
// Deployment after creation.
func validateDBSyncDeployment(current *appsv1.Deployment, desired *appsv1.Deployment) error {
	if !equality.Semantic.DeepEqual(current.Spec.Selector, desired.Spec.Selector) {
		return unsupportedWorkloadChange(
			"Deployment %s selector drifted from desired value",
			ctrlmetadata.ObjectKey(desired),
		)
	}

	return nil
}

// mutateDBSyncDeployment is the ApplyOwnedObject Mutate callback for a
// CardanoDBSync-owned Deployment. Replaces the controller-owned pod spec
// fields with desired and delegates ObjectMeta merging to the ctrlkit
// helper.
func mutateDBSyncDeployment(current *appsv1.Deployment, desired *appsv1.Deployment) error {
	ctrlresources.MutateDeployment(current, desired, mergeDBSyncOwnedAnnotations, func(current *corev1.PodSpec, desired *corev1.PodSpec) {
		current.AutomountServiceAccountToken = desired.AutomountServiceAccountToken
		current.SecurityContext = desired.SecurityContext
		current.Containers = desired.Containers
		current.Volumes = desired.Volumes
	})

	return nil
}

// mutateDBSyncService is the ApplyOwnedObject Mutate callback for a
// CardanoDBSync-owned Service. The ctrlkit helper preserves Kubernetes-
// assigned ClusterIP / ClusterIPs / NodePort / IPFamilies fields.
func mutateDBSyncService(current *corev1.Service, desired *corev1.Service) error {
	ctrlresources.MutateService(current, desired, nil)

	return nil
}

// validateControllerOwner asserts that current is owned by the same
// controller as desired. Wraps the ctrlkit ownership check into a
// resourceConflict error the reconciler can surface as a Degraded
// condition with the canonical reason string.
func validateControllerOwner(current metav1.Object, desired metav1.Object) error {
	if err := ctrlmetadata.ValidateControllerOwner(current, desired); err != nil {
		return controllerOwnerConflict(err)
	}

	return nil
}

// controlledBy reports whether owner controls the current object,
// expressed in terms of the CardanoDBSync GroupVersionKind.
func controlledBy(current metav1.Object, owner metav1.Object) bool {
	return ctrlmetadata.ControlledBy(current, owner, yacdv1alpha1.GroupVersion.String(), "CardanoDBSync")
}

// defaultObject runs the Scheme defaulting hooks against the desired
// object. Used as the Default callback in ApplyOwnedObject for resources
// whose runtime defaults the reconciler needs filled in before
// comparison.
func (r *CardanoDBSyncReconciler) defaultObject(object client.Object) error {
	if r.Scheme == nil {
		return fmt.Errorf("scheme is required")
	}
	r.Scheme.Default(object)

	return nil
}
