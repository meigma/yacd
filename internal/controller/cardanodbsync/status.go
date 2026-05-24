package cardanodbsync

import (
	"context"
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
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
	conditionReasonNetworkUnavailable                = "NetworkUnavailable"
	conditionReasonNetworkStatusStale                = "NetworkStatusStale"
	conditionReasonNetworkArtifactsPending           = "NetworkArtifactsPending"
	conditionReasonNetworkArtifactsMismatch          = "NetworkArtifactsMismatch"
	conditionReasonNodeToNodeEndpointMissing         = "NodeToNodeEndpointMissing"
	conditionReasonWorkloadMissing                   = "WorkloadMissing"
	conditionReasonDeploymentProgressing             = "DeploymentProgressing"
	conditionReasonFollowerNodeReady                 = "FollowerNodeReady"
	conditionReasonDBSyncReady                       = "DBSyncReady"
	conditionReasonExternalDatabaseNotProbed         = "ExternalDatabaseNotProbed"
	conditionReasonSyncNotProbed                     = "SyncNotProbed"
	conditionReasonRuntimeProbesPending              = "RuntimeProbesPending"
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
	metricsService *corev1.Service,
	externalDatabase *yacdv1alpha1.CardanoDBSyncExternalDatabaseSpec,
	acceptedIdentityFingerprint string,
) (metav1.Condition, error) {
	followerNodeReady, err := r.workloadContainerReadyCondition(ctx, dbSync, followerNodeContainerName, conditionTypeFollowerNodeReady, conditionReasonFollowerNodeReady, "Follower node container is ready", "Follower node container is not ready")
	if err != nil {
		return metav1.Condition{}, err
	}
	dbSyncReady, err := r.workloadContainerReadyCondition(ctx, dbSync, dbSyncContainerName, conditionTypeDBSyncReady, conditionReasonDBSyncReady, "db-sync container is ready", "db-sync container is not ready")
	if err != nil {
		return metav1.Condition{}, err
	}

	postgresMessage := "External Postgres was accepted by reference but is not probed by this controller slice"
	postgresReady := postgresReadyCondition(conditionReasonExternalDatabaseNotProbed, postgresMessage)
	synced := syncedCondition(conditionReasonSyncNotProbed, "db-sync chain progress is not probed by this controller slice")
	ready := workloadsReadyCondition(followerNodeReady, dbSyncReady, postgresReady, synced)
	progressing := progressingForReadyCondition(ready)

	err = r.patchStatus(ctx, dbSync, func(status *yacdv1alpha1.CardanoDBSyncStatus) {
		status.Endpoints = &yacdv1alpha1.CardanoDBSyncEndpointsStatus{
			Postgres: postgresEndpointFor(externalDatabase),
			Metrics:  serviceEndpointFor(metricsService, "http", "/metrics"),
		}
		if status.Database == nil {
			status.Database = &yacdv1alpha1.CardanoDBSyncDatabaseStatus{}
		}
		status.Database.AcceptedIdentityFingerprint = acceptedIdentityFingerprint
		status.Sync = nil
	},
		degradedCondition(metav1.ConditionFalse, conditionReasonReconcileSucceeded, "CardanoDBSync workloads are applied"),
		progressing,
		ready,
		followerNodeReady,
		postgresReady,
		dbSyncReady,
		synced,
	)
	return ready, err
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
		if status.Database != nil {
			acceptedIdentityFingerprint = status.Database.AcceptedIdentityFingerprint
		}
		status.Endpoints = nil
		status.Sync = nil
		status.Database = databaseStatusForAcceptedIdentity(acceptedIdentityFingerprint)
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
	for _, condition := range conditions {
		condition.ObservedGeneration = dbSync.Generation
		apimeta.SetStatusCondition(&dbSync.Status.Conditions, condition)
	}

	if equality.Semantic.DeepEqual(original.Status, dbSync.Status) {
		return nil
	}

	return r.Status().Patch(ctx, dbSync, client.MergeFrom(original))
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
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return condition(conditionType, metav1.ConditionFalse, conditionReasonWorkloadMissing, "CardanoDBSync Deployment is missing"), nil
		}
		return metav1.Condition{}, err
	}

	if deployment.Status.ObservedGeneration != deployment.Generation {
		return condition(
			conditionType,
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"CardanoDBSync Deployment has not observed the latest generation",
		), nil
	}
	if !deploymentAvailable(deployment) {
		return condition(
			conditionType,
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"CardanoDBSync Deployment is not available",
		), nil
	}

	containerReady, err := r.workloadPodContainerReady(ctx, dbSync, containerName)
	if err != nil {
		return metav1.Condition{}, err
	}
	if !containerReady {
		return condition(conditionType, metav1.ConditionFalse, conditionReasonDeploymentProgressing, notReadyMessage), nil
	}

	return condition(conditionType, metav1.ConditionTrue, readyReason, readyMessage), nil
}

func deploymentAvailable(deployment *appsv1.Deployment) bool {
	desiredReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}
	if desiredReplicas < 1 {
		return false
	}
	if deployment.Status.UpdatedReplicas < desiredReplicas ||
		deployment.Status.ReadyReplicas < desiredReplicas ||
		deployment.Status.AvailableReplicas < desiredReplicas {
		return false
	}

	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}

func (r *CardanoDBSyncReconciler) workloadPodContainerReady(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	containerName string,
) (bool, error) {
	pods := &corev1.PodList{}
	if err := r.statusReader().List(
		ctx,
		pods,
		client.InNamespace(dbSync.Namespace),
		client.MatchingLabels(dbSyncWorkloadSelectorLabels(dbSync)),
	); err != nil {
		return false, err
	}

	for i := range pods.Items {
		if podContainerReady(&pods.Items[i], containerName) {
			return true, nil
		}
	}

	return false, nil
}

func (r *CardanoDBSyncReconciler) statusReader() client.Reader {
	return r.liveReader()
}

func podContainerReady(pod *corev1.Pod, containerName string) bool {
	if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return false
	}

	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName && status.Ready && status.State.Running != nil {
			return true
		}
	}

	return false
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

func postgresEndpointFor(database *yacdv1alpha1.CardanoDBSyncExternalDatabaseSpec) *yacdv1alpha1.ServiceEndpointStatus {
	if database == nil || database.Host == "" || database.Port == 0 {
		return nil
	}

	return &yacdv1alpha1.ServiceEndpointStatus{
		Port: database.Port,
		URL:  fmt.Sprintf("postgres://%s:%d/%s", database.Host, database.Port, database.Database),
	}
}

func databaseStatusForAcceptedIdentity(acceptedIdentityFingerprint string) *yacdv1alpha1.CardanoDBSyncDatabaseStatus {
	if acceptedIdentityFingerprint == "" {
		return nil
	}

	return &yacdv1alpha1.CardanoDBSyncDatabaseStatus{
		AcceptedIdentityFingerprint: acceptedIdentityFingerprint,
	}
}

func workloadsReadyCondition(followerNodeReady metav1.Condition, dbSyncReady metav1.Condition, postgresReady metav1.Condition, synced metav1.Condition) metav1.Condition {
	if followerNodeReady.Status == metav1.ConditionTrue &&
		dbSyncReady.Status == metav1.ConditionTrue &&
		postgresReady.Status == metav1.ConditionTrue &&
		synced.Status == metav1.ConditionTrue {
		return condition(conditionTypeReady, metav1.ConditionTrue, conditionReasonReady, "CardanoDBSync is ready")
	}
	if followerNodeReady.Status != metav1.ConditionTrue {
		return condition(conditionTypeReady, metav1.ConditionFalse, followerNodeReady.Reason, followerNodeReady.Message)
	}
	if dbSyncReady.Status != metav1.ConditionTrue {
		return condition(conditionTypeReady, metav1.ConditionFalse, dbSyncReady.Reason, dbSyncReady.Message)
	}
	if postgresReady.Status != metav1.ConditionTrue {
		return condition(
			conditionTypeReady,
			metav1.ConditionFalse,
			conditionReasonRuntimeProbesPending,
			"CardanoDBSync workloads are running, but database connectivity and sync progress probes are not implemented",
		)
	}
	if synced.Status != metav1.ConditionTrue {
		return condition(
			conditionTypeReady,
			metav1.ConditionFalse,
			conditionReasonRuntimeProbesPending,
			"CardanoDBSync workloads are running, but database connectivity and sync progress probes are not implemented",
		)
	}

	return condition(conditionTypeReady, metav1.ConditionTrue, conditionReasonReady, "CardanoDBSync is ready")
}

func progressingForReadyCondition(ready metav1.Condition) metav1.Condition {
	if ready.Status == metav1.ConditionTrue {
		return progressingCondition(metav1.ConditionFalse, conditionReasonReady, "CardanoDBSync is ready")
	}
	if ready.Reason == conditionReasonDeploymentProgressing ||
		ready.Reason == conditionReasonWorkloadMissing ||
		ready.Reason == conditionReasonRuntimeProbesPending {
		return progressingCondition(metav1.ConditionTrue, ready.Reason, ready.Message)
	}

	return progressingCondition(metav1.ConditionFalse, ready.Reason, ready.Message)
}

func degradedCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return condition(conditionTypeDegraded, status, reason, message)
}

func progressingCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return condition(conditionTypeProgressing, status, reason, message)
}

func readyCondition(reason string, message string) metav1.Condition {
	return condition(conditionTypeReady, metav1.ConditionFalse, reason, message)
}

func followerNodeReadyCondition(reason string, message string) metav1.Condition {
	return condition(conditionTypeFollowerNodeReady, metav1.ConditionFalse, reason, message)
}

func postgresReadyCondition(reason string, message string) metav1.Condition {
	return condition(conditionTypePostgresReady, metav1.ConditionFalse, reason, message)
}

func dbSyncReadyCondition(reason string, message string) metav1.Condition {
	return condition(conditionTypeDBSyncReady, metav1.ConditionFalse, reason, message)
}

func syncedCondition(reason string, message string) metav1.Condition {
	return condition(conditionTypeSynced, metav1.ConditionFalse, reason, message)
}

func condition(conditionType string, status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return metav1.Condition{
		Type:    conditionType,
		Status:  status,
		Reason:  reason,
		Message: message,
	}
}
