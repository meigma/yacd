package cardanodbsync

import (
	"context"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
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

	conditionReasonReconcileSucceeded        = "ReconcileSucceeded"
	conditionReasonNetworkUnavailable        = "NetworkUnavailable"
	conditionReasonNetworkStatusStale        = "NetworkStatusStale"
	conditionReasonNetworkArtifactsPending   = "NetworkArtifactsPending"
	conditionReasonNetworkArtifactsMismatch  = "NetworkArtifactsMismatch"
	conditionReasonNodeToNodeEndpointMissing = "NodeToNodeEndpointMissing"
	conditionReasonWorkloadsPending          = "WorkloadsPending"
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
		readyCondition(metav1.ConditionFalse, reason, message),
		followerNodeReadyCondition(metav1.ConditionFalse, reason, message),
		postgresReadyCondition(metav1.ConditionFalse, reason, message),
		dbSyncReadyCondition(metav1.ConditionFalse, reason, message),
		syncedCondition(metav1.ConditionFalse, reason, message),
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
		readyCondition(metav1.ConditionFalse, reason, message),
		followerNodeReadyCondition(metav1.ConditionFalse, reason, message),
		postgresReadyCondition(metav1.ConditionFalse, reason, message),
		dbSyncReadyCondition(metav1.ConditionFalse, reason, message),
		syncedCondition(metav1.ConditionFalse, reason, message),
	)
}

func (r *CardanoDBSyncReconciler) patchWorkloadsPendingStatus(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) error {
	message := "CardanoDBSync dependencies are accepted; runtime workloads are pending a later controller slice"
	return r.patchStatusConditions(ctx, dbSync,
		degradedCondition(metav1.ConditionFalse, conditionReasonReconcileSucceeded, "CardanoDBSync dependencies are accepted"),
		progressingCondition(metav1.ConditionTrue, conditionReasonWorkloadsPending, message),
		readyCondition(metav1.ConditionFalse, conditionReasonWorkloadsPending, message),
		followerNodeReadyCondition(metav1.ConditionFalse, conditionReasonWorkloadsPending, message),
		postgresReadyCondition(metav1.ConditionFalse, conditionReasonWorkloadsPending, message),
		dbSyncReadyCondition(metav1.ConditionFalse, conditionReasonWorkloadsPending, message),
		syncedCondition(metav1.ConditionFalse, conditionReasonWorkloadsPending, message),
	)
}

func (r *CardanoDBSyncReconciler) patchStatusConditions(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	conditions ...metav1.Condition,
) error {
	original := dbSync.DeepCopy()
	dbSync.Status.ObservedGeneration = dbSync.Generation
	dbSync.Status.Endpoints = nil
	dbSync.Status.Database = nil
	dbSync.Status.Sync = nil
	for _, condition := range conditions {
		condition.ObservedGeneration = dbSync.Generation
		apimeta.SetStatusCondition(&dbSync.Status.Conditions, condition)
	}

	if equality.Semantic.DeepEqual(original.Status, dbSync.Status) {
		return nil
	}

	return r.Status().Patch(ctx, dbSync, client.MergeFrom(original))
}

func degradedCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return condition(conditionTypeDegraded, status, reason, message)
}

func progressingCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return condition(conditionTypeProgressing, status, reason, message)
}

func readyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return condition(conditionTypeReady, status, reason, message)
}

func followerNodeReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return condition(conditionTypeFollowerNodeReady, status, reason, message)
}

func postgresReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return condition(conditionTypePostgresReady, status, reason, message)
}

func dbSyncReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return condition(conditionTypeDBSyncReady, status, reason, message)
}

func syncedCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return condition(conditionTypeSynced, status, reason, message)
}

func condition(conditionType string, status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return metav1.Condition{
		Type:    conditionType,
		Status:  status,
		Reason:  reason,
		Message: message,
	}
}
