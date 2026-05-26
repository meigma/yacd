package cardanodbsync

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlapply "github.com/meigma/yacd/internal/ctrlkit/apply"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	ctrlstorage "github.com/meigma/yacd/internal/ctrlkit/storage"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	dbSyncResourceConflictRequeueAfter = time.Minute
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

type unsupportedApplyError = ctrlapply.UnsupportedError

type dbSyncWorkloadApplyResults struct {
	ConfigMap                     controllerutil.OperationResult
	PGPassSecret                  controllerutil.OperationResult
	PersistentVolumeClaim         controllerutil.OperationResult
	FollowerPersistentVolumeClaim controllerutil.OperationResult
	Deployment                    controllerutil.OperationResult
	MetricsService                controllerutil.OperationResult
}

type managedPostgresApplyResults struct {
	PersistentVolumeClaim controllerutil.OperationResult
	Service               controllerutil.OperationResult
	Deployment            controllerutil.OperationResult
}

func (r dbSyncWorkloadApplyResults) unchanged() bool {
	return r.ConfigMap == controllerutil.OperationResultNone &&
		r.PGPassSecret == controllerutil.OperationResultNone &&
		r.PersistentVolumeClaim == controllerutil.OperationResultNone &&
		r.FollowerPersistentVolumeClaim == controllerutil.OperationResultNone &&
		r.Deployment == controllerutil.OperationResultNone &&
		r.MetricsService == controllerutil.OperationResultNone
}

func (r managedPostgresApplyResults) unchanged() bool {
	return r.PersistentVolumeClaim == controllerutil.OperationResultNone &&
		r.Service == controllerutil.OperationResultNone &&
		r.Deployment == controllerutil.OperationResultNone
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

func (r *CardanoDBSyncReconciler) applyManagedPostgresResources(
	ctx context.Context,
	resources *managedPostgresResources,
) (managedPostgresApplyResults, error) {
	var results managedPostgresApplyResults
	var err error

	results.PersistentVolumeClaim, err = r.applyDBSyncPersistentVolumeClaim(ctx, resources.PersistentVolumeClaim)
	if err != nil {
		return results, err
	}
	results.Service, err = r.applyDBSyncMetricsService(ctx, resources.Service)
	if err != nil {
		return results, err
	}
	results.Deployment, err = r.applyDBSyncDeployment(ctx, resources.Deployment)

	return results, err
}

func (r *CardanoDBSyncReconciler) validateAcceptedDBSyncDatabaseIdentity(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	desiredFingerprint string,
) error {
	if desiredFingerprint == "" {
		return unsupportedSpec("db-sync database identity fingerprint is required")
	}

	acceptedFingerprint := ""
	if dbSync.Status.Database != nil {
		acceptedFingerprint = dbSync.Status.Database.AcceptedIdentityFingerprint
	}
	if acceptedFingerprint == "" {
		var err error
		acceptedFingerprint, err = r.acceptedDBSyncDatabaseIdentityFromPVC(ctx, dbSync)
		if err != nil {
			return err
		}
	}
	if acceptedFingerprint == "" || acceptedFingerprint == desiredFingerprint {
		if acceptedFingerprint != "" &&
			(dbSync.Status.Database == nil || dbSync.Status.Database.AcceptedIdentityFingerprint == "") {
			dbSync.Status.Database = databaseStatus(acceptedFingerprint, dbSyncDatabaseAuthSecretName(dbSync))
		}
		return nil
	}
	if dbSync.Status.Database == nil || dbSync.Status.Database.AcceptedIdentityFingerprint == "" {
		dbSync.Status.Database = databaseStatus(acceptedFingerprint, dbSyncDatabaseAuthSecretName(dbSync))
	}

	return unsupportedDatabaseIdentityChange(
		"CardanoDBSync database-affecting inputs changed from accepted identity; delete and recreate the CardanoDBSync with a fresh or compatible external database",
	)
}

func (r *CardanoDBSyncReconciler) currentAcceptedDBSyncDatabaseIdentity(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) (string, error) {
	if dbSync.Status.Database != nil && dbSync.Status.Database.AcceptedIdentityFingerprint != "" {
		return dbSync.Status.Database.AcceptedIdentityFingerprint, nil
	}

	return r.acceptedDBSyncDatabaseIdentityFromPVC(ctx, dbSync)
}

func dbSyncDatabaseAuthSecretName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	if dbSync.Status.Database == nil {
		return ""
	}

	return dbSync.Status.Database.AuthSecretName
}

func (r *CardanoDBSyncReconciler) acceptedDBSyncDatabaseIdentityFromPVC(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) (string, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncStatePVCName(dbSync)}, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	if !controlledBy(pvc, dbSync) {
		return "", nil
	}

	return pvc.Annotations[dbSyncDatabaseIdentityAnno], nil
}

func (r *CardanoDBSyncReconciler) validateAcceptedManagedPostgresIdentity(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	desiredFingerprint string,
) error {
	if desiredFingerprint == "" {
		return unsupportedSpec("managed Postgres identity fingerprint is required")
	}

	acceptedFingerprint, err := r.acceptedManagedPostgresIdentity(ctx, dbSync)
	if err != nil {
		return err
	}
	if acceptedFingerprint == "" || acceptedFingerprint == desiredFingerprint {
		return nil
	}

	return unsupportedDatabaseIdentityChange(
		"Managed Postgres bootstrap inputs changed from accepted identity; delete and recreate the CardanoDBSync with a fresh database",
	)
}

func (r *CardanoDBSyncReconciler) acceptedManagedPostgresIdentity(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) (string, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: managedPostgresPVCName(dbSync)}, pvc); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", err
		}
	} else if controlledBy(pvc, dbSync) {
		if fingerprint := pvc.Annotations[managedPostgresIdentityAnno]; fingerprint != "" {
			return fingerprint, nil
		}
	}

	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: managedPostgresDeploymentName(dbSync)}, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	if !controlledBy(deployment, dbSync) {
		return "", nil
	}
	if fingerprint := deployment.Annotations[managedPostgresIdentityAnno]; fingerprint != "" {
		return fingerprint, nil
	}

	return deployment.Spec.Template.Annotations[managedPostgresIdentityAnno], nil
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

	if statusErr := r.patchWorkloadApplyBlockedStatus(ctx, dbSync, unsupported.Reason, unsupported.Message); statusErr != nil {
		return ctrl.Result{}, statusErr
	}
	if unsupported.Reason == conditionReasonResourceConflict {
		return ctrl.Result{RequeueAfter: dbSyncResourceConflictRequeueAfter}, nil
	}

	return ctrl.Result{}, nil
}

func (r *CardanoDBSyncReconciler) applyDBSyncConfigMap(
	ctx context.Context,
	desired *corev1.ConfigMap,
) (controllerutil.OperationResult, error) {
	current := &corev1.ConfigMap{}
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
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
	current.Labels = ctrlmetadata.OverlayStringMap(current.Labels, desired.Labels)
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
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
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
	current.Labels = ctrlmetadata.OverlayStringMap(current.Labels, desired.Labels)
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
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
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
	if !ctrlstorage.StorageClassCompatible(current.Spec.StorageClassName, desired.Spec.StorageClassName) {
		return controllerutil.OperationResultNone, unsupportedStorageChange(
			"PVC %s storageClassName cannot be changed from %s to %s",
			ctrlmetadata.ObjectKey(desired),
			ctrlstorage.StringPtrValue(current.Spec.StorageClassName),
			ctrlstorage.StringPtrValue(desired.Spec.StorageClassName),
		)
	}
	if !reflect.DeepEqual(current.Spec.AccessModes, desired.Spec.AccessModes) {
		return controllerutil.OperationResultNone, unsupportedStorageChange(
			"PVC %s accessModes drifted from desired value",
			ctrlmetadata.ObjectKey(desired),
		)
	}

	currentStorage := current.Spec.Resources.Requests[corev1.ResourceStorage]
	desiredStorage := desired.Spec.Resources.Requests[corev1.ResourceStorage]
	if currentStorage.Cmp(desiredStorage) > 0 {
		return controllerutil.OperationResultNone, unsupportedStorageChange(
			"PVC %s storage cannot be decreased from %s to %s",
			ctrlmetadata.ObjectKey(desired),
			currentStorage.String(),
			desiredStorage.String(),
		)
	}

	before := current.DeepCopy()
	current.Labels = ctrlmetadata.OverlayStringMap(current.Labels, desired.Labels)
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
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
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
			ctrlmetadata.ObjectKey(desired),
		)
	}

	before := current.DeepCopy()
	current.Labels = ctrlmetadata.OverlayStringMap(current.Labels, desired.Labels)
	current.Annotations = mergeDBSyncOwnedAnnotations(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences
	current.Spec.Paused = desired.Spec.Paused
	current.Spec.Replicas = desired.Spec.Replicas
	current.Spec.Strategy = desired.Spec.Strategy
	current.Spec.Template.Labels = ctrlmetadata.OverlayStringMap(current.Spec.Template.Labels, desired.Spec.Template.Labels)
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
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
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
	current.Labels = ctrlmetadata.OverlayStringMap(current.Labels, desired.Labels)
	current.Annotations = ctrlmetadata.OverlayStringMap(current.Annotations, desired.Annotations)
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
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
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

func mergeDBSyncOwnedAnnotations(current map[string]string, desired map[string]string) map[string]string {
	return ctrlmetadata.MergeOwnedAnnotations(
		current,
		desired,
		dbSyncPlanFingerprintAnno,
		dbSyncDatabaseIdentityAnno,
		dbSyncSecretVersionAnno,
		dbSyncArtifactDataHashAnno,
		managedPostgresIdentityAnno,
		managedPostgresPasswordFingerprintAnno,
		ctrlstorage.RequestedStorageClassAnnotation,
	)
}

func resourceConflict(format string, args ...any) unsupportedApplyError {
	return ctrlapply.Unsupported(conditionReasonResourceConflict, format, args...)
}

func unsupportedStorageChange(format string, args ...any) unsupportedApplyError {
	return ctrlapply.Unsupported(conditionReasonUnsupportedStorageChange, format, args...)
}

func unsupportedWorkloadChange(format string, args ...any) unsupportedApplyError {
	return ctrlapply.Unsupported(conditionReasonUnsupportedWorkloadChange, format, args...)
}

func unsupportedDatabaseIdentityChange(format string, args ...any) unsupportedApplyError {
	return ctrlapply.Unsupported(conditionReasonUnsupportedDatabaseIdentityChange, format, args...)
}

func validateControllerOwner(current metav1.Object, desired metav1.Object) error {
	if err := ctrlmetadata.ValidateControllerOwner(current, desired); err != nil {
		return resourceConflict("%s", err.Error())
	}

	return nil
}

func controlledBy(current metav1.Object, owner metav1.Object) bool {
	return ctrlmetadata.ControlledBy(current, owner, yacdv1alpha1.GroupVersion.String(), "CardanoDBSync")
}

func validateRequestedStorageClass(current *corev1.PersistentVolumeClaim, desired *corev1.PersistentVolumeClaim) error {
	drift, changed := ctrlstorage.RequestedStorageClassDriftFor(current.Annotations, desired.Annotations)
	if !changed {
		return nil
	}

	return unsupportedStorageChange(
		"PVC %s requested storageClassName cannot be changed from %s to %s",
		ctrlmetadata.ObjectKey(desired),
		drift.CurrentDisplay(),
		drift.DesiredDisplay(),
	)
}
