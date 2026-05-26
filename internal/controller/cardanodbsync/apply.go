package cardanodbsync

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	controllerstorage "github.com/meigma/yacd/internal/controller/storage"
	ctrlapply "github.com/meigma/yacd/internal/ctrlkit/apply"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	ctrlresources "github.com/meigma/yacd/internal/ctrlkit/resources"
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

// dbSyncWorkloadApplyResults captures the per-resource OperationResult
// returned by each apply* call so the reconciler can decide whether the
// run produced cluster mutations (and therefore whether to log at info or
// debug).
type dbSyncWorkloadApplyResults struct {
	ConfigMap                     controllerutil.OperationResult
	PGPassSecret                  controllerutil.OperationResult
	PersistentVolumeClaim         controllerutil.OperationResult
	FollowerPersistentVolumeClaim controllerutil.OperationResult
	Deployment                    controllerutil.OperationResult
	MetricsService                controllerutil.OperationResult
}

// managedPostgresApplyResults captures the per-resource OperationResult for
// the managed Postgres workload bundle.
type managedPostgresApplyResults struct {
	PersistentVolumeClaim controllerutil.OperationResult
	Service               controllerutil.OperationResult
	Deployment            controllerutil.OperationResult
}

// unchanged reports whether every owned child was already in the desired
// state. Used to demote the reconcile log line to debug level when nothing
// actually changed.
func (r dbSyncWorkloadApplyResults) unchanged() bool {
	return r.ConfigMap == controllerutil.OperationResultNone &&
		r.PGPassSecret == controllerutil.OperationResultNone &&
		r.PersistentVolumeClaim == controllerutil.OperationResultNone &&
		r.FollowerPersistentVolumeClaim == controllerutil.OperationResultNone &&
		r.Deployment == controllerutil.OperationResultNone &&
		r.MetricsService == controllerutil.OperationResultNone
}

// unchanged reports whether the managed Postgres bundle was already in the
// desired state.
func (r managedPostgresApplyResults) unchanged() bool {
	return r.PersistentVolumeClaim == controllerutil.OperationResultNone &&
		r.Service == controllerutil.OperationResultNone &&
		r.Deployment == controllerutil.OperationResultNone
}

// applyDBSyncWorkloadResources applies the dbsync workload bundle in
// dependency order: config and pgpass material first (the init container
// consumes pgpass; the containers consume the config), PVCs before the
// Deployment so volumes can mount, and the metrics Service last.
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
	results.MetricsService, err = r.applyDBSyncService(ctx, resources.MetricsService)

	return results, err
}

// applyManagedPostgresResources applies the managed Postgres bundle: PVC,
// Service, and Deployment. The auth Secret is reconciled separately in
// database.go because it has create-once token semantics.
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
	results.Service, err = r.applyDBSyncService(ctx, resources.Service)
	if err != nil {
		return results, err
	}
	results.Deployment, err = r.applyDBSyncDeployment(ctx, resources.Deployment)

	return results, err
}

// validateAcceptedDBSyncDatabaseIdentity rejects a workload apply when the
// dbsync database identity has drifted from the accepted fingerprint. The
// CardanoDBSync must be deleted and recreated to change identity-affecting
// inputs. As a side effect the function lifts the accepted fingerprint
// from the live PVC into in-memory status so the subsequent patch carries
// it forward.
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

// currentAcceptedDBSyncDatabaseIdentity returns the accepted database
// identity fingerprint from CardanoDBSync status or the PVC annotation
// (whichever exists), without consulting the desired fingerprint. Used by
// the intermediate "managed Postgres applied" patch so it can carry the
// accepted identity forward even before the dbsync workload runs.
func (r *CardanoDBSyncReconciler) currentAcceptedDBSyncDatabaseIdentity(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) (string, error) {
	if dbSync.Status.Database != nil && dbSync.Status.Database.AcceptedIdentityFingerprint != "" {
		return dbSync.Status.Database.AcceptedIdentityFingerprint, nil
	}

	return r.acceptedDBSyncDatabaseIdentityFromPVC(ctx, dbSync)
}

// dbSyncDatabaseAuthSecretName returns the previously-stamped auth Secret
// name from CardanoDBSync status. Empty when no managed-Postgres apply has
// ever stamped one.
func dbSyncDatabaseAuthSecretName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	if dbSync.Status.Database == nil {
		return ""
	}

	return dbSync.Status.Database.AuthSecretName
}

// acceptedDBSyncDatabaseIdentityFromPVC reads the accepted identity
// fingerprint annotation from the dbsync state PVC. Returns ("", nil) when
// the PVC is missing or owned by another controller.
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

// validateAcceptedManagedPostgresIdentity rejects a managed-Postgres apply
// when the bootstrap-affecting inputs (image, database name, user,
// password material) have drifted from the accepted identity. The
// CardanoDBSync must be deleted and recreated with a fresh database to
// change those inputs.
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

// acceptedManagedPostgresIdentity returns the accepted managed-Postgres
// identity fingerprint from the live PVC annotation, falling back to the
// Deployment metadata annotation, then the Deployment pod-template
// annotation. Returns ("", nil) when no owned child carries an accepted
// fingerprint yet.
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

// handleDBSyncWorkloadApplyError funnels typed errors from builder
// validation or owned-child apply into a Degraded status patch. Untyped
// errors are returned unchanged so the controller-runtime loop reschedules
// with its default backoff.
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

	var conditionErr statusConditionError
	if !errors.As(err, &conditionErr) {
		return ctrl.Result{}, err
	}
	// conditionErr.Reason crosses the ctrlstatus boundary as a plain string;
	// retype it once and reuse below.
	reason := conditionReason(conditionErr.Reason)
	if statusErr := r.patchWorkloadApplyBlockedStatus(ctx, dbSync, reason, conditionErr.Message); statusErr != nil {
		return ctrl.Result{}, statusErr
	}
	if reason == conditionReasonResourceConflict {
		return ctrl.Result{RequeueAfter: dbSyncResourceConflictRequeueAfter}, nil
	}

	return ctrl.Result{}, nil
}

// applyDBSyncConfigMap applies the dbsync config ConfigMap.
func (r *CardanoDBSyncReconciler) applyDBSyncConfigMap(
	ctx context.Context,
	desired *corev1.ConfigMap,
) (controllerutil.OperationResult, error) {
	result, _, err := ctrlapply.ApplyOwnedObject(ctx, r.Client, desired, ctrlapply.OwnedObjectOptions[*corev1.ConfigMap]{
		Current:       &corev1.ConfigMap{},
		OwnerConflict: controllerOwnerConflict,
		Mutate:        mutateDBSyncConfigMap,
	})
	return result, err
}

// applyDBSyncPGPassSecret applies the dbsync pgpass Secret.
func (r *CardanoDBSyncReconciler) applyDBSyncPGPassSecret(
	ctx context.Context,
	desired *corev1.Secret,
) (controllerutil.OperationResult, error) {
	result, _, err := ctrlapply.ApplyOwnedObject(ctx, r.Client, desired, ctrlapply.OwnedObjectOptions[*corev1.Secret]{
		Current:       &corev1.Secret{},
		OwnerConflict: controllerOwnerConflict,
		Mutate:        mutateDBSyncPGPassSecret,
	})
	return result, err
}

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

// applyDBSyncPersistentVolumeClaim applies a CardanoDBSync-owned PVC. The
// UpdateModeUpdate switch is required because PVCs reject server-side
// patch for spec fields Kubernetes treats as immutable.
func (r *CardanoDBSyncReconciler) applyDBSyncPersistentVolumeClaim(
	ctx context.Context,
	desired *corev1.PersistentVolumeClaim,
) (controllerutil.OperationResult, error) {
	result, _, err := ctrlapply.ApplyOwnedObject(ctx, r.Client, desired, ctrlapply.OwnedObjectOptions[*corev1.PersistentVolumeClaim]{
		Current:       &corev1.PersistentVolumeClaim{},
		OwnerConflict: controllerOwnerConflict,
		Validate:      validateDBSyncPersistentVolumeClaim,
		Mutate:        mutateDBSyncPersistentVolumeClaim,
		UpdateMode:    ctrlapply.UpdateModeUpdate,
	})
	return result, err
}

// applyDBSyncDeployment applies a CardanoDBSync-owned Deployment. The
// Default callback fills in Kubernetes runtime defaults so the diff
// against the live object reflects only intentional drift.
func (r *CardanoDBSyncReconciler) applyDBSyncDeployment(
	ctx context.Context,
	desired *appsv1.Deployment,
) (controllerutil.OperationResult, error) {
	result, _, err := ctrlapply.ApplyOwnedObject(ctx, r.Client, desired, ctrlapply.OwnedObjectOptions[*appsv1.Deployment]{
		Current:       &appsv1.Deployment{},
		Default:       func(desired *appsv1.Deployment) error { return r.defaultObject(desired) },
		OwnerConflict: controllerOwnerConflict,
		Validate:      validateDBSyncDeployment,
		Mutate:        mutateDBSyncDeployment,
	})
	return result, err
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

// applyDBSyncService applies a CardanoDBSync-owned Service. The Default
// callback runs the Scheme defaulting hooks before comparison so
// Kubernetes-assigned fields do not register as drift.
func (r *CardanoDBSyncReconciler) applyDBSyncService(
	ctx context.Context,
	desired *corev1.Service,
) (controllerutil.OperationResult, error) {
	result, _, err := ctrlapply.ApplyOwnedObject(ctx, r.Client, desired, ctrlapply.OwnedObjectOptions[*corev1.Service]{
		Current:       &corev1.Service{},
		Default:       func(desired *corev1.Service) error { return r.defaultObject(desired) },
		OwnerConflict: controllerOwnerConflict,
		Mutate:        mutateDBSyncService,
	})
	return result, err
}

// mutateDBSyncService is the ApplyOwnedObject Mutate callback for a
// CardanoDBSync-owned Service. The ctrlkit helper preserves Kubernetes-
// assigned ClusterIP / ClusterIPs / NodePort / IPFamilies fields.
func mutateDBSyncService(current *corev1.Service, desired *corev1.Service) error {
	ctrlresources.MutateService(current, desired, nil)

	return nil
}

// suspendDBSyncDeploymentIfOwned scales the dbsync workload Deployment to
// zero replicas when it exists and is owned by this CardanoDBSync. Used
// from every status patcher that signals a Degraded or Waiting state so
// the workload does not keep crash-looping while the operator surfaces the
// fix.
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

// defaultObject runs the Scheme defaulting hooks against the desired
// object. Used as the Default callback in ApplyOwnedObject for resources
// whose runtime defaults the reconciler needs filled in before comparison.
func (r *CardanoDBSyncReconciler) defaultObject(object client.Object) error {
	if r.Scheme == nil {
		return fmt.Errorf("scheme is required")
	}
	r.Scheme.Default(object)

	return nil
}

// mergeDBSyncOwnedAnnotations preserves the cardanodbsync-owned annotation
// set from current onto desired and discards any unrelated annotations that
// live on the cluster object but are not owned by this controller.
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
		ctrlannotations.RequestedStorageClass,
	)
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
