// Package cardanodbsync contains the CardanoDBSync controller.
package cardanodbsync

import (
	"context"
	"strings"
	"time"

	"github.com/go-logr/logr"
	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlbuilder "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	networkArtifactSchemaVersionAnno   = "yacd.meigma.io/artifact-schema-version"
	networkArtifactDataHashAnno        = "yacd.meigma.io/artifact-data-hash"
	defaultExternalDatabasePasswordKey = "password"

	dbSyncWorkloadReadinessRequeueAfter = 15 * time.Second
)

// CardanoDBSyncReconciler reconciles CardanoDBSync resources.
type CardanoDBSyncReconciler struct {
	// Client is the controller-runtime client used to read and write
	// CardanoDBSync resources and cached dependencies.
	client.Client

	// Reader is the uncached reader used for live dependency checks.
	Reader client.Reader

	// Scheme is the runtime scheme available to future owned child resources.
	Scheme *runtime.Scheme
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
	if network.Status.Artifacts == nil ||
		network.Status.Artifacts.NetworkConfigMapName == "" ||
		network.Status.Artifacts.SchemaVersion == "" ||
		network.Status.Artifacts.DataHash == "" {
		return ctrl.Result{}, r.patchDependencyWaitingStatus(ctx, dbSync,
			conditionReasonNetworkArtifactsPending,
			"Referenced CardanoNetwork artifact status is incomplete",
		)
	}

	configMap := &corev1.ConfigMap{}
	configMapKey := client.ObjectKey{Namespace: dbSync.Namespace, Name: network.Status.Artifacts.NetworkConfigMapName}
	if err := r.liveReader().Get(ctx, configMapKey, configMap); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, r.patchDependencyWaitingStatus(ctx, dbSync,
				conditionReasonNetworkArtifactsPending,
				"Referenced CardanoNetwork artifact ConfigMap does not exist",
			)
		}
		return ctrl.Result{}, err
	}
	if !configMap.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.patchDependencyWaitingStatus(ctx, dbSync,
			conditionReasonNetworkArtifactsPending,
			"Referenced CardanoNetwork artifact ConfigMap is deleting",
		)
	}
	if configMap.Annotations[networkArtifactSchemaVersionAnno] != network.Status.Artifacts.SchemaVersion ||
		configMap.Annotations[networkArtifactDataHashAnno] != network.Status.Artifacts.DataHash {
		return ctrl.Result{}, r.patchDependencyWaitingStatus(ctx, dbSync,
			conditionReasonNetworkArtifactsMismatch,
			"Referenced CardanoNetwork artifact ConfigMap metadata does not match status",
		)
	}
	if err := validateNetworkArtifactsConfigMapData(configMap, network.Status.Artifacts.DataHash); err != nil {
		return ctrl.Result{}, r.patchDependencyWaitingStatus(ctx, dbSync,
			conditionReasonNetworkArtifactsMismatch,
			"Referenced CardanoNetwork artifact ConfigMap is invalid: "+err.Error(),
		)
	}
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

	return r.reconcileWorkloads(ctx, log, dbSync, network, configMap, databaseRuntime)
}

func (r *CardanoDBSyncReconciler) reconcileWorkloads(
	ctx context.Context,
	log logr.Logger,
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
	configMap *corev1.ConfigMap,
	databaseRuntime databaseRuntime,
) (ctrl.Result, error) {
	builder := dbSyncWorkloadBuilder{scheme: r.Scheme}
	if databaseRuntime.Mode == databaseModeManaged {
		ready, err := r.reconcileManagedPostgres(ctx, log, dbSync, builder, databaseRuntime)
		if err != nil {
			return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
		}
		if ready.Status != metav1.ConditionTrue {
			if err := r.patchManagedPostgresAppliedStatus(ctx, dbSync, databaseRuntime, ready); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: dbSyncWorkloadReadinessRequeueAfter}, nil
		}
	}

	resources, err := builder.BuildForDatabase(dbSync, network, configMap, databaseRuntime.PasswordSecret, databaseRuntime.Database)
	if err != nil {
		return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
	}
	if err := r.validateAcceptedDBSyncDatabaseIdentity(ctx, dbSync, resources.Plan.DatabaseIdentityFingerprint.Value); err != nil {
		return r.handleDBSyncWorkloadApplyError(ctx, dbSync, err)
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

	ready, err := r.patchWorkloadsAppliedStatus(ctx, dbSync, resources.MetricsService, databaseRuntime, resources.Plan.DatabaseIdentityFingerprint.Value)
	if err != nil {
		return ctrl.Result{}, err
	}
	if ready.Status != metav1.ConditionTrue && ready.Reason == conditionReasonDeploymentProgressing {
		return ctrl.Result{RequeueAfter: dbSyncWorkloadReadinessRequeueAfter}, nil
	}

	return ctrl.Result{}, nil
}

func (r *CardanoDBSyncReconciler) reconcileManagedPostgres(
	ctx context.Context,
	log logr.Logger,
	dbSync *yacdv1alpha1.CardanoDBSync,
	builder dbSyncWorkloadBuilder,
	databaseRuntime databaseRuntime,
) (metav1.Condition, error) {
	resources, err := builder.managedPostgresResources(dbSync, databaseRuntime.PasswordSecret)
	if err != nil {
		return metav1.Condition{}, err
	}
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
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &yacdv1alpha1.CardanoDBSync{}, cardanoDBSyncNetworkRefNameField, func(object client.Object) []string {
		dbSync, ok := object.(*yacdv1alpha1.CardanoDBSync)
		if !ok || dbSync.Spec.NetworkRef.Name == "" {
			return nil
		}
		return []string{dbSync.Spec.NetworkRef.Name}
	}); err != nil {
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
		if !ok || dbSync.Spec.Database.Managed == nil || dbSync.Spec.Database.Managed.AuthSecretRef == nil || dbSync.Spec.Database.Managed.AuthSecretRef.Name == "" {
			return nil
		}
		return []string{dbSync.Spec.Database.Managed.AuthSecretRef.Name}
	}); err != nil {
		return err
	}

	logf.Log.WithName("controllers").WithName(controllerName).
		Info("Starting CardanoDBSync controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&yacdv1alpha1.CardanoDBSync{}, ctrlbuilder.WithPredicates(predicate.GenerationChangedPredicate{})).
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

func externalDatabasePasswordKey(database *yacdv1alpha1.CardanoDBSyncExternalDatabaseSpec) string {
	if database.PasswordSecretRef.Key != "" {
		return database.PasswordSecretRef.Key
	}
	return defaultExternalDatabasePasswordKey
}

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

func (r *CardanoDBSyncReconciler) liveReader() client.Reader {
	if r.Reader != nil {
		return r.Reader
	}
	return r.Client
}
