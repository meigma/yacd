package cardanonetwork

import (
	"context"
	"fmt"
	"reflect"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	if err := validateControllerOwner(current, desired); err != nil {
		return controllerutil.OperationResultNone, err
	}

	if err := validateLocalnetFingerprint(current, desired); err != nil {
		return controllerutil.OperationResultNone, err
	}

	if err := validateRequestedStorageClass(current, desired); err != nil {
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

	if err := validateControllerOwner(current, desired); err != nil {
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

func resourceConflict(format string, args ...any) unsupportedApplyError {
	return unsupportedApplyError{
		reason:  conditionReasonResourceConflict,
		message: fmt.Sprintf(format, args...),
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

func validateControllerOwner(current metav1.Object, desired metav1.Object) error {
	desiredController := metav1.GetControllerOf(desired)
	if desiredController == nil {
		return resourceConflict(
			"resource %s has no desired controller owner",
			clientObjectKey(desired),
		)
	}

	currentController := metav1.GetControllerOf(current)
	if currentController == nil {
		return resourceConflict(
			"resource %s already exists without a controller owner",
			clientObjectKey(desired),
		)
	}
	if currentController.APIVersion != desiredController.APIVersion ||
		currentController.Kind != desiredController.Kind ||
		currentController.Name != desiredController.Name ||
		currentController.UID != desiredController.UID {
		return resourceConflict(
			"resource %s is already controlled by %s/%s",
			clientObjectKey(desired),
			currentController.Kind,
			currentController.Name,
		)
	}

	return nil
}

func validateAcceptedLocalnetFingerprint(network *yacdv1alpha1.CardanoNetwork, desiredFingerprint string) error {
	if network.Status.Network == nil || network.Status.Network.LocalnetFingerprint == "" {
		return nil
	}
	if network.Status.Network.LocalnetFingerprint == desiredFingerprint {
		return nil
	}

	return unsupportedLocalnetChange(
		"CardanoNetwork localnet inputs changed from accepted fingerprint; delete and recreate the CardanoNetwork to change network parameters",
	)
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

func validateRequestedStorageClass(current *corev1.PersistentVolumeClaim, desired *corev1.PersistentVolumeClaim) error {
	currentStorageClass, currentHasStorageClassRequest := current.Annotations[requestedStorageClassAnno]
	desiredStorageClass, desiredHasStorageClassRequest := desired.Annotations[requestedStorageClassAnno]
	if currentHasStorageClassRequest == desiredHasStorageClassRequest && currentStorageClass == desiredStorageClass {
		return nil
	}

	return unsupportedStorageChange(
		"PVC %s requested storageClassName cannot be changed from %s to %s",
		clientObjectKey(desired),
		annotationValue(currentStorageClass, currentHasStorageClassRequest),
		annotationValue(desiredStorageClass, desiredHasStorageClassRequest),
	)
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

func annotationValue(value string, ok bool) string {
	if !ok {
		return "<default>"
	}

	return value
}

func stringPtrValue(value *string) string {
	if value == nil {
		return "<default>"
	}

	return *value
}
