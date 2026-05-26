package cardanodbsync

import (
	"context"
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/dbsync"
	ctrlstatus "github.com/meigma/yacd/internal/ctrlkit/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// networkArtifactsReady reports whether the referenced CardanoNetwork has a
// fresh ArtifactsReady=True condition observed at the current generation.
// The reconciler uses it to gate workload apply on upstream readiness.
func networkArtifactsReady(network *yacdv1alpha1.CardanoNetwork) bool {
	condition := apimeta.FindStatusCondition(network.Status.Conditions, "ArtifactsReady")
	return condition != nil &&
		condition.Status == metav1.ConditionTrue &&
		condition.ObservedGeneration >= network.Generation
}

// patchDependencyUnavailableStatus suspends the dbsync workload and writes a
// Degraded status patch. Used when a hard dependency (database Secret,
// referenced CardanoNetwork) is missing or invalid.
func (r *CardanoDBSyncReconciler) patchDependencyUnavailableStatus(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	reason conditionReason,
	message string,
) error {
	if err := r.suspendDBSyncDeploymentIfOwned(ctx, dbSync); err != nil {
		return err
	}

	return r.patchStatusConditions(ctx, dbSync,
		degradedCondition(metav1.ConditionTrue, reason, message),
		progressingCondition(metav1.ConditionFalse, reason, message),
		readyCondition(reason, message),
		followerNodeReadyCondition(metav1.ConditionFalse, reason, message),
		postgresReadyCondition(metav1.ConditionFalse, reason, message),
		dbSyncReadyCondition(metav1.ConditionFalse, reason, message),
		syncedCondition(metav1.ConditionFalse, reason, message),
	)
}

// patchDependencyWaitingStatus suspends the dbsync workload and writes a
// Progressing status patch. Used when a soft dependency (network status
// stale, artifacts still being verified) is still converging.
func (r *CardanoDBSyncReconciler) patchDependencyWaitingStatus(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	reason conditionReason,
	message string,
) error {
	if err := r.suspendDBSyncDeploymentIfOwned(ctx, dbSync); err != nil {
		return err
	}

	return r.patchStatusConditions(ctx, dbSync,
		degradedCondition(metav1.ConditionFalse, conditionReasonReconcileSucceeded, conditionMessageDependenciesConverging),
		progressingCondition(metav1.ConditionTrue, reason, message),
		readyCondition(reason, message),
		followerNodeReadyCondition(metav1.ConditionFalse, reason, message),
		postgresReadyCondition(metav1.ConditionFalse, reason, message),
		dbSyncReadyCondition(metav1.ConditionFalse, reason, message),
		syncedCondition(metav1.ConditionFalse, reason, message),
	)
}

// patchWorkloadsAppliedStatus computes per-component readiness for the
// freshly applied dbsync workload, runs the Postgres + Ogmios runtime
// probes, and writes the aggregated status patch. Returns the aggregate
// Ready condition along with whether a runtime probe ran (used by the
// caller to decide requeue cadence).
func (r *CardanoDBSyncReconciler) patchWorkloadsAppliedStatus(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
	metricsService *corev1.Service,
	databaseRuntime databaseRuntime,
	acceptedIdentityFingerprint string,
) (metav1.Condition, bool, error) {
	followerNodeReady, err := r.dbSyncContainerReadyCondition(ctx, dbSync, followerNodeContainerName, followerNodeReadyCondition, conditionReasonFollowerNodeReady, conditionMessageFollowerNodeReady, conditionMessageFollowerNodeNotReady)
	if err != nil {
		return metav1.Condition{}, false, err
	}
	dbSyncReady, err := r.dbSyncContainerReadyCondition(ctx, dbSync, dbSyncContainerName, dbSyncReadyCondition, conditionReasonDBSyncReady, conditionMessageDBSyncContainerReady, conditionMessageDBSyncContainerNotReady)
	if err != nil {
		return metav1.Condition{}, false, err
	}

	target := dbSyncRuntimeProbeTarget{
		Database:       databaseRuntime.Database,
		PasswordSecret: databaseRuntime.PasswordSecret,
		OgmiosURL:      ogmiosEndpointURL(network),
	}
	var (
		probed        bool
		syncStatus    *yacdv1alpha1.CardanoDBSyncProgressStatus
		postgresReady metav1.Condition
		synced        = syncedCondition(metav1.ConditionFalse, conditionReasonRuntimeProbesPending, conditionMessageSyncProbedPending)
	)
	// Only probe the node tip once both containers are ready; otherwise just
	// probe Postgres so the operator sees a partial signal while the
	// follower-node and db-sync containers are still starting.
	if followerNodeReady.Status == metav1.ConditionTrue && dbSyncReady.Status == metav1.ConditionTrue {
		probeResult, err := r.runtimeProber().Probe(ctx, target)
		if err != nil {
			return metav1.Condition{}, false, err
		}
		probed = true
		syncStatus = probeResult.Sync
		postgresReady = probeResult.PostgresReady
		synced = probeResult.Synced
	} else {
		probeResult, err := r.runtimeProber().ProbePostgres(ctx, target)
		if err != nil {
			return metav1.Condition{}, false, err
		}
		probed = true
		syncStatus = probeResult.Sync
		postgresReady = probeResult.PostgresReady
		if probeResult.PostgresReady.Status != metav1.ConditionTrue ||
			probeResult.Synced.Reason == string(conditionReasonPostgresSchemaPending) {
			synced = probeResult.Synced
		}
	}
	ready := workloadsReadyCondition(followerNodeReady, dbSyncReady, postgresReady, synced)
	progressing := progressingForReadyCondition(ready)

	err = r.patchStatus(ctx, dbSync, func(status *yacdv1alpha1.CardanoDBSyncStatus) {
		status.Endpoints = &yacdv1alpha1.CardanoDBSyncEndpointsStatus{
			Postgres: databaseRuntime.PostgresEndpoint,
			Metrics:  serviceEndpointFor(metricsService, "http", "/metrics"),
		}
		status.Database = databaseStatus(acceptedIdentityFingerprint, databaseRuntime.GeneratedAuthSecretName)
		status.Sync = syncStatus
	},
		degradedCondition(metav1.ConditionFalse, conditionReasonReconcileSucceeded, conditionMessageWorkloadsApplied),
		progressing,
		ready,
		followerNodeReady,
		postgresReady,
		dbSyncReady,
		synced,
	)
	return ready, probed, err
}

// patchManagedPostgresAppliedStatus writes a status patch for the
// intermediate state where managed Postgres is applied but the dbsync
// workload still depends on it. Postgres readiness comes from the caller;
// the dbsync runtime probes are deferred until follower-node and db-sync
// are ready.
func (r *CardanoDBSyncReconciler) patchManagedPostgresAppliedStatus(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	databaseRuntime databaseRuntime,
	postgresReady metav1.Condition,
	acceptedIdentityFingerprint string,
) error {
	followerNodeReady, err := r.dbSyncContainerReadyCondition(ctx, dbSync, followerNodeContainerName, followerNodeReadyCondition, conditionReasonFollowerNodeReady, conditionMessageFollowerNodeReady, conditionMessageFollowerNodeNotReady)
	if err != nil {
		return err
	}
	dbSyncReady, err := r.dbSyncContainerReadyCondition(ctx, dbSync, dbSyncContainerName, dbSyncReadyCondition, conditionReasonDBSyncReady, conditionMessageDBSyncContainerReady, conditionMessageDBSyncContainerNotReady)
	if err != nil {
		return err
	}
	metricsEndpoint, err := r.currentDBSyncMetricsEndpoint(ctx, dbSync)
	if err != nil {
		return err
	}

	synced := syncedCondition(metav1.ConditionFalse, conditionReasonSyncNotProbed, conditionMessageSyncNotProbed)
	postgresReason := conditionReason(postgresReady.Reason)
	return r.patchStatus(ctx, dbSync, func(status *yacdv1alpha1.CardanoDBSyncStatus) {
		status.Endpoints = &yacdv1alpha1.CardanoDBSyncEndpointsStatus{
			Postgres: databaseRuntime.PostgresEndpoint,
			Metrics:  metricsEndpoint,
		}
		status.Database = databaseStatus(acceptedIdentityFingerprint, databaseRuntime.GeneratedAuthSecretName)
		status.Sync = nil
	},
		degradedCondition(metav1.ConditionFalse, conditionReasonReconcileSucceeded, conditionMessageManagedPostgresApplied),
		progressingCondition(metav1.ConditionTrue, postgresReason, postgresReady.Message),
		readyCondition(postgresReason, postgresReady.Message),
		followerNodeReady,
		postgresReady,
		dbSyncReady,
		synced,
	)
}

// patchWorkloadApplyBlockedStatus suspends the dbsync workload and writes a
// Degraded status patch. Used when builder validation or owned-child apply
// fails with a typed condition error.
func (r *CardanoDBSyncReconciler) patchWorkloadApplyBlockedStatus(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	reason conditionReason,
	message string,
) error {
	if err := r.suspendDBSyncDeploymentIfOwned(ctx, dbSync); err != nil {
		return err
	}

	return r.patchStatusConditions(ctx, dbSync,
		degradedCondition(metav1.ConditionTrue, reason, message),
		progressingCondition(metav1.ConditionFalse, reason, message),
		readyCondition(reason, message),
		followerNodeReadyCondition(metav1.ConditionFalse, reason, message),
		postgresReadyCondition(metav1.ConditionFalse, reason, message),
		dbSyncReadyCondition(metav1.ConditionFalse, reason, message),
		syncedCondition(metav1.ConditionFalse, reason, message),
	)
}

// patchStatusConditions writes a CardanoDBSync status patch carrying only
// the supplied conditions. Endpoints and sync state are cleared because
// callers use this for non-applied paths (dependency unavailable, apply
// blocked) where neither is meaningful.
func (r *CardanoDBSyncReconciler) patchStatusConditions(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	conditions ...metav1.Condition,
) error {
	return r.patchStatus(ctx, dbSync, func(status *yacdv1alpha1.CardanoDBSyncStatus) {
		acceptedIdentityFingerprint := ""
		generatedAuthSecretName := ""
		if status.Database != nil {
			acceptedIdentityFingerprint = status.Database.AcceptedIdentityFingerprint
			generatedAuthSecretName = status.Database.AuthSecretName
		}
		status.Endpoints = nil
		status.Sync = nil
		status.Database = databaseStatus(acceptedIdentityFingerprint, generatedAuthSecretName)
	}, conditions...)
}

// patchStatus writes the CardanoDBSync status patch. mutate carries the
// payload mutations; this function owns the observedGeneration stamp and
// the diff-aware patch through ctrlstatus.
func (r *CardanoDBSyncReconciler) patchStatus(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	mutate func(*yacdv1alpha1.CardanoDBSyncStatus),
	conditions ...metav1.Condition,
) error {
	original := dbSync.DeepCopy()
	dbSync.Status.ObservedGeneration = dbSync.Generation
	if mutate != nil {
		mutate(&dbSync.Status)
	}
	ctrlstatus.SetObserved(&dbSync.Status.Conditions, dbSync.Generation, conditions...)

	return ctrlstatus.PatchIfChanged(ctx, r.Status(), dbSync, original)
}

// currentDBSyncMetricsEndpoint reads the live db-sync metrics Service to
// derive the status endpoint payload. Returns nil when the Service is
// missing or no longer owned by this CardanoDBSync.
func (r *CardanoDBSyncReconciler) currentDBSyncMetricsEndpoint(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) (*yacdv1alpha1.ServiceEndpointStatus, error) {
	service := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncMetricsServiceName(dbSync)}, service); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if !controlledBy(service, dbSync) {
		return nil, nil
	}

	return serviceEndpointFor(service, "http", "/metrics"), nil
}

// serviceEndpointFor renders the canonical in-cluster URL for a metrics or
// API Service into a status endpoint payload. scheme and path are appended
// to the Service's first port to form the URL.
func serviceEndpointFor(service *corev1.Service, scheme string, path string) *yacdv1alpha1.ServiceEndpointStatus {
	if service == nil || len(service.Spec.Ports) == 0 {
		return nil
	}

	port := service.Spec.Ports[0].Port
	status := &yacdv1alpha1.ServiceEndpointStatus{
		ServiceName: service.Name,
		Port:        port,
	}
	if scheme != "" {
		status.URL = fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d%s", scheme, service.Name, service.Namespace, port, path)
	}

	return status
}

// postgresEndpointFor renders the Postgres connection URL into a status
// endpoint payload. serviceName is empty for external databases (the
// payload then carries the user-provided host).
func postgresEndpointFor(database dbsync.Database, serviceName string) *yacdv1alpha1.ServiceEndpointStatus {
	if database.Host == "" || database.Port == 0 {
		return nil
	}

	return &yacdv1alpha1.ServiceEndpointStatus{
		ServiceName: serviceName,
		Port:        database.Port,
		URL:         fmt.Sprintf("postgres://%s:%d/%s", database.Host, database.Port, database.Name),
	}
}

// databaseStatus builds the CardanoDBSyncDatabaseStatus payload, returning
// nil when neither field has a value. Used by every status patcher to
// preserve the accepted identity fingerprint across reconciles.
func databaseStatus(acceptedIdentityFingerprint string, generatedAuthSecretName string) *yacdv1alpha1.CardanoDBSyncDatabaseStatus {
	if acceptedIdentityFingerprint == "" && generatedAuthSecretName == "" {
		return nil
	}

	return &yacdv1alpha1.CardanoDBSyncDatabaseStatus{
		AcceptedIdentityFingerprint: acceptedIdentityFingerprint,
		AuthSecretName:              generatedAuthSecretName,
	}
}
