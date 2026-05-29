package cardanodbsync

import (
	"context"
	"errors"
	"fmt"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	controllerstorage "github.com/meigma/yacd/internal/controller/storage"
	ctrlapply "github.com/meigma/yacd/internal/ctrlkit/apply"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

// primarySidecarDBSyncApplyResults captures CardanoDBSync-owned resource
// applies for primarySidecar placement.
type primarySidecarDBSyncApplyResults struct {
	ConfigMap             controllerutil.OperationResult
	PGPassSecret          controllerutil.OperationResult
	PersistentVolumeClaim controllerutil.OperationResult
	MetricsService        controllerutil.OperationResult
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

// unchanged reports whether the primarySidecar material bundle was already in
// the desired state.
func (r primarySidecarDBSyncApplyResults) unchanged() bool {
	return r.ConfigMap == controllerutil.OperationResultNone &&
		r.PGPassSecret == controllerutil.OperationResultNone &&
		r.PersistentVolumeClaim == controllerutil.OperationResultNone &&
		r.MetricsService == controllerutil.OperationResultNone
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

// applyPrimarySidecarDBSyncResources applies the CardanoDBSync-owned material
// that the CardanoNetwork primary Pod will mount.
func (r *CardanoDBSyncReconciler) applyPrimarySidecarDBSyncResources(
	ctx context.Context,
	resources *primarySidecarDBSyncResources,
) (primarySidecarDBSyncApplyResults, error) {
	var results primarySidecarDBSyncApplyResults
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
// dbsync database identity has drifted from the fingerprint accepted on owned
// runtime state. The db-sync state PVC is authoritative; status only mirrors
// the PVC annotation for humans and clients.
func (r *CardanoDBSyncReconciler) validateAcceptedDBSyncDatabaseIdentity(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	desiredFingerprint string,
) error {
	if desiredFingerprint == "" {
		return unsupportedSpec("db-sync database identity fingerprint is required")
	}

	acceptedFingerprint, err := r.currentAcceptedDBSyncDatabaseIdentity(ctx, dbSync)
	if err != nil {
		return err
	}
	if acceptedFingerprint == "" || acceptedFingerprint == desiredFingerprint {
		if acceptedFingerprint != "" &&
			(dbSync.Status.Database == nil || dbSync.Status.Database.AcceptedIdentityFingerprint != acceptedFingerprint) {
			_, authSecretName, acceptedPlacementMode := databaseStatusValues(dbSync.Status.Database)
			dbSync.Status.Database = databaseStatus(acceptedFingerprint, authSecretName, acceptedPlacementMode)
		}
		return nil
	}
	if dbSync.Status.Database == nil || dbSync.Status.Database.AcceptedIdentityFingerprint != acceptedFingerprint {
		_, authSecretName, acceptedPlacementMode := databaseStatusValues(dbSync.Status.Database)
		dbSync.Status.Database = databaseStatus(acceptedFingerprint, authSecretName, acceptedPlacementMode)
	}

	return unsupportedDatabaseIdentityChange(
		"CardanoDBSync database-affecting inputs changed from accepted identity %q to desired identity %q; accepted identity is stored on PVC %q annotation %q. Restore the previous database-affecting spec fields, or recreate the CardanoDBSync with a fresh or compatible database if the change is intentional",
		acceptedFingerprint,
		desiredFingerprint,
		dbSyncStatePVCName(dbSync),
		dbSyncDatabaseIdentityAnno,
	)
}

// validateAcceptedDBSyncPlacementMode rejects a workload apply when dbsync has
// already accepted state in a different placement mode. Placement controls the
// node socket source, so moving an initialized database between primarySidecar
// and dedicatedFollower is not safe without an explicit recreate or migration.
func (r *CardanoDBSyncReconciler) validateAcceptedDBSyncPlacementMode(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
	desiredMode yacdv1alpha1.CardanoDBSyncPlacementMode,
) error {
	if desiredMode == "" {
		return unsupportedSpec("db-sync placement mode is required")
	}

	acceptedMode, err := r.currentAcceptedDBSyncPlacementMode(ctx, dbSync, network)
	if err != nil {
		return err
	}
	if acceptedMode == "" || acceptedMode == desiredMode {
		if acceptedMode != "" &&
			(dbSync.Status.Database == nil || dbSync.Status.Database.AcceptedPlacementMode == "") {
			acceptedIdentityFingerprint, authSecretName, _ := databaseStatusValues(dbSync.Status.Database)
			dbSync.Status.Database = databaseStatus(acceptedIdentityFingerprint, authSecretName, acceptedMode)
		}
		return nil
	}
	if dbSync.Status.Database == nil || dbSync.Status.Database.AcceptedPlacementMode == "" {
		acceptedIdentityFingerprint, authSecretName, _ := databaseStatusValues(dbSync.Status.Database)
		dbSync.Status.Database = databaseStatus(acceptedIdentityFingerprint, authSecretName, acceptedMode)
	}

	return unsupportedDatabaseIdentityChange(
		"CardanoDBSync placement changed from accepted placement %q to %q; delete and recreate the CardanoDBSync with a fresh or compatible database",
		acceptedMode,
		desiredMode,
	)
}

// currentAcceptedDBSyncDatabaseIdentity returns the database identity accepted
// on owned runtime material, without consulting status or the desired
// fingerprint. Status mirrors this value, but direct status edits are not part
// of the acceptance decision.
func (r *CardanoDBSyncReconciler) currentAcceptedDBSyncDatabaseIdentity(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) (string, error) {
	return r.acceptedDBSyncDatabaseIdentityFromPVC(ctx, dbSync)
}

// currentAcceptedDBSyncPlacementMode returns the placement mode already
// accepted by this CardanoDBSync. It prefers explicit new status/annotation
// state, then falls back to legacy workload-shape inference only when a
// database identity has already been accepted.
func (r *CardanoDBSyncReconciler) currentAcceptedDBSyncPlacementMode(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
) (yacdv1alpha1.CardanoDBSyncPlacementMode, error) {
	if dbSync.Status.Database != nil && dbSync.Status.Database.AcceptedPlacementMode != "" {
		return dbSync.Status.Database.AcceptedPlacementMode, nil
	}

	pvcMode, pvcAcceptedIdentity, err := r.acceptedDBSyncPlacementModeFromPVC(ctx, dbSync)
	if err != nil {
		return "", err
	}
	if pvcMode != "" {
		return pvcMode, nil
	}
	if dbSync.Status.Database == nil &&
		!pvcAcceptedIdentity {
		return "", nil
	}
	if dbSync.Status.Database != nil &&
		dbSync.Status.Database.AcceptedIdentityFingerprint == "" &&
		!pvcAcceptedIdentity {
		return "", nil
	}

	return r.inferAcceptedDBSyncPlacementMode(ctx, dbSync, network)
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

// acceptedDBSyncPlacementModeFromPVC reads the accepted placement annotation
// from the dbsync state PVC. The boolean reports whether the same PVC carries
// an accepted database identity, which lets legacy resources infer placement
// without locking pre-acceptance resources.
func (r *CardanoDBSyncReconciler) acceptedDBSyncPlacementModeFromPVC(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) (yacdv1alpha1.CardanoDBSyncPlacementMode, bool, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncStatePVCName(dbSync)}, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if !controlledBy(pvc, dbSync) {
		return "", false, nil
	}

	return yacdv1alpha1.CardanoDBSyncPlacementMode(pvc.Annotations[dbSyncPlacementModeAnno]),
		pvc.Annotations[dbSyncDatabaseIdentityAnno] != "",
		nil
}

func (r *CardanoDBSyncReconciler) inferAcceptedDBSyncPlacementMode(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
) (yacdv1alpha1.CardanoDBSyncPlacementMode, error) {
	primarySidecarPresent, err := r.primarySidecarDBSyncPresent(ctx, dbSync, network)
	if err != nil {
		return "", err
	}
	if primarySidecarPresent {
		return yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar, nil
	}

	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, deployment); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", err
		}
	} else if controlledBy(deployment, dbSync) {
		return yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower, nil
	}

	if dbSync.Status.Placement != nil && dbSync.Status.Placement.Mode != "" {
		return dbSync.Status.Placement.Mode, nil
	}

	return "", nil
}

func (r *CardanoDBSyncReconciler) primarySidecarDBSyncPresent(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
) (bool, error) {
	if network == nil {
		return false, nil
	}
	expectedDBSyncLabel := primarySidecarPodSelectorLabels(dbSync)[labelDBSync]
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryNetworkDeploymentName(network)}, deployment); err != nil {
		if !apierrors.IsNotFound(err) {
			return false, err
		}
	} else if deployment.Spec.Template.Labels[labelDBSync] == expectedDBSyncLabel &&
		podSpecHasDBSyncContainer(deployment.Spec.Template.Spec) {
		return true, nil
	}

	pods := &corev1.PodList{}
	if err := r.liveReader().List(
		ctx,
		pods,
		client.InNamespace(network.Namespace),
		client.MatchingLabels(primaryNetworkSelectorLabels(network)),
	); err != nil {
		return false, err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Labels[labelDBSync] == expectedDBSyncLabel &&
			!podTerminal(pod) &&
			podSpecHasDBSyncContainer(pod.Spec) {
			return true, nil
		}
	}

	return false, nil
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
		UpdateError: func(current *corev1.PersistentVolumeClaim, desired *corev1.PersistentVolumeClaim, err error) error {
			return controllerstorage.PersistentVolumeClaimUpdateError(string(conditionReasonStorageExpansionRejected), current, desired, err)
		},
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
