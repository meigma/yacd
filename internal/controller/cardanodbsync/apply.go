package cardanodbsync

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	dbSyncResourceConflictRequeueAfter = time.Minute
	requestedStorageClassAnno          = "yacd.meigma.io/requested-storage-class"
)

type unsupportedSpecError struct {
	message string
}

func (e unsupportedSpecError) Error() string {
	return e.message
}

func unsupportedSpec(format string, args ...any) unsupportedSpecError {
	return unsupportedSpecError{message: fmt.Sprintf(format, args...)}
}

type unsupportedApplyError struct {
	reason  string
	message string
}

func (e unsupportedApplyError) Error() string {
	return e.message
}

type dbSyncWorkloadApplyResults struct {
	ConfigMap                     controllerutil.OperationResult
	PGPassSecret                  controllerutil.OperationResult
	PersistentVolumeClaim         controllerutil.OperationResult
	FollowerPersistentVolumeClaim controllerutil.OperationResult
	Deployment                    controllerutil.OperationResult
	MetricsService                controllerutil.OperationResult
}

func (r dbSyncWorkloadApplyResults) unchanged() bool {
	return r.ConfigMap == controllerutil.OperationResultNone &&
		r.PGPassSecret == controllerutil.OperationResultNone &&
		r.PersistentVolumeClaim == controllerutil.OperationResultNone &&
		r.FollowerPersistentVolumeClaim == controllerutil.OperationResultNone &&
		r.Deployment == controllerutil.OperationResultNone &&
		r.MetricsService == controllerutil.OperationResultNone
}

func (r *CardanoDBSyncReconciler) applyDBSyncWorkloadResources(
	ctx context.Context,
	resources *dbSyncWorkloadResources,
) (dbSyncWorkloadApplyResults, error) {
	var results dbSyncWorkloadApplyResults
	var err error

	results.ConfigMap, err = r.applyDBSyncConfigMap(ctx, resources.ConfigMap)
	if err != nil {
		return results, err
	}
	results.PGPassSecret, err = r.applyDBSyncPGPassSecret(ctx, resources.PGPassSecret)
	if err != nil {
		return results, err
	}
	results.PersistentVolumeClaim, err = r.applyDBSyncPersistentVolumeClaim(ctx, resources.PersistentVolumeClaim)
	if err != nil {
		return results, err
	}
	results.FollowerPersistentVolumeClaim, err = r.applyDBSyncPersistentVolumeClaim(ctx, resources.FollowerPersistentVolumeClaim)
	if err != nil {
		return results, err
	}
	results.Deployment, err = r.applyDBSyncDeployment(ctx, resources.Deployment)
	if err != nil {
		return results, err
	}
	results.MetricsService, err = r.applyDBSyncMetricsService(ctx, resources.MetricsService)

	return results, err
}

func (r *CardanoDBSyncReconciler) handleDBSyncWorkloadApplyError(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	err error,
) (ctrl.Result, error) {
	var unsupportedSpec unsupportedSpecError
	if errors.As(err, &unsupportedSpec) {
		return ctrl.Result{}, r.patchWorkloadApplyBlockedStatus(ctx, dbSync,
			conditionReasonUnsupportedSpec,
			unsupportedSpec.Error(),
		)
	}

	var unsupported unsupportedApplyError
	if !errors.As(err, &unsupported) {
		return ctrl.Result{}, err
	}

	if statusErr := r.patchWorkloadApplyBlockedStatus(ctx, dbSync, unsupported.reason, unsupported.message); statusErr != nil {
		return ctrl.Result{}, statusErr
	}
	if unsupported.reason == conditionReasonResourceConflict {
		return ctrl.Result{RequeueAfter: dbSyncResourceConflictRequeueAfter}, nil
	}

	return ctrl.Result{}, nil
}

func (r *CardanoDBSyncReconciler) applyDBSyncConfigMap(
	ctx context.Context,
	desired *corev1.ConfigMap,
) (controllerutil.OperationResult, error) {
	current := &corev1.ConfigMap{}
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

	before := current.DeepCopy()
	current.Labels = mergeStringMap(current.Labels, desired.Labels)
	current.Annotations = mergeDBSyncOwnedAnnotations(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences
	current.Data = maps.Clone(desired.Data)
	current.BinaryData = maps.Clone(desired.BinaryData)

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, nil
	}
	if err := r.Patch(ctx, current, client.MergeFrom(before)); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.OperationResultUpdated, nil
}

func (r *CardanoDBSyncReconciler) applyDBSyncPGPassSecret(
	ctx context.Context,
	desired *corev1.Secret,
) (controllerutil.OperationResult, error) {
	current := &corev1.Secret{}
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

	before := current.DeepCopy()
	current.Labels = mergeStringMap(current.Labels, desired.Labels)
	current.Annotations = mergeDBSyncOwnedAnnotations(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences
	current.Type = corev1.SecretTypeOpaque
	current.Data = maps.Clone(desired.Data)
	current.StringData = nil

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, nil
	}
	if err := r.Patch(ctx, current, client.MergeFrom(before)); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.OperationResultUpdated, nil
}

func (r *CardanoDBSyncReconciler) applyDBSyncPersistentVolumeClaim(
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
	current.Labels = mergeStringMap(current.Labels, desired.Labels)
	current.Annotations = mergeDBSyncOwnedAnnotations(current.Annotations, desired.Annotations)
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

func (r *CardanoDBSyncReconciler) applyDBSyncDeployment(
	ctx context.Context,
	desired *appsv1.Deployment,
) (controllerutil.OperationResult, error) {
	desired = desired.DeepCopy()
	if err := r.defaultObject(desired); err != nil {
		return controllerutil.OperationResultNone, err
	}

	current := &appsv1.Deployment{}
	err := r.Get(ctx, clientObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
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
	current.Labels = mergeStringMap(current.Labels, desired.Labels)
	current.Annotations = mergeDBSyncOwnedAnnotations(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences
	current.Spec.Paused = desired.Spec.Paused
	current.Spec.Replicas = desired.Spec.Replicas
	current.Spec.Strategy = desired.Spec.Strategy
	current.Spec.Template.Labels = mergeStringMap(current.Spec.Template.Labels, desired.Spec.Template.Labels)
	current.Spec.Template.Annotations = mergeDBSyncOwnedAnnotations(current.Spec.Template.Annotations, desired.Spec.Template.Annotations)
	current.Spec.Template.Spec.AutomountServiceAccountToken = desired.Spec.Template.Spec.AutomountServiceAccountToken
	current.Spec.Template.Spec.SecurityContext = desired.Spec.Template.Spec.SecurityContext
	current.Spec.Template.Spec.Containers = desired.Spec.Template.Spec.Containers
	current.Spec.Template.Spec.Volumes = desired.Spec.Template.Spec.Volumes

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, nil
	}
	if err := r.Patch(ctx, current, client.MergeFrom(before)); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.OperationResultUpdated, nil
}

func (r *CardanoDBSyncReconciler) applyDBSyncMetricsService(
	ctx context.Context,
	desired *corev1.Service,
) (controllerutil.OperationResult, error) {
	desired = desired.DeepCopy()
	if err := r.defaultObject(desired); err != nil {
		return controllerutil.OperationResultNone, err
	}

	current := &corev1.Service{}
	err := r.Get(ctx, clientObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
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

	before := current.DeepCopy()
	current.Labels = mergeStringMap(current.Labels, desired.Labels)
	current.Annotations = mergeStringMap(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences
	current.Spec.Type = desired.Spec.Type
	current.Spec.Selector = maps.Clone(desired.Spec.Selector)
	current.Spec.Ports = desired.Spec.Ports
	current.Spec.ExternalName = desired.Spec.ExternalName

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, nil
	}
	if err := r.Patch(ctx, current, client.MergeFrom(before)); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.OperationResultUpdated, nil
}

func (r *CardanoDBSyncReconciler) suspendDBSyncDeploymentIfOwned(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) error {
	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dbSyncWorkloadName(dbSync),
			Namespace: dbSync.Namespace,
		},
	}
	if err := controllerutil.SetControllerReference(dbSync, desired, r.Scheme); err != nil {
		return fmt.Errorf("set desired db-sync Deployment owner reference: %w", err)
	}

	current := &appsv1.Deployment{}
	err := r.Get(ctx, clientObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if validateControllerOwner(current, desired) != nil {
		return nil
	}
	if current.Spec.Replicas != nil && *current.Spec.Replicas == 0 {
		return nil
	}

	before := current.DeepCopy()
	current.Spec.Replicas = new(int32)
	return r.Patch(ctx, current, client.MergeFrom(before))
}

func (r *CardanoDBSyncReconciler) defaultObject(object client.Object) error {
	if r.Scheme == nil {
		return fmt.Errorf("scheme is required")
	}
	r.Scheme.Default(object)

	return nil
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

func mergeStringMap(current map[string]string, desired map[string]string) map[string]string {
	merged := map[string]string{}
	maps.Copy(merged, current)
	maps.Copy(merged, desired)
	if len(merged) == 0 {
		return nil
	}

	return merged
}

func mergeDBSyncOwnedAnnotations(current map[string]string, desired map[string]string) map[string]string {
	merged := map[string]string{}
	maps.Copy(merged, current)
	for _, key := range []string{
		dbSyncPlanFingerprintAnno,
		dbSyncSecretVersionAnno,
		dbSyncArtifactDataHashAnno,
		requestedStorageClassAnno,
	} {
		if value, ok := desired[key]; ok {
			merged[key] = value
			continue
		}
		delete(merged, key)
	}
	if len(merged) == 0 {
		return nil
	}

	return merged
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
