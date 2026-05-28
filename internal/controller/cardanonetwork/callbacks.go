package cardanonetwork

import (
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	controllerstorage "github.com/meigma/yacd/internal/controller/storage"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	ctrlresources "github.com/meigma/yacd/internal/ctrlkit/resources"
	ctrlstorage "github.com/meigma/yacd/internal/ctrlkit/storage"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// validatePrimaryPersistentVolumeClaim is the ApplyOwnedObject Validate
// callback for the primary node PVC. It rejects two unsupported drifts: a
// changed localnet fingerprint (cannot reuse state for a different network
// shape) and a changed storage class or shrunk capacity (Kubernetes does not
// allow these in-place).
func validatePrimaryPersistentVolumeClaim(current *corev1.PersistentVolumeClaim, desired *corev1.PersistentVolumeClaim) error {
	if err := validateLocalnetFingerprint(current, desired); err != nil {
		return err
	}
	if drift, changed := ctrlstorage.PersistentVolumeClaimDriftFor(current, desired, ctrlannotations.RequestedStorageClass); changed {
		return controllerstorage.UnsupportedPersistentVolumeClaimDrift(string(conditionReasonUnsupportedStorageChange), desired, drift)
	}

	return nil
}

// mutatePrimaryPersistentVolumeClaim is the ApplyOwnedObject Mutate callback
// for the primary node PVC. The underlying ctrlkit helper preserves
// Kubernetes-assigned spec fields and merges the cardanonetwork-owned
// annotation set.
func mutatePrimaryPersistentVolumeClaim(current *corev1.PersistentVolumeClaim, desired *corev1.PersistentVolumeClaim) error {
	ctrlresources.MutatePersistentVolumeClaim(current, desired, mergeOwnedAnnotations)

	return nil
}

// validatePrimaryDeployment is the ApplyOwnedObject Validate callback for
// the primary Deployment. Kubernetes does not allow selector changes on a
// Deployment after creation, so we reject drift before attempting the patch.
func validatePrimaryDeployment(current *appsv1.Deployment, desired *appsv1.Deployment) error {
	if !equality.Semantic.DeepEqual(current.Spec.Selector, desired.Spec.Selector) {
		return unsupportedWorkloadChange(
			"Deployment %s selector drifted from desired value",
			ctrlmetadata.ObjectKey(desired),
		)
	}

	return nil
}

// mutatePrimaryDeployment is the ApplyOwnedObject Mutate callback for the
// primary Deployment. It replaces the controller-owned pod spec fields with
// desired while delegating ObjectMeta merging and the cardanonetwork-owned
// annotation overlay to the shared helper.
func mutatePrimaryDeployment(current *appsv1.Deployment, desired *appsv1.Deployment) error {
	ctrlresources.MutateDeployment(current, desired, mergeOwnedAnnotations, func(current *corev1.PodSpec, desired *corev1.PodSpec) {
		current.ServiceAccountName = desired.ServiceAccountName
		current.AutomountServiceAccountToken = desired.AutomountServiceAccountToken
		current.SecurityContext = desired.SecurityContext
		current.InitContainers = desired.InitContainers
		current.Containers = desired.Containers
		current.Volumes = desired.Volumes
	})
	if _, desiredDBSyncLabel := desired.Spec.Template.Labels[labelDBSync]; !desiredDBSyncLabel && current.Spec.Template.Labels != nil {
		delete(current.Spec.Template.Labels, labelDBSync)
		if len(current.Spec.Template.Labels) == 0 {
			current.Spec.Template.Labels = nil
		}
	}

	return nil
}

// mutateArtifactPublisherServiceAccount is the Mutate callback for the
// artifact publisher ServiceAccount. AutomountServiceAccountToken is
// explicitly false because the init container takes its token through a
// projected volume instead.
func mutateArtifactPublisherServiceAccount(current *corev1.ServiceAccount, desired *corev1.ServiceAccount) error {
	ctrlresources.MutateObjectMetadata(current, desired, nil)
	current.AutomountServiceAccountToken = desired.AutomountServiceAccountToken

	return nil
}

// mutateArtifactPublisherRole is the Mutate callback for the artifact
// publisher Role. The Rules slice carries the resourceNames-scoped
// configmaps grant.
func mutateArtifactPublisherRole(current *rbacv1.Role, desired *rbacv1.Role) error {
	ctrlresources.MutateObjectMetadata(current, desired, nil)
	current.Rules = desired.Rules

	return nil
}

// validateArtifactPublisherRoleBinding is the Validate callback for the
// artifact publisher RoleBinding. RoleRef is immutable on a RoleBinding, so
// we reject drift before attempting the patch.
func validateArtifactPublisherRoleBinding(current *rbacv1.RoleBinding, desired *rbacv1.RoleBinding) error {
	if !equality.Semantic.DeepEqual(current.RoleRef, desired.RoleRef) {
		return unsupportedWorkloadChange(
			"RoleBinding %s roleRef drifted from desired value",
			ctrlmetadata.ObjectKey(desired),
		)
	}

	return nil
}

// mutateArtifactPublisherRoleBinding is the Mutate callback for the artifact
// publisher RoleBinding.
func mutateArtifactPublisherRoleBinding(current *rbacv1.RoleBinding, desired *rbacv1.RoleBinding) error {
	ctrlresources.MutateObjectMetadata(current, desired, nil)
	current.Subjects = desired.Subjects

	return nil
}

// mutatePrimaryService is the Mutate callback for the node-to-node Service.
// The ctrlkit helper preserves Kubernetes-assigned ClusterIP / ClusterIPs /
// NodePort / IPFamilies fields.
func mutatePrimaryService(current *corev1.Service, desired *corev1.Service) error {
	ctrlresources.MutateService(current, desired, nil)

	return nil
}

// validateControllerOwner asserts that current is owned by the same
// controller as desired. It wraps the ctrlkit ownership check into a
// resourceConflict error the reconciler can surface as a Degraded condition
// with the canonical reason string.
func validateControllerOwner(current metav1.Object, desired metav1.Object) error {
	if err := ctrlmetadata.ValidateControllerOwner(current, desired); err != nil {
		return controllerOwnerConflict(err)
	}

	return nil
}

// validateAcceptedNetworkFingerprint rejects desired-state changes that would
// alter network inputs after the CardanoNetwork has accepted a fingerprint.
// The CardanoNetwork must be deleted and recreated to change network
// parameters. Localnet status written before the mode-neutral field existed is
// accepted through the localnetFingerprint fallback.
func validateAcceptedNetworkFingerprint(network *yacdv1alpha1.CardanoNetwork, desired primaryNetworkPlan) error {
	if network.Status.Network == nil {
		return nil
	}

	if desiredLocalnetFingerprint := desired.localnetFingerprint(); desiredLocalnetFingerprint != "" &&
		network.Status.Network.LocalnetFingerprint != "" {
		if network.Status.Network.LocalnetFingerprint == desiredLocalnetFingerprint {
			return nil
		}
		return unsupportedLocalnetChange(
			"CardanoNetwork localnet inputs changed from accepted fingerprint; delete and recreate the CardanoNetwork to change network parameters",
		)
	}

	if network.Status.Network.NetworkFingerprint == "" {
		return nil
	}
	if network.Status.Network.NetworkFingerprint == desired.Fingerprint {
		return nil
	}

	return unsupportedNetworkChange(
		"CardanoNetwork network inputs changed from accepted fingerprint; delete and recreate the CardanoNetwork to change network parameters",
	)
}

// validateLocalnetFingerprint rejects a PVC apply when the live PVC's
// localnet fingerprint annotation is missing or drifts from desired. The
// localnet state is per-fingerprint, so we cannot safely reuse the PVC
// across drift.
func validateLocalnetFingerprint(current *corev1.PersistentVolumeClaim, desired *corev1.PersistentVolumeClaim) error {
	desiredLocalnetFingerprint := desired.Annotations[localnetFingerprintAnno]
	if desiredLocalnetFingerprint != "" {
		currentFingerprint := current.Annotations[localnetFingerprintAnno]
		if currentFingerprint == "" {
			return missingLocalnetFingerprint(
				"PVC %s is missing localnet fingerprint annotation; delete and recreate the CardanoNetwork to recreate localnet state",
				ctrlmetadata.ObjectKey(desired),
			)
		}

		if currentFingerprint != desiredLocalnetFingerprint {
			return unsupportedLocalnetChange(
				"CardanoNetwork localnet inputs changed for PVC %s; delete and recreate the CardanoNetwork to change network parameters",
				ctrlmetadata.ObjectKey(desired),
			)
		}

		return nil
	}

	currentFingerprint := current.Annotations[networkFingerprintAnno]
	if currentFingerprint == "" {
		return missingNetworkFingerprint(
			"PVC %s is missing network fingerprint annotation; delete and recreate the CardanoNetwork to recreate network state",
			ctrlmetadata.ObjectKey(desired),
		)
	}

	desiredFingerprint := desired.Annotations[networkFingerprintAnno]
	if currentFingerprint != desiredFingerprint {
		return unsupportedNetworkChange(
			"CardanoNetwork network inputs changed for PVC %s; delete and recreate the CardanoNetwork to change network parameters",
			ctrlmetadata.ObjectKey(desired),
		)
	}

	return nil
}

// defaultObject runs the Scheme defaulting hooks against the desired object.
// Used as the Default callback in ApplyOwnedObject for resources whose
// runtime defaults (Service cluster IP family policy, PodSpec defaults) the
// reconciler needs filled in before comparison.
func (r *CardanoNetworkReconciler) defaultObject(object client.Object) error {
	if r.Scheme == nil {
		return fmt.Errorf("scheme is required")
	}
	r.Scheme.Default(object)

	return nil
}
