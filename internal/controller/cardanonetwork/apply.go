package cardanonetwork

import (
	"context"

	controllerstorage "github.com/meigma/yacd/internal/controller/storage"
	ctrlapply "github.com/meigma/yacd/internal/ctrlkit/apply"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Apply orchestrators for the primary workload's owned children. Each
// method delegates to ctrlkit.apply.ApplyOwnedObject with the relevant
// Validate/Mutate callbacks (callbacks.go) so the create-read-owner-check-
// validate-mutate-persist skeleton stays uniform across resource types.

// applyPrimaryPersistentVolumeClaim applies the primary node state PVC. The
// UpdateModeUpdate switch is required because PVCs reject server-side patch
// for spec fields Kubernetes treats as immutable.
func (r *CardanoNetworkReconciler) applyPrimaryPersistentVolumeClaim(
	ctx context.Context,
	desired *corev1.PersistentVolumeClaim,
	acceptedIdentity acceptedNetworkIdentity,
) (controllerutil.OperationResult, error) {
	result, _, err := ctrlapply.ApplyOwnedObject(ctx, r.Client, desired, ctrlapply.OwnedObjectOptions[*corev1.PersistentVolumeClaim]{
		Current:       &corev1.PersistentVolumeClaim{},
		OwnerConflict: controllerOwnerConflict,
		ValidateCreate: func(desired *corev1.PersistentVolumeClaim) error {
			return validatePrimaryPersistentVolumeClaimCreate(desired, acceptedIdentity)
		},
		ObjectDeleting: childBeingDeleted[*corev1.PersistentVolumeClaim],
		Validate:       validatePrimaryPersistentVolumeClaim,
		Mutate:         mutatePrimaryPersistentVolumeClaim,
		UpdateMode:     ctrlapply.UpdateModeUpdate,
		UpdateError: func(current *corev1.PersistentVolumeClaim, desired *corev1.PersistentVolumeClaim, err error) error {
			return controllerstorage.PersistentVolumeClaimUpdateError(string(conditionReasonStorageExpansionRejected), current, desired, err)
		},
	})
	return result, err
}

// validateAcceptedPrimaryPersistentVolumeClaim checks the live primary PVC's
// accepted network fingerprint before other children are mutated. The apply
// callback repeats this validation, but this early gate prevents profile drift
// from patching artifacts or rolling the Deployment first.
func (r *CardanoNetworkReconciler) validateAcceptedPrimaryPersistentVolumeClaim(
	ctx context.Context,
	desired *corev1.PersistentVolumeClaim,
	acceptedIdentity acceptedNetworkIdentity,
) error {
	current := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current); err != nil {
		if apierrors.IsNotFound(err) {
			return validatePrimaryPersistentVolumeClaimCreate(desired, acceptedIdentity)
		}
		return err
	}
	if err := validateControllerOwner(current, desired); err != nil {
		return err
	}
	if !current.DeletionTimestamp.IsZero() {
		return childBeingDeleted(current, desired)
	}

	return validateLocalnetFingerprint(current, desired)
}

// applyPrimaryDeployment applies the primary node Deployment.
func (r *CardanoNetworkReconciler) applyPrimaryDeployment(
	ctx context.Context,
	desired *appsv1.Deployment,
) (controllerutil.OperationResult, error) {
	result, _, err := ctrlapply.ApplyOwnedObject(ctx, r.Client, desired, ctrlapply.OwnedObjectOptions[*appsv1.Deployment]{
		Current:        &appsv1.Deployment{},
		Default:        func(desired *appsv1.Deployment) error { return r.defaultObject(desired) },
		OwnerConflict:  controllerOwnerConflict,
		ObjectDeleting: childBeingDeleted[*appsv1.Deployment],
		Validate:       validatePrimaryDeployment,
		Mutate:         mutatePrimaryDeployment,
	})
	return result, err
}

// applyPrimaryService applies a Service through the shared mutator. The
// orchestrator in controller.go reuses it for the optional chain API
// Services too because their mutation shape is identical to the primary
// node-to-node Service.
func (r *CardanoNetworkReconciler) applyPrimaryService(
	ctx context.Context,
	desired *corev1.Service,
) (controllerutil.OperationResult, error) {
	result, _, err := ctrlapply.ApplyOwnedObject(ctx, r.Client, desired, ctrlapply.OwnedObjectOptions[*corev1.Service]{
		Current:        &corev1.Service{},
		Default:        func(desired *corev1.Service) error { return r.defaultObject(desired) },
		OwnerConflict:  controllerOwnerConflict,
		ObjectDeleting: childBeingDeleted[*corev1.Service],
		Mutate:         mutatePrimaryService,
	})
	return result, err
}
