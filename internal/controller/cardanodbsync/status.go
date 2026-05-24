package cardanodbsync

import (
	"context"
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
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

	conditionReasonReconcileSucceeded            = "ReconcileSucceeded"
	conditionReasonUnsupportedDatabaseMode       = "UnsupportedDatabaseMode"
	conditionReasonExternalDatabaseSecretMissing = "ExternalDatabaseSecretMissing"
	conditionReasonExternalDatabaseSecretInvalid = "ExternalDatabaseSecretInvalid"
	conditionReasonNetworkUnavailable            = "NetworkUnavailable"
	conditionReasonNetworkStatusStale            = "NetworkStatusStale"
	conditionReasonNetworkArtifactsPending       = "NetworkArtifactsPending"
	conditionReasonNetworkArtifactsMismatch      = "NetworkArtifactsMismatch"
	conditionReasonNodeToNodeEndpointMissing     = "NodeToNodeEndpointMissing"
	conditionReasonWorkloadsApplied              = "WorkloadsApplied"
	conditionReasonExternalDatabaseNotProbed     = "ExternalDatabaseNotProbed"
	conditionReasonUnsupportedSpec               = "UnsupportedSpec"
	conditionReasonUnsupportedStorageChange      = "UnsupportedStorageChange"
	conditionReasonUnsupportedWorkloadChange     = "UnsupportedWorkloadChange"
	conditionReasonResourceConflict              = "ResourceConflict"
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
) error {
	message := "CardanoDBSync workloads are applied; runtime readiness checks are pending a later controller slice"
	postgresMessage := "External Postgres was accepted by reference but is not probed by this controller slice"

	return r.patchStatus(ctx, dbSync, func(status *yacdv1alpha1.CardanoDBSyncStatus) {
		status.Endpoints = &yacdv1alpha1.CardanoDBSyncEndpointsStatus{
			Metrics: serviceEndpointFor(metricsService, "http", "/metrics"),
		}
		status.Database = nil
		status.Sync = nil
	},
		degradedCondition(metav1.ConditionFalse, conditionReasonReconcileSucceeded, "CardanoDBSync workloads are applied"),
		progressingCondition(metav1.ConditionTrue, conditionReasonWorkloadsApplied, message),
		readyCondition(conditionReasonWorkloadsApplied, message),
		followerNodeReadyCondition(conditionReasonWorkloadsApplied, message),
		postgresReadyCondition(conditionReasonExternalDatabaseNotProbed, postgresMessage),
		dbSyncReadyCondition(conditionReasonWorkloadsApplied, message),
		syncedCondition(conditionReasonWorkloadsApplied, message),
	)
}

func (r *CardanoDBSyncReconciler) patchWorkloadApplyBlockedStatus(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	reason string,
	message string,
) error {
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
		status.Endpoints = nil
		status.Database = nil
		status.Sync = nil
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
