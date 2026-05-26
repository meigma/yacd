package cardanodbsync

import (
	"context"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlreadiness "github.com/meigma/yacd/internal/ctrlkit/readiness"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// dbSyncContainerReadyCondition probes the live state of a named container
// on the dbsync workload Deployment and returns the matching component
// condition. condition selects the conditionType (e.g. followerNodeReadyCondition,
// dbSyncReadyCondition); the message tuple covers the ready and not-ready
// branches.
func (r *CardanoDBSyncReconciler) dbSyncContainerReadyCondition(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	containerName string,
	condition componentConditionFunc,
	readyReason conditionReason,
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
		condition,
		readyReason,
		readyMessage,
		conditionMessageDBSyncDeploymentMissing,
		conditionMessageDBSyncDeploymentStale,
		conditionMessageDBSyncDeploymentBusy,
		notReadyMessage,
	), nil
}

// managedPostgresReadyCondition probes the live state of the managed
// Postgres Deployment and projects it into a PostgresReady condition.
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
		postgresReadyCondition,
		conditionReasonPostgresReady,
		conditionMessageManagedPostgresReady,
		conditionMessageManagedPostgresMissing,
		conditionMessageManagedPostgresStale,
		conditionMessageManagedPostgresUnavailable,
		conditionMessageManagedPostgresNotReady,
	), nil
}

// deploymentContainerReadiness reads the named Deployment and its Pods
// through the live reader and returns the readiness state for the named
// container. Pods are listed through the uncached reader so the readiness
// verdict reflects fresh container status rather than the cache.
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
	if err := r.liveReader().List(
		ctx,
		pods,
		client.InNamespace(namespace),
		client.MatchingLabels(selectorLabels),
	); err != nil {
		return "", err
	}

	return ctrlreadiness.DeploymentReadiness(deployment, pods.Items, containerName), nil
}
