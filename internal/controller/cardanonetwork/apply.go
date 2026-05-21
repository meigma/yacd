package cardanonetwork

import (
	"context"
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type unsupportedApplyError struct {
	reason  string
	message string
}

func (e unsupportedApplyError) Error() string {
	return e.message
}

func (r *CardanoNetworkReconciler) applyPrimaryPersistentVolumeClaim(
	ctx context.Context,
	desired *corev1.PersistentVolumeClaim,
) (controllerutil.OperationResult, error) {
	current := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, clientObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired.DeepCopy()); err != nil {
			return controllerutil.OperationResultNone, err
		}

		return controllerutil.OperationResultCreated, nil
	}
	if err != nil {
		return controllerutil.OperationResultNone, err
	}

	if err := validateLocalnetFingerprint(current, desired); err != nil {
		return controllerutil.OperationResultNone, err
	}

	if !storageClassCompatible(current.Spec.StorageClassName, desired.Spec.StorageClassName) {
		return controllerutil.OperationResultNone, unsupportedStorageChange(
			"PVC %s storageClassName cannot be changed from %s to %s",
			clientObjectKey(desired),
			stringPtrValue(current.Spec.StorageClassName),
			stringPtrValue(desired.Spec.StorageClassName),
		)
	}
	if !reflect.DeepEqual(current.Spec.AccessModes, desired.Spec.AccessModes) {
		return controllerutil.OperationResultNone, unsupportedStorageChange(
			"PVC %s accessModes drifted from desired value",
			clientObjectKey(desired),
		)
	}

	currentStorage := current.Spec.Resources.Requests[corev1.ResourceStorage]
	desiredStorage := desired.Spec.Resources.Requests[corev1.ResourceStorage]
	if currentStorage.Cmp(desiredStorage) > 0 {
		return controllerutil.OperationResultNone, unsupportedStorageChange(
			"PVC %s storage cannot be decreased from %s to %s",
			clientObjectKey(desired),
			currentStorage.String(),
			desiredStorage.String(),
		)
	}

	before := current.DeepCopy()
	current.Labels = desired.Labels
	current.Annotations = desired.Annotations
	current.OwnerReferences = desired.OwnerReferences
	if current.Spec.Resources.Requests == nil {
		current.Spec.Resources.Requests = corev1.ResourceList{}
	}
	if currentStorage.Cmp(desiredStorage) < 0 {
		current.Spec.Resources.Requests[corev1.ResourceStorage] = desiredStorage
	}

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, nil
	}
	if err := r.Update(ctx, current); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.OperationResultUpdated, nil
}

func (r *CardanoNetworkReconciler) applyPrimaryDeployment(
	ctx context.Context,
	desired *appsv1.Deployment,
) (controllerutil.OperationResult, error) {
	current := &appsv1.Deployment{}
	err := r.Get(ctx, clientObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired.DeepCopy()); err != nil {
			return controllerutil.OperationResultNone, err
		}

		return controllerutil.OperationResultCreated, nil
	}
	if err != nil {
		return controllerutil.OperationResultNone, err
	}

	if !equality.Semantic.DeepEqual(current.Spec.Selector, desired.Spec.Selector) {
		return controllerutil.OperationResultNone, unsupportedWorkloadChange(
			"Deployment %s selector drifted from desired value",
			clientObjectKey(desired),
		)
	}

	before := current.DeepCopy()
	current.Labels = desired.Labels
	current.Annotations = desired.Annotations
	current.OwnerReferences = desired.OwnerReferences
	current.Spec.Replicas = desired.Spec.Replicas
	current.Spec.Strategy = desired.Spec.Strategy
	current.Spec.Template = desired.Spec.Template

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, nil
	}
	if err := r.Update(ctx, current); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.OperationResultUpdated, nil
}

func clientObjectKey(object interface {
	GetName() string
	GetNamespace() string
}) types.NamespacedName {
	return types.NamespacedName{
		Namespace: object.GetNamespace(),
		Name:      object.GetName(),
	}
}

func unsupportedStorageChange(format string, args ...any) unsupportedApplyError {
	return unsupportedApplyError{
		reason:  conditionReasonUnsupportedStorageChange,
		message: fmt.Sprintf(format, args...),
	}
}

func unsupportedWorkloadChange(format string, args ...any) unsupportedApplyError {
	return unsupportedApplyError{
		reason:  conditionReasonUnsupportedWorkloadChange,
		message: fmt.Sprintf(format, args...),
	}
}

func unsupportedLocalnetChange(format string, args ...any) unsupportedApplyError {
	return unsupportedApplyError{
		reason:  conditionReasonUnsupportedLocalnetChange,
		message: fmt.Sprintf(format, args...),
	}
}

func missingLocalnetFingerprint(format string, args ...any) unsupportedApplyError {
	return unsupportedApplyError{
		reason:  conditionReasonMissingLocalnetFingerprint,
		message: fmt.Sprintf(format, args...),
	}
}

func validateLocalnetFingerprint(current *corev1.PersistentVolumeClaim, desired *corev1.PersistentVolumeClaim) error {
	currentFingerprint := current.Annotations[localnetFingerprintAnno]
	if currentFingerprint == "" {
		return missingLocalnetFingerprint(
			"PVC %s is missing localnet fingerprint annotation; delete and recreate the CardanoNetwork to recreate localnet state",
			clientObjectKey(desired),
		)
	}

	desiredFingerprint := desired.Annotations[localnetFingerprintAnno]
	if currentFingerprint != desiredFingerprint {
		return unsupportedLocalnetChange(
			"CardanoNetwork localnet inputs changed for PVC %s; delete and recreate the CardanoNetwork to change network parameters",
			clientObjectKey(desired),
		)
	}

	return nil
}

func storageClassCompatible(current *string, desired *string) bool {
	if desired == nil {
		return true
	}
	if current == nil {
		return false
	}

	return *current == *desired
}

func stringPtrValue(value *string) string {
	if value == nil {
		return "<default>"
	}

	return *value
}
