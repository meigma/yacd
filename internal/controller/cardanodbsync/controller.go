// Package cardanodbsync contains the CardanoDBSync controller.
package cardanodbsync

import (
	"context"
	"strings"
	"time"

	"github.com/go-logr/logr"
	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlnetworkartifacts "github.com/meigma/yacd/internal/controller/networkartifacts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlbuilder "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// controllerName is the controller-runtime name used for logs, metrics,
	// and controller registration.
	controllerName = "cardanodbsync"

	cardanoDBSyncNetworkRefNameField             = "spec.networkRef.name"
	cardanoDBSyncExternalDatabaseSecretNameField = "spec.database.external.passwordSecretRef.name"
	cardanoDBSyncManagedDatabaseSecretNameField  = "spec.database.managed.authSecretRef.name"

	dbSyncWorkloadReadinessRequeueAfter = 15 * time.Second
	dbSyncRuntimeProbeRequeueAfter      = 30 * time.Second
)

// CardanoDBSyncReconciler reconciles CardanoDBSync resources.
type CardanoDBSyncReconciler struct {
	// Client is the controller-runtime client used to read and write
	// CardanoDBSync resources and cached dependencies.
	client.Client

	// Reader is the uncached reader used for live dependency checks.
	Reader client.Reader

	// Scheme is the runtime scheme used when setting controller references on
	// owned child resources.
	Scheme *runtime.Scheme

	// DefaultCardanoTestnetImage overrides the follower-node container
	// image. Empty leaves the built-in
	// "<repo>:<networkNodeVersion>-<revision>" formula in place.
	DefaultCardanoTestnetImage string

	// DefaultCardanoToolsImage overrides the cardano-tools container image
	// used for artifact staging. Empty leaves the built-in
	// "<repo>:<networkNodeVersion>-<revision>" formula in place.
	DefaultCardanoToolsImage string

	// runtimeProberOverride lets tests avoid requiring real Postgres/Ogmios.
	runtimeProberOverride runtimeProber
}

// +kubebuilder:rbac:groups=yacd.meigma.io,resources=cardanodbsyncs,verbs=get;list;watch
// +kubebuilder:rbac:groups=yacd.meigma.io,resources=cardanodbsyncs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=yacd.meigma.io,resources=cardanonetworks,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list

// Reconcile validates CardanoDBSync dependencies, applies database and db-sync
// workload resources, and publishes workload-level runtime status.
func (r *CardanoDBSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx, "cardanodbsync", req.String())

	dbSync := &yacdv1alpha1.CardanoDBSync{}
	if err := r.Get(ctx, req.NamespacedName, dbSync); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("CardanoDBSync not found; ignoring deleted object")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !dbSync.DeletionTimestamp.IsZero() {
		log.V(1).Info("CardanoDBSync is deleting; skipping reconcile")
		return ctrl.Result{}, nil
	}

	placementReady, err := r.reconcilePlacement(ctx, dbSync)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !placementReady {
		return ctrl.Result{}, nil
	}

	if effectivePlacementMode(dbSync) == yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar {
		ok, err := r.preflightPrimarySidecarNetwork(ctx, dbSync)
		if err != nil || !ok {
			return ctrl.Result{}, err
		}
	}

	databaseRuntime, ok, err := r.resolveDatabase(ctx, dbSync)
	if err != nil {
		return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
	}
	if !ok {
		return ctrl.Result{}, nil
	}

	network := &yacdv1alpha1.CardanoNetwork{}
	networkKey := client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSync.Spec.NetworkRef.Name}
	if err := r.Get(ctx, networkKey, network); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, r.patchDependencyUnavailableStatus(ctx, dbSync,
				conditionReasonNetworkUnavailable,
				"Referenced CardanoNetwork does not exist",
			)
		}
		return ctrl.Result{}, err
	}
	if !network.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.patchDependencyUnavailableStatus(ctx, dbSync,
			conditionReasonNetworkUnavailable,
			"Referenced CardanoNetwork is deleting",
		)
	}

	if network.Status.ObservedGeneration < network.Generation {
		return ctrl.Result{}, r.patchDependencyWaitingStatus(ctx, dbSync,
			conditionReasonNetworkStatusStale,
			"Referenced CardanoNetwork status has not observed the latest generation",
		)
	}
	if !networkArtifactsReady(network) {
		return ctrl.Result{}, r.patchDependencyWaitingStatus(ctx, dbSync,
			conditionReasonNetworkArtifactsPending,
			"Referenced CardanoNetwork has not published fresh verified artifacts",
		)
	}
	artifactStatus := ctrlnetworkartifacts.ConsumerStatus(network.Status.Artifacts)
	if !artifactStatus.Ready {
		return ctrl.Result{}, r.patchDependencyWaitingStatus(ctx, dbSync,
			conditionReasonNetworkArtifactsPending,
			artifactStatus.Message,
		)
	}

	configMap := &corev1.ConfigMap{}
	configMapKey := client.ObjectKey{Namespace: dbSync.Namespace, Name: artifactStatus.ConfigMapName}
	if err := r.liveReader().Get(ctx, configMapKey, configMap); err != nil {
		if apierrors.IsNotFound(err) {
			result := ctrlnetworkartifacts.ConsumerConfigMap(nil, *network.Status.Artifacts)
			return ctrl.Result{}, r.patchDependencyWaitingStatus(ctx, dbSync,
				conditionReasonNetworkArtifactsPending,
				result.Message,
			)
		}
		return ctrl.Result{}, err
	}

	configMapResult := ctrlnetworkartifacts.ConsumerConfigMap(configMap, *network.Status.Artifacts)
	if configMapResult.Ready {
		return r.reconcileReadyDBSync(ctx, log, dbSync, network, configMap, databaseRuntime)
	}
	if configMapResult.Pending {
		return ctrl.Result{}, r.patchDependencyWaitingStatus(ctx, dbSync,
			conditionReasonNetworkArtifactsPending,
			configMapResult.Message,
		)
	}
	return ctrl.Result{}, r.patchDependencyWaitingStatus(ctx, dbSync,
		conditionReasonNetworkArtifactsMismatch,
		configMapResult.Message,
	)
}

// preflightPrimarySidecarNetwork validates primarySidecar-only CardanoNetwork
// constraints before database or DB Sync-owned material is applied.
func (r *CardanoDBSyncReconciler) preflightPrimarySidecarNetwork(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) (bool, error) {
	network := &yacdv1alpha1.CardanoNetwork{}
	networkKey := client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSync.Spec.NetworkRef.Name}
	if err := r.Get(ctx, networkKey, network); err != nil {
		if apierrors.IsNotFound(err) {
			return false, r.patchDependencyUnavailableStatus(ctx, dbSync,
				conditionReasonNetworkUnavailable,
				"Referenced CardanoNetwork does not exist",
			)
		}
		return false, err
	}
	if !network.DeletionTimestamp.IsZero() {
		return false, r.patchDependencyUnavailableStatus(ctx, dbSync,
			conditionReasonNetworkUnavailable,
			"Referenced CardanoNetwork is deleting",
		)
	}
	if err := ValidatePrimarySidecarNetwork(dbSync, network); err != nil {
		return false, r.patchWorkloadApplyBlockedStatus(ctx, dbSync, conditionReasonUnsupportedSpec, err.Error())
	}

	return true, nil
}

// reconcileReadyDBSync is the workload-apply leg of Reconcile, entered
// only after the referenced CardanoNetwork has published verified
// artifacts. It rejects requests that lack a node-to-node endpoint
// (without that the follower-node cannot peer) before handing off to
// reconcileWorkloads.
func (r *CardanoDBSyncReconciler) reconcileReadyDBSync(
	ctx context.Context,
	log logr.Logger,
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
	configMap *corev1.ConfigMap,
	databaseRuntime databaseRuntime,
) (ctrl.Result, error) {
	if network.Status.Endpoints == nil ||
		network.Status.Endpoints.NodeToNode == nil ||
		network.Status.Endpoints.NodeToNode.ServiceName == "" ||
		network.Status.Endpoints.NodeToNode.Port == 0 ||
		network.Status.Endpoints.NodeToNode.URL == "" {
		return ctrl.Result{}, r.patchDependencyWaitingStatus(ctx, dbSync,
			conditionReasonNodeToNodeEndpointMissing,
			"Referenced CardanoNetwork has not published a node-to-node endpoint",
		)
	}

	connectionResult := ctrlnetworkartifacts.ConsumerConnection(configMap, network)
	if !connectionResult.Ready {
		return ctrl.Result{}, r.patchDependencyWaitingStatus(ctx, dbSync,
			conditionReasonNetworkArtifactsMismatch,
			connectionResult.Message,
		)
	}
	if err := validatePublicDBSyncSupport(dbSync, connectionResult.Connection); err != nil {
		return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
	}

	return r.reconcileWorkloads(ctx, log, dbSync, network, configMap, databaseRuntime, connectionResult.Connection)
}

// reconcileWorkloads applies (in dependency order) the managed Postgres
// workload when spec.database.managed is set, the dbsync workload, and
// the matching status patches. The dbsync workload apply is gated on
// managed Postgres becoming ready first so the dbsync containers do not
// crash-loop against an unavailable database.
func (r *CardanoDBSyncReconciler) reconcileWorkloads(
	ctx context.Context,
	log logr.Logger,
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
	configMap *corev1.ConfigMap,
	databaseRuntime databaseRuntime,
	networkConnection ctrlnetworkartifacts.Connection,
) (ctrl.Result, error) {
	builder := dbSyncWorkloadBuilder{
		scheme:                     r.Scheme,
		defaultCardanoTestnetImage: r.DefaultCardanoTestnetImage,
		networkConnection:          &networkConnection,
	}
	var postgresResources *managedPostgresResources
	if databaseRuntime.Mode == databaseModeManaged {
		var err error
		postgresResources, err = builder.managedPostgresResources(dbSync, databaseRuntime.workloadPasswordSecret())
		if err != nil {
			return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
		}
		if err := r.validateAcceptedManagedPostgresIdentity(ctx, dbSync, postgresResources.IdentityFingerprint); err != nil {
			return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
		}
	}

	if effectivePlacementMode(dbSync) == yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar {
		return r.reconcilePrimarySidecarWorkloads(ctx, log, dbSync, network, configMap, databaseRuntime, builder, postgresResources)
	}

	resources, err := builder.BuildForDatabase(dbSync, network, configMap, databaseRuntime.workloadPasswordSecret(), databaseRuntime.Database)
	if err != nil {
		return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
	}
	if err := r.validateAcceptedDBSyncDatabaseIdentity(ctx, dbSync, resources.Plan.DatabaseIdentityFingerprint.Value); err != nil {
		return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
	}
	if err := r.validateAcceptedDBSyncPlacementMode(ctx, dbSync, network, yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower); err != nil {
		return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
	}

	sidecarGone, err := r.primarySidecarDBSyncGone(ctx, network)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !sidecarGone {
		if err := r.patchPlacementTransitionWaitingStatus(ctx, dbSync, conditionMessageWaitingForPrimarySidecar); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: dbSyncWorkloadReadinessRequeueAfter}, nil
	}
	if databaseRuntime.Mode == databaseModeManaged {
		ready, err := r.reconcileManagedPostgresResources(ctx, log, dbSync, postgresResources)
		if err != nil {
			return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
		}
		if ready.Status != metav1.ConditionTrue {
			acceptedIdentity, err := r.currentAcceptedDBSyncDatabaseIdentity(ctx, dbSync)
			if err != nil {
				return ctrl.Result{}, err
			}
			if err := r.patchManagedPostgresAppliedStatus(ctx, dbSync, databaseRuntime, ready, acceptedIdentity); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: dbSyncWorkloadReadinessRequeueAfter}, nil
		}
	}

	applyResults, err := r.applyDBSyncWorkloadResources(ctx, resources)
	if err != nil {
		return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
	}

	resultLog := log
	if applyResults.unchanged() {
		resultLog = log.V(1)
	}
	resultLog.Info("Applied CardanoDBSync workloads",
		"configMap", client.ObjectKeyFromObject(resources.ConfigMap),
		"configMapOperation", applyResults.ConfigMap,
		"pgPassSecret", client.ObjectKeyFromObject(resources.PGPassSecret),
		"pgPassSecretOperation", applyResults.PGPassSecret,
		"persistentVolumeClaim", client.ObjectKeyFromObject(resources.PersistentVolumeClaim),
		"persistentVolumeClaimOperation", applyResults.PersistentVolumeClaim,
		"followerPersistentVolumeClaim", client.ObjectKeyFromObject(resources.FollowerPersistentVolumeClaim),
		"followerPersistentVolumeClaimOperation", applyResults.FollowerPersistentVolumeClaim,
		"deployment", client.ObjectKeyFromObject(resources.Deployment),
		"deploymentOperation", applyResults.Deployment,
		"metricsService", client.ObjectKeyFromObject(resources.MetricsService),
		"metricsServiceOperation", applyResults.MetricsService,
		"planFingerprint", resources.Plan.Fingerprint.Value)

	ready, probed, err := r.patchWorkloadsAppliedStatus(ctx, dbSync, network, resources.MetricsService, databaseRuntime, resources.Plan.DatabaseIdentityFingerprint.Value)
	if err != nil {
		return ctrl.Result{}, err
	}
	if probed {
		return ctrl.Result{RequeueAfter: dbSyncRuntimeProbeRequeueAfter}, nil
	}
	if ready.Status != metav1.ConditionTrue && ready.Reason == string(conditionReasonDeploymentProgressing) {
		return ctrl.Result{RequeueAfter: dbSyncWorkloadReadinessRequeueAfter}, nil
	}

	return ctrl.Result{}, nil
}

// reconcilePrimarySidecarWorkloads applies the DB Sync-owned material for
// primarySidecar placement after suspending any owned dedicated Deployment and
// waiting for its Pods to terminate.
func (r *CardanoDBSyncReconciler) reconcilePrimarySidecarWorkloads(
	ctx context.Context,
	log logr.Logger,
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
	configMap *corev1.ConfigMap,
	databaseRuntime databaseRuntime,
	builder dbSyncWorkloadBuilder,
	postgresResources *managedPostgresResources,
) (ctrl.Result, error) {
	resources, err := builder.BuildPrimarySidecarForDatabase(dbSync, network, configMap, databaseRuntime.workloadPasswordSecret(), databaseRuntime.Database)
	if err != nil {
		return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
	}
	if err := r.validateAcceptedDBSyncDatabaseIdentity(ctx, dbSync, resources.Plan.DatabaseIdentityFingerprint.Value); err != nil {
		return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
	}
	if err := r.validateAcceptedDBSyncPlacementMode(ctx, dbSync, network, yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar); err != nil {
		return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
	}
	if err := r.suspendDBSyncDeploymentIfOwned(ctx, dbSync); err != nil {
		return ctrl.Result{}, err
	}
	dedicatedPodsGone, err := r.dedicatedDBSyncPodsGone(ctx, dbSync)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !dedicatedPodsGone {
		if err := r.patchPlacementTransitionWaitingStatus(ctx, dbSync, conditionMessageWaitingForDedicatedPods); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: dbSyncWorkloadReadinessRequeueAfter}, nil
	}
	if databaseRuntime.Mode == databaseModeManaged {
		ready, err := r.reconcileManagedPostgresResources(ctx, log, dbSync, postgresResources)
		if err != nil {
			return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
		}
		if ready.Status != metav1.ConditionTrue {
			acceptedIdentity, err := r.currentAcceptedDBSyncDatabaseIdentity(ctx, dbSync)
			if err != nil {
				return ctrl.Result{}, err
			}
			if err := r.patchPrimarySidecarManagedPostgresAppliedStatus(ctx, dbSync, databaseRuntime, ready, acceptedIdentity); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: dbSyncWorkloadReadinessRequeueAfter}, nil
		}
	}

	applyResults, err := r.applyPrimarySidecarDBSyncResources(ctx, resources)
	if err != nil {
		return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
	}

	resultLog := log
	if applyResults.unchanged() {
		resultLog = log.V(1)
	}
	resultLog.Info("Applied CardanoDBSync primary-sidecar resources",
		"configMap", client.ObjectKeyFromObject(resources.ConfigMap),
		"configMapOperation", applyResults.ConfigMap,
		"pgPassSecret", client.ObjectKeyFromObject(resources.PGPassSecret),
		"pgPassSecretOperation", applyResults.PGPassSecret,
		"persistentVolumeClaim", client.ObjectKeyFromObject(resources.PersistentVolumeClaim),
		"persistentVolumeClaimOperation", applyResults.PersistentVolumeClaim,
		"metricsService", client.ObjectKeyFromObject(resources.MetricsService),
		"metricsServiceOperation", applyResults.MetricsService,
		"planFingerprint", resources.Plan.Fingerprint.Value)

	ready, probed, err := r.patchPrimarySidecarResourcesAppliedStatus(ctx, dbSync, network, resources, databaseRuntime, resources.Plan.DatabaseIdentityFingerprint.Value)
	if err != nil {
		return ctrl.Result{}, err
	}
	if probed {
		return ctrl.Result{RequeueAfter: dbSyncRuntimeProbeRequeueAfter}, nil
	}
	if ready.Status != metav1.ConditionTrue && ready.Reason == string(conditionReasonDeploymentProgressing) {
		return ctrl.Result{RequeueAfter: dbSyncWorkloadReadinessRequeueAfter}, nil
	}

	return ctrl.Result{}, nil
}

// reconcileManagedPostgresResources applies the managed Postgres bundle
// and returns the resulting PostgresReady condition. The caller uses the
// condition to decide whether to proceed with the dbsync workload apply
// (Ready=True) or wait for Postgres to come up (Ready=False).
func (r *CardanoDBSyncReconciler) reconcileManagedPostgresResources(
	ctx context.Context,
	log logr.Logger,
	dbSync *yacdv1alpha1.CardanoDBSync,
	resources *managedPostgresResources,
) (metav1.Condition, error) {
	applyResults, err := r.applyManagedPostgresResources(ctx, resources)
	if err != nil {
		return metav1.Condition{}, err
	}

	resultLog := log
	if applyResults.unchanged() {
		resultLog = log.V(1)
	}
	resultLog.Info("Applied managed Postgres resources",
		"persistentVolumeClaim", client.ObjectKeyFromObject(resources.PersistentVolumeClaim),
		"persistentVolumeClaimOperation", applyResults.PersistentVolumeClaim,
		"service", client.ObjectKeyFromObject(resources.Service),
		"serviceOperation", applyResults.Service,
		"deployment", client.ObjectKeyFromObject(resources.Deployment),
		"deploymentOperation", applyResults.Deployment)

	return r.managedPostgresReadyCondition(ctx, dbSync)
}

// SetupWithManager sets up the CardanoDBSync controller with the manager.
func (r *CardanoDBSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &yacdv1alpha1.CardanoDBSync{}, cardanoDBSyncNetworkRefNameField, cardanoDBSyncNetworkRefIndexer); err != nil {
		return err
	}
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &yacdv1alpha1.CardanoDBSync{}, cardanoDBSyncExternalDatabaseSecretNameField, func(object client.Object) []string {
		dbSync, ok := object.(*yacdv1alpha1.CardanoDBSync)
		if !ok || dbSync.Spec.Database.External == nil || dbSync.Spec.Database.External.PasswordSecretRef.Name == "" {
			return nil
		}
		return []string{dbSync.Spec.Database.External.PasswordSecretRef.Name}
	}); err != nil {
		return err
	}
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &yacdv1alpha1.CardanoDBSync{}, cardanoDBSyncManagedDatabaseSecretNameField, func(object client.Object) []string {
		dbSync, ok := object.(*yacdv1alpha1.CardanoDBSync)
		if !ok || dbSync.Spec.Database.Managed == nil {
			return nil
		}
		if dbSync.Spec.Database.Managed.AuthSecretRef == nil {
			return []string{managedPostgresAuthSecretName(dbSync)}
		}
		if dbSync.Spec.Database.Managed.AuthSecretRef.Name == "" {
			return nil
		}

		return []string{dbSync.Spec.Database.Managed.AuthSecretRef.Name}
	}); err != nil {
		return err
	}

	logf.Log.WithName("controllers").WithName(controllerName).
		Info("Starting CardanoDBSync controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&yacdv1alpha1.CardanoDBSync{}, ctrlbuilder.WithPredicates(cardanoDBSyncEventPredicate())).
		Watches(&yacdv1alpha1.CardanoDBSync{}, r.placementPeerEventHandler()).
		Watches(&yacdv1alpha1.CardanoNetwork{}, handler.EnqueueRequestsFromMapFunc(r.cardanoDBSyncsForNetwork)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.cardanoDBSyncsForDatabaseSecret)).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Named(controllerName).
		Complete(r)
}

func cardanoDBSyncEventPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldDBSync, oldOK := e.ObjectOld.(*yacdv1alpha1.CardanoDBSync)
			newDBSync, newOK := e.ObjectNew.(*yacdv1alpha1.CardanoDBSync)
			if !oldOK || !newOK {
				return true
			}
			if oldDBSync.Generation != newDBSync.Generation {
				return true
			}

			return acceptedDatabaseIdentityStatusChanged(oldDBSync, newDBSync)
		},
	}
}

func acceptedDatabaseIdentityStatusChanged(oldDBSync *yacdv1alpha1.CardanoDBSync, newDBSync *yacdv1alpha1.CardanoDBSync) bool {
	return acceptedDatabaseIdentityStatusValue(oldDBSync.Status.Database) !=
		acceptedDatabaseIdentityStatusValue(newDBSync.Status.Database)
}

func acceptedDatabaseIdentityStatusValue(status *yacdv1alpha1.CardanoDBSyncDatabaseStatus) string {
	if status == nil {
		return ""
	}

	return status.AcceptedIdentityFingerprint
}

// placementPeerEventHandler enqueues peer primary-sidecar claims when a
// CardanoDBSync claim is created, deleted, or materially changes. The primary
// object watch already enqueues the changed object itself; this handler exists
// so same-network peers can enter or leave PlacementConflict promptly.
func (r *CardanoDBSyncReconciler) placementPeerEventHandler() handler.EventHandler {
	return handler.Funcs{
		CreateFunc: func(ctx context.Context, event event.CreateEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			r.enqueuePlacementPeersForObject(ctx, queue, event.Object)
		},
		UpdateFunc: func(ctx context.Context, event event.UpdateEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			if event.ObjectOld != nil && event.ObjectNew != nil &&
				event.ObjectOld.GetGeneration() == event.ObjectNew.GetGeneration() {
				return
			}

			r.enqueuePlacementPeersForObject(ctx, queue, event.ObjectOld)
			r.enqueuePlacementPeersForObject(ctx, queue, event.ObjectNew)
		},
		DeleteFunc: func(ctx context.Context, event event.DeleteEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			r.enqueuePlacementPeersForObject(ctx, queue, event.Object)
		},
	}
}

// enqueuePlacementPeersForObject requeues primarySidecar peers for the
// CardanoNetwork referenced by the supplied CardanoDBSync object.
func (r *CardanoDBSyncReconciler) enqueuePlacementPeersForObject(
	ctx context.Context,
	queue workqueue.TypedRateLimitingInterface[reconcile.Request],
	object client.Object,
) {
	dbSync, ok := object.(*yacdv1alpha1.CardanoDBSync)
	if !ok || effectivePlacementMode(dbSync) != yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar {
		return
	}

	r.enqueuePrimarySidecarPeers(ctx, queue, dbSync.Namespace, dbSync.Spec.NetworkRef.Name)
}

// enqueuePrimarySidecarPeers lists and enqueues primarySidecar claims for the
// given CardanoNetwork reference.
func (r *CardanoDBSyncReconciler) enqueuePrimarySidecarPeers(
	ctx context.Context,
	queue workqueue.TypedRateLimitingInterface[reconcile.Request],
	namespace string,
	networkName string,
) {
	claims, err := r.primarySidecarClaims(ctx, namespace, networkName)
	if err != nil {
		logf.FromContext(ctx).Error(err, "Unable to list primary-sidecar CardanoDBSync placement peers", "namespace", namespace, "network", networkName)
		return
	}

	for i := range claims {
		queue.Add(reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&claims[i])})
	}
}

// cardanoDBSyncNetworkRefIndexer indexes CardanoDBSync resources by
// spec.networkRef.name for CardanoNetwork watch fan-out.
func cardanoDBSyncNetworkRefIndexer(object client.Object) []string {
	dbSync, ok := object.(*yacdv1alpha1.CardanoDBSync)
	if !ok || dbSync.Spec.NetworkRef.Name == "" {
		return nil
	}
	return []string{dbSync.Spec.NetworkRef.Name}
}

// validateExternalDatabaseSecret reads and validates the external Postgres
// password Secret. The returned bool is true when the Secret resolved
// cleanly; false when the function already published a Degraded status
// patch (the caller should return the resulting error to controller-
// runtime and wait for the next reconcile).
func (r *CardanoDBSyncReconciler) validateExternalDatabaseSecret(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	database *yacdv1alpha1.CardanoDBSyncExternalDatabaseSpec,
) (*corev1.Secret, bool, error) {
	secretName := database.PasswordSecretRef.Name
	passwordKey := externalDatabasePasswordKey(database)
	if secretName == "" || passwordKey == "" {
		return nil, false, r.patchDependencyUnavailableStatus(ctx, dbSync,
			conditionReasonExternalDatabaseSecretInvalid,
			"External Postgres password Secret reference is incomplete",
		)
	}

	secret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: secretName}, secret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, r.patchDependencyUnavailableStatus(ctx, dbSync,
				conditionReasonExternalDatabaseSecretMissing,
				"External Postgres password Secret does not exist",
			)
		}
		return nil, false, err
	}
	if !secret.DeletionTimestamp.IsZero() {
		return nil, false, r.patchDependencyUnavailableStatus(ctx, dbSync,
			conditionReasonExternalDatabaseSecretMissing,
			"External Postgres password Secret is deleting",
		)
	}
	if len(secret.Data[passwordKey]) == 0 {
		return nil, false, r.patchDependencyUnavailableStatus(ctx, dbSync,
			conditionReasonExternalDatabaseSecretInvalid,
			"External Postgres password Secret does not contain the configured key",
		)
	}
	if strings.ContainsAny(string(secret.Data[passwordKey]), "\r\n") {
		return nil, false, r.patchDependencyUnavailableStatus(ctx, dbSync,
			conditionReasonExternalDatabaseSecretInvalid,
			"External Postgres password Secret value cannot contain newlines",
		)
	}

	return secret, true, nil
}

// cardanoDBSyncsForNetwork is the Watches mapper that enqueues every
// CardanoDBSync that references the given CardanoNetwork. Used so a
// CardanoNetwork status change (artifacts ready, endpoints published)
// triggers downstream CardanoDBSync reconciles.
func (r *CardanoDBSyncReconciler) cardanoDBSyncsForNetwork(ctx context.Context, object client.Object) []reconcile.Request {
	network, ok := object.(*yacdv1alpha1.CardanoNetwork)
	if !ok {
		return nil
	}

	dbSyncs := &yacdv1alpha1.CardanoDBSyncList{}
	if err := r.List(ctx, dbSyncs,
		client.InNamespace(network.Namespace),
		client.MatchingFields{cardanoDBSyncNetworkRefNameField: network.Name},
	); err != nil {
		logf.FromContext(ctx).Error(err, "Unable to list CardanoDBSync resources for CardanoNetwork", "cardanonetwork", client.ObjectKeyFromObject(network))
		return nil
	}

	requests := make([]reconcile.Request, 0, len(dbSyncs.Items))
	for _, dbSync := range dbSyncs.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&dbSync)})
	}
	return requests
}

// cardanoDBSyncsForDatabaseSecret is the Watches mapper that enqueues every
// CardanoDBSync whose external, provided managed, or generated managed database
// Secret matches the given Secret. Used so credential repair or rotation
// re-enters the owning reconcile loop.
func (r *CardanoDBSyncReconciler) cardanoDBSyncsForDatabaseSecret(ctx context.Context, object client.Object) []reconcile.Request {
	secret, ok := object.(*corev1.Secret)
	if !ok {
		return nil
	}

	requestsByKey := map[client.ObjectKey]reconcile.Request{}

	dbSyncs := &yacdv1alpha1.CardanoDBSyncList{}
	if err := r.List(ctx, dbSyncs,
		client.InNamespace(secret.Namespace),
		client.MatchingFields{cardanoDBSyncExternalDatabaseSecretNameField: secret.Name},
	); err != nil {
		logf.FromContext(ctx).Error(err, "Unable to list CardanoDBSync resources for external database Secret", "secret", client.ObjectKeyFromObject(secret))
	} else {
		for _, dbSync := range dbSyncs.Items {
			key := client.ObjectKeyFromObject(&dbSync)
			requestsByKey[key] = reconcile.Request{NamespacedName: key}
		}
	}

	dbSyncs = &yacdv1alpha1.CardanoDBSyncList{}
	if err := r.List(ctx, dbSyncs,
		client.InNamespace(secret.Namespace),
		client.MatchingFields{cardanoDBSyncManagedDatabaseSecretNameField: secret.Name},
	); err != nil {
		logf.FromContext(ctx).Error(err, "Unable to list CardanoDBSync resources for managed database Secret", "secret", client.ObjectKeyFromObject(secret))
	} else {
		for _, dbSync := range dbSyncs.Items {
			key := client.ObjectKeyFromObject(&dbSync)
			requestsByKey[key] = reconcile.Request{NamespacedName: key}
		}
	}

	requests := make([]reconcile.Request, 0, len(requestsByKey))
	for _, request := range requestsByKey {
		requests = append(requests, request)
	}
	return requests
}

// liveReader is the uncached reader for reads that must observe the
// freshest cluster state. When the Reconciler was constructed with a
// dedicated Reader (typical for envtest), the function returns it;
// otherwise it falls back to the cached Client.
func (r *CardanoDBSyncReconciler) liveReader() client.Reader {
	if r.Reader != nil {
		return r.Reader
	}
	return r.Client
}
