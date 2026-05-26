package cardanodbsync

import (
	"context"
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/dbsync"
	ctrlreadiness "github.com/meigma/yacd/internal/ctrlkit/readiness"
	ctrlstatus "github.com/meigma/yacd/internal/ctrlkit/status"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	conditionTypeProgressing       = "Progressing"
	conditionTypeDegraded          = "Degraded"
	conditionTypeReady             = "Ready"
	conditionTypeFollowerNodeReady = "FollowerNodeReady"
	conditionTypePostgresReady     = "PostgresReady"
	conditionTypeDBSyncReady       = "DBSyncReady"
	conditionTypeSynced            = "Synced"

	conditionReasonReconcileSucceeded                = "ReconcileSucceeded"
	conditionReasonUnsupportedDatabaseMode           = "UnsupportedDatabaseMode"
	conditionReasonExternalDatabaseSecretMissing     = "ExternalDatabaseSecretMissing"
	conditionReasonExternalDatabaseSecretInvalid     = "ExternalDatabaseSecretInvalid"
	conditionReasonManagedDatabaseSecretMissing      = "ManagedDatabaseSecretMissing"
	conditionReasonManagedDatabaseSecretInvalid      = "ManagedDatabaseSecretInvalid"
	conditionReasonNetworkUnavailable                = "NetworkUnavailable"
	conditionReasonNetworkStatusStale                = "NetworkStatusStale"
	conditionReasonNetworkArtifactsPending           = "NetworkArtifactsPending"
	conditionReasonNetworkArtifactsMismatch          = "NetworkArtifactsMismatch"
	conditionReasonNodeToNodeEndpointMissing         = "NodeToNodeEndpointMissing"
	conditionReasonWorkloadMissing                   = "WorkloadMissing"
	conditionReasonDeploymentProgressing             = "DeploymentProgressing"
	conditionReasonFollowerNodeReady                 = "FollowerNodeReady"
	conditionReasonDBSyncReady                       = "DBSyncReady"
	conditionReasonPostgresReady                     = "PostgresReady"
	conditionReasonExternalDatabaseNotProbed         = "ExternalDatabaseNotProbed"
	conditionReasonSyncNotProbed                     = "SyncNotProbed"
	conditionReasonRuntimeProbesPending              = "RuntimeProbesPending"
	conditionReasonPostgresUnavailable               = "PostgresUnavailable"
	conditionReasonPostgresSchemaPending             = "PostgresSchemaPending"
	conditionReasonNodeTipUnavailable                = "NodeTipUnavailable"
	conditionReasonSyncLagging                       = "SyncLagging"
	conditionReasonSynced                            = "Synced"
	conditionReasonUnsupportedSpec                   = "UnsupportedSpec"
	conditionReasonUnsupportedStorageChange          = "UnsupportedStorageChange"
	conditionReasonUnsupportedWorkloadChange         = "UnsupportedWorkloadChange"
	conditionReasonUnsupportedDatabaseIdentityChange = "UnsupportedDatabaseIdentityChange"
	conditionReasonResourceConflict                  = "ResourceConflict"
	conditionReasonReady                             = "Ready"
)

func networkArtifactsReady(network *yacdv1alpha1.CardanoNetwork) bool {
	condition := apimeta.FindStatusCondition(network.Status.Conditions, "ArtifactsReady")
	return condition != nil &&
		condition.Status == metav1.ConditionTrue &&
		condition.ObservedGeneration >= network.Generation
}

func (r *CardanoDBSyncReconciler) patchDependencyUnavailableStatus(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	reason string,
	message string,
) error {
	if err := r.suspendDBSyncDeploymentIfOwned(ctx, dbSync); err != nil {
		return err
	}

	return r.patchStatusConditions(ctx, dbSync,
		degradedCondition(metav1.ConditionTrue, reason, message),
		progressingCondition(metav1.ConditionFalse, reason, message),
		readyCondition(reason, message),
		followerNodeReadyCondition(reason, message),
		postgresReadyCondition(reason, message),
		dbSyncReadyCondition(reason, message),
		syncedCondition(reason, message),
	)
}

func (r *CardanoDBSyncReconciler) patchDependencyWaitingStatus(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	reason string,
	message string,
) error {
	if err := r.suspendDBSyncDeploymentIfOwned(ctx, dbSync); err != nil {
		return err
	}

	return r.patchStatusConditions(ctx, dbSync,
		degradedCondition(metav1.ConditionFalse, conditionReasonReconcileSucceeded, "CardanoDBSync dependencies are still converging"),
		progressingCondition(metav1.ConditionTrue, reason, message),
		readyCondition(reason, message),
		followerNodeReadyCondition(reason, message),
		postgresReadyCondition(reason, message),
		dbSyncReadyCondition(reason, message),
		syncedCondition(reason, message),
	)
}

func (r *CardanoDBSyncReconciler) patchWorkloadsAppliedStatus(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
	metricsService *corev1.Service,
	databaseRuntime databaseRuntime,
	acceptedIdentityFingerprint string,
) (metav1.Condition, bool, error) {
	followerNodeReady, err := r.workloadContainerReadyCondition(ctx, dbSync, followerNodeContainerName, conditionTypeFollowerNodeReady, conditionReasonFollowerNodeReady, "Follower node container is ready", "Follower node container is not ready")
	if err != nil {
		return metav1.Condition{}, false, err
	}
	dbSyncReady, err := r.workloadContainerReadyCondition(ctx, dbSync, dbSyncContainerName, conditionTypeDBSyncReady, conditionReasonDBSyncReady, "db-sync container is ready", "db-sync container is not ready")
	if err != nil {
		return metav1.Condition{}, false, err
	}

	probed := false
	syncStatus := (*yacdv1alpha1.CardanoDBSyncProgressStatus)(nil)
	var postgresReady metav1.Condition
	synced := syncedCondition(conditionReasonRuntimeProbesPending, "db-sync progress will be probed after workloads are ready")
	if followerNodeReady.Status == metav1.ConditionTrue && dbSyncReady.Status == metav1.ConditionTrue {
		probeResult, err := r.runtimeProber().Probe(ctx, dbSyncRuntimeProbeTarget{
			Database:       databaseRuntime.Database,
			PasswordSecret: databaseRuntime.PasswordSecret,
			OgmiosURL:      ogmiosEndpointURL(network),
		})
		if err != nil {
			return metav1.Condition{}, false, err
		}
		probed = true
		syncStatus = probeResult.Sync
		postgresReady = probeResult.PostgresReady
		synced = probeResult.Synced
	} else {
		probeResult, err := r.runtimeProber().ProbePostgres(ctx, dbSyncRuntimeProbeTarget{
			Database:       databaseRuntime.Database,
			PasswordSecret: databaseRuntime.PasswordSecret,
			OgmiosURL:      ogmiosEndpointURL(network),
		})
		if err != nil {
			return metav1.Condition{}, false, err
		}
		probed = true
		syncStatus = probeResult.Sync
		postgresReady = probeResult.PostgresReady
		if probeResult.PostgresReady.Status != metav1.ConditionTrue ||
			probeResult.Synced.Reason == conditionReasonPostgresSchemaPending {
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
		degradedCondition(metav1.ConditionFalse, conditionReasonReconcileSucceeded, "CardanoDBSync workloads are applied"),
		progressing,
		ready,
		followerNodeReady,
		postgresReady,
		dbSyncReady,
		synced,
	)
	return ready, probed, err
}

func (r *CardanoDBSyncReconciler) patchManagedPostgresAppliedStatus(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	databaseRuntime databaseRuntime,
	postgresReady metav1.Condition,
	acceptedIdentityFingerprint string,
) error {
	followerNodeReady, err := r.workloadContainerReadyCondition(ctx, dbSync, followerNodeContainerName, conditionTypeFollowerNodeReady, conditionReasonFollowerNodeReady, "Follower node container is ready", "Follower node container is not ready")
	if err != nil {
		return err
	}
	dbSyncReady, err := r.workloadContainerReadyCondition(ctx, dbSync, dbSyncContainerName, conditionTypeDBSyncReady, conditionReasonDBSyncReady, "db-sync container is ready", "db-sync container is not ready")
	if err != nil {
		return err
	}
	metricsEndpoint, err := r.currentDBSyncMetricsEndpoint(ctx, dbSync)
	if err != nil {
		return err
	}

	synced := syncedCondition(conditionReasonSyncNotProbed, "db-sync chain progress has not been probed yet")
	return r.patchStatus(ctx, dbSync, func(status *yacdv1alpha1.CardanoDBSyncStatus) {
		status.Endpoints = &yacdv1alpha1.CardanoDBSyncEndpointsStatus{
			Postgres: databaseRuntime.PostgresEndpoint,
			Metrics:  metricsEndpoint,
		}
		status.Database = databaseStatus(acceptedIdentityFingerprint, databaseRuntime.GeneratedAuthSecretName)
		status.Sync = nil
	},
		degradedCondition(metav1.ConditionFalse, conditionReasonReconcileSucceeded, "Managed Postgres resources are applied"),
		progressingCondition(metav1.ConditionTrue, postgresReady.Reason, postgresReady.Message),
		readyCondition(postgresReady.Reason, postgresReady.Message),
		followerNodeReady,
		postgresReady,
		dbSyncReady,
		synced,
	)
}

func (r *CardanoDBSyncReconciler) patchWorkloadApplyBlockedStatus(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	reason string,
	message string,
) error {
	if err := r.suspendDBSyncDeploymentIfOwned(ctx, dbSync); err != nil {
		return err
	}

	return r.patchStatusConditions(ctx, dbSync,
		degradedCondition(metav1.ConditionTrue, reason, message),
		progressingCondition(metav1.ConditionFalse, reason, message),
		readyCondition(reason, message),
		followerNodeReadyCondition(reason, message),
		postgresReadyCondition(reason, message),
		dbSyncReadyCondition(reason, message),
		syncedCondition(reason, message),
	)
}

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

func (r *CardanoDBSyncReconciler) workloadContainerReadyCondition(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	containerName string,
	conditionType string,
	readyReason string,
	readyMessage string,
	notReadyMessage string,
) (metav1.Condition, error) {
	readiness, err := r.deploymentContainerReadiness(
		ctx,
		dbSync.Namespace,
		dbSyncWorkloadName(dbSync),
		dbSyncWorkloadSelectorLabels(dbSync),
		containerName,
	)
	if err != nil {
		return metav1.Condition{}, err
	}

	return deploymentContainerCondition(
		readiness,
		conditionType,
		readyReason,
		readyMessage,
		"CardanoDBSync Deployment is missing",
		"CardanoDBSync Deployment has not observed the latest generation",
		"CardanoDBSync Deployment is not available",
		notReadyMessage,
	), nil
}

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

func (r *CardanoDBSyncReconciler) managedPostgresReadyCondition(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) (metav1.Condition, error) {
	readiness, err := r.deploymentContainerReadiness(
		ctx,
		dbSync.Namespace,
		managedPostgresDeploymentName(dbSync),
		managedPostgresSelectorLabels(dbSync),
		managedPostgresContainerName,
	)
	if err != nil {
		return metav1.Condition{}, err
	}

	return deploymentContainerCondition(
		readiness,
		conditionTypePostgresReady,
		conditionReasonPostgresReady,
		"Managed Postgres container is ready",
		"Managed Postgres Deployment is missing",
		"Managed Postgres Deployment has not observed the latest generation",
		"Managed Postgres Deployment is not available",
		"Managed Postgres container is not ready",
	), nil
}

func (r *CardanoDBSyncReconciler) deploymentContainerReadiness(
	ctx context.Context,
	namespace string,
	deploymentName string,
	selectorLabels map[string]string,
	containerName string,
) (ctrlreadiness.DeploymentReadinessState, error) {
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: deploymentName}, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrlreadiness.DeploymentMissing, nil
		}
		return "", err
	}

	pods := &corev1.PodList{}
	if err := r.statusReader().List(
		ctx,
		pods,
		client.InNamespace(namespace),
		client.MatchingLabels(selectorLabels),
	); err != nil {
		return "", err
	}

	return ctrlreadiness.DeploymentReadiness(deployment, pods.Items, containerName), nil
}

func deploymentContainerCondition(
	readiness ctrlreadiness.DeploymentReadinessState,
	conditionType string,
	readyReason string,
	readyMessage string,
	missingMessage string,
	staleMessage string,
	unavailableMessage string,
	containerNotReadyMessage string,
) metav1.Condition {
	switch readiness {
	case ctrlreadiness.DeploymentReady:
		return ctrlstatus.Condition(conditionType, metav1.ConditionTrue, readyReason, readyMessage)
	case ctrlreadiness.DeploymentMissing:
		return ctrlstatus.Condition(conditionType, metav1.ConditionFalse, conditionReasonWorkloadMissing, missingMessage)
	case ctrlreadiness.DeploymentStale:
		return ctrlstatus.Condition(conditionType, metav1.ConditionFalse, conditionReasonDeploymentProgressing, staleMessage)
	case ctrlreadiness.DeploymentUnavailable:
		return ctrlstatus.Condition(conditionType, metav1.ConditionFalse, conditionReasonDeploymentProgressing, unavailableMessage)
	default:
		return ctrlstatus.Condition(conditionType, metav1.ConditionFalse, conditionReasonDeploymentProgressing, containerNotReadyMessage)
	}
}

func (r *CardanoDBSyncReconciler) statusReader() client.Reader {
	return r.liveReader()
}

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

func databaseStatus(acceptedIdentityFingerprint string, generatedAuthSecretName string) *yacdv1alpha1.CardanoDBSyncDatabaseStatus {
	if acceptedIdentityFingerprint == "" && generatedAuthSecretName == "" {
		return nil
	}

	return &yacdv1alpha1.CardanoDBSyncDatabaseStatus{
		AcceptedIdentityFingerprint: acceptedIdentityFingerprint,
		AuthSecretName:              generatedAuthSecretName,
	}
}

func workloadsReadyCondition(followerNodeReady metav1.Condition, dbSyncReady metav1.Condition, postgresReady metav1.Condition, synced metav1.Condition) metav1.Condition {
	return ctrlstatus.AggregateReady(
		conditionTypeReady,
		conditionReasonReady,
		"CardanoDBSync is ready",
		followerNodeReady,
		postgresReady,
		dbSyncReady,
		synced,
	)
}

func progressingForReadyCondition(ready metav1.Condition) metav1.Condition {
	return ctrlstatus.ProgressingForReady(
		conditionTypeProgressing,
		conditionReasonReady,
		"CardanoDBSync is ready",
		ready,
		conditionReasonDeploymentProgressing,
		conditionReasonWorkloadMissing,
		conditionReasonRuntimeProbesPending,
		conditionReasonPostgresUnavailable,
		conditionReasonPostgresSchemaPending,
		conditionReasonNodeTipUnavailable,
		conditionReasonSyncLagging,
	)
}

func degradedCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlstatus.Condition(conditionTypeDegraded, status, reason, message)
}

func progressingCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlstatus.Condition(conditionTypeProgressing, status, reason, message)
}

func readyCondition(reason string, message string) metav1.Condition {
	return ctrlstatus.Condition(conditionTypeReady, metav1.ConditionFalse, reason, message)
}

func followerNodeReadyCondition(reason string, message string) metav1.Condition {
	return ctrlstatus.Condition(conditionTypeFollowerNodeReady, metav1.ConditionFalse, reason, message)
}

func postgresReadyCondition(reason string, message string) metav1.Condition {
	return ctrlstatus.Condition(conditionTypePostgresReady, metav1.ConditionFalse, reason, message)
}

func dbSyncReadyCondition(reason string, message string) metav1.Condition {
	return ctrlstatus.Condition(conditionTypeDBSyncReady, metav1.ConditionFalse, reason, message)
}

func syncedCondition(reason string, message string) metav1.Condition {
	return ctrlstatus.Condition(conditionTypeSynced, metav1.ConditionFalse, reason, message)
}
