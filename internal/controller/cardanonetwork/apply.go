package cardanonetwork

import (
	"context"

	ctrlapply "github.com/meigma/yacd/internal/ctrlkit/apply"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Apply orchestrators for the primary workload's owned children. Each
// method delegates to ctrlkit.apply.ApplyOwnedObject with the relevant
// Validate/Mutate callbacks (callbacks.go) so the create-read-owner-check-
// validate-mutate-persist skeleton stays uniform across resource types.
//
// applyNetworkArtifactsConfigMap (below) is the deliberate exception: the
// ConfigMap has delete-and-recover semantics that do not fit the
// mutate-in-place ApplyOwnedObject model, so its flow is inlined.

// applyPrimaryPersistentVolumeClaim applies the primary node state PVC. The
// UpdateModeUpdate switch is required because PVCs reject server-side patch
// for spec fields Kubernetes treats as immutable.
func (r *CardanoNetworkReconciler) applyPrimaryPersistentVolumeClaim(
	ctx context.Context,
	desired *corev1.PersistentVolumeClaim,
) (controllerutil.OperationResult, error) {
	result, _, err := ctrlapply.ApplyOwnedObject(ctx, r.Client, desired, ctrlapply.OwnedObjectOptions[*corev1.PersistentVolumeClaim]{
		Current:       &corev1.PersistentVolumeClaim{},
		OwnerConflict: controllerOwnerConflict,
		Validate:      validatePrimaryPersistentVolumeClaim,
		Mutate:        mutatePrimaryPersistentVolumeClaim,
		UpdateMode:    ctrlapply.UpdateModeUpdate,
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
) error {
	current := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := validateControllerOwner(current, desired); err != nil {
		return err
	}

	return validateLocalnetFingerprint(current, desired)
}

// applyPrimaryDeployment applies the primary node Deployment.
func (r *CardanoNetworkReconciler) applyPrimaryDeployment(
	ctx context.Context,
	desired *appsv1.Deployment,
) (controllerutil.OperationResult, error) {
	result, _, err := ctrlapply.ApplyOwnedObject(ctx, r.Client, desired, ctrlapply.OwnedObjectOptions[*appsv1.Deployment]{
		Current:       &appsv1.Deployment{},
		Default:       func(desired *appsv1.Deployment) error { return r.defaultObject(desired) },
		OwnerConflict: controllerOwnerConflict,
		Validate:      validatePrimaryDeployment,
		Mutate:        mutatePrimaryDeployment,
	})
	return result, err
}

// applyNetworkArtifactsConfigMap reconciles the artifact ConfigMap.
//
// The ConfigMap is intentionally NOT routed through ApplyOwnedObject because
// it has special delete-and-recover semantics: when the live ConfigMap fails
// the producer-side verification (data hash drift, missing keys, schema
// drift) we delete the live object and let the next reconcile recreate it.
// The new ConfigMap UID is stamped into the Deployment pod-template
// annotations, which is how the primary Pod rolls to pick up republished
// localnet files. The mutate-in-place model ApplyOwnedObject implements
// cannot satisfy that invariant.
func (r *CardanoNetworkReconciler) applyNetworkArtifactsConfigMap(
	ctx context.Context,
	desired *corev1.ConfigMap,
) (controllerutil.OperationResult, *corev1.ConfigMap, error) {
	desired = desired.DeepCopy()
	current := &corev1.ConfigMap{}
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
			return controllerutil.OperationResultNone, nil, err
		}

		return controllerutil.OperationResultCreated, desired, nil
	}
	if err != nil {
		return controllerutil.OperationResultNone, nil, err
	}

	if err := validateControllerOwner(current, desired); err != nil {
		return controllerutil.OperationResultNone, nil, err
	}

	// A delete already in flight: skip the patch and let the next reconcile
	// hit the NotFound branch and recreate.
	if !current.DeletionTimestamp.IsZero() {
		return controllerutil.OperationResultUpdated, current, nil
	}

	// Recovery path: the live ConfigMap is missing required keys, has
	// foreign data keys, or otherwise fails verification. Delete it and let
	// the next reconcile recreate; the new UID rolls the Deployment so the
	// init publisher can republish.
	if artifactConfigMapNeedsRecovery(current, desired.Annotations[networkFingerprintAnno]) {
		if err := r.Delete(ctx, current); err != nil && !apierrors.IsNotFound(err) {
			return controllerutil.OperationResultNone, nil, err
		}

		return controllerutil.OperationResultUpdated, current, nil
	}

	before := current.DeepCopy()
	current.Labels = ctrlmetadata.OverlayStringMap(current.Labels, desired.Labels)
	current.Annotations = ctrlmetadata.OverlayStringMap(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences
	if len(desired.Data) > 0 {
		current.Data = desired.Data
		current.BinaryData = nil
	}

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, current, nil
	}
	if err := r.Patch(ctx, current, client.MergeFrom(before)); err != nil {
		return controllerutil.OperationResultNone, nil, err
	}

	return controllerutil.OperationResultUpdated, current, nil
}

// applyArtifactPublisherServiceAccount applies the artifact publisher
// ServiceAccount.
func (r *CardanoNetworkReconciler) applyArtifactPublisherServiceAccount(
	ctx context.Context,
	desired *corev1.ServiceAccount,
) (controllerutil.OperationResult, error) {
	result, _, err := ctrlapply.ApplyOwnedObject(ctx, r.Client, desired, ctrlapply.OwnedObjectOptions[*corev1.ServiceAccount]{
		Current:       &corev1.ServiceAccount{},
		OwnerConflict: controllerOwnerConflict,
		Mutate:        mutateArtifactPublisherServiceAccount,
	})
	return result, err
}

// applyArtifactPublisherRole applies the artifact publisher Role. The Role
// is resourceName-scoped to the network artifact ConfigMap.
func (r *CardanoNetworkReconciler) applyArtifactPublisherRole(
	ctx context.Context,
	desired *rbacv1.Role,
) (controllerutil.OperationResult, error) {
	result, _, err := ctrlapply.ApplyOwnedObject(ctx, r.Client, desired, ctrlapply.OwnedObjectOptions[*rbacv1.Role]{
		Current:       &rbacv1.Role{},
		OwnerConflict: controllerOwnerConflict,
		Mutate:        mutateArtifactPublisherRole,
	})
	return result, err
}

// applyArtifactPublisherRoleBinding applies the artifact publisher
// RoleBinding.
func (r *CardanoNetworkReconciler) applyArtifactPublisherRoleBinding(
	ctx context.Context,
	desired *rbacv1.RoleBinding,
) (controllerutil.OperationResult, error) {
	result, _, err := ctrlapply.ApplyOwnedObject(ctx, r.Client, desired, ctrlapply.OwnedObjectOptions[*rbacv1.RoleBinding]{
		Current:       &rbacv1.RoleBinding{},
		OwnerConflict: controllerOwnerConflict,
		Validate:      validateArtifactPublisherRoleBinding,
		Mutate:        mutateArtifactPublisherRoleBinding,
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
		Current:       &corev1.Service{},
		Default:       func(desired *corev1.Service) error { return r.defaultObject(desired) },
		OwnerConflict: controllerOwnerConflict,
		Mutate:        mutatePrimaryService,
	})
	return result, err
}
