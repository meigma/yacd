package cardanodbsync

import (
	"context"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	conditionMessageWaitingForDedicatedPods  = "Waiting for dedicated db-sync Pods to terminate before attaching primary-sidecar placement"
	conditionMessageWaitingForPrimarySidecar = "Waiting for primary db-sync sidecar Pods to terminate before starting dedicatedFollower placement"
)

// dedicatedDBSyncPodsGone reports whether all Pods selected by the dedicated
// DB Sync Deployment are terminal, allowing primarySidecar placement to attach
// without running two db-sync processes against the same state.
func (r *CardanoDBSyncReconciler) dedicatedDBSyncPodsGone(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) (bool, error) {
	pods := &corev1.PodList{}
	if err := r.liveReader().List(
		ctx,
		pods,
		client.InNamespace(dbSync.Namespace),
		client.MatchingLabels(dbSyncWorkloadSelectorLabels(dbSync)),
	); err != nil {
		return false, err
	}

	for i := range pods.Items {
		if !podTerminal(&pods.Items[i]) {
			return false, nil
		}
	}

	return true, nil
}

// primarySidecarDBSyncGone reports whether the primary Deployment template and
// live primary Pods no longer contain the db-sync sidecar, allowing
// dedicatedFollower placement to start.
func (r *CardanoDBSyncReconciler) primarySidecarDBSyncGone(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (bool, error) {
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryNetworkDeploymentName(network)}, deployment); err != nil {
		if !apierrors.IsNotFound(err) {
			return false, err
		}
	} else if podSpecHasContainer(deployment.Spec.Template.Spec, dbSyncContainerName) {
		return false, nil
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
		if !podTerminal(&pods.Items[i]) && podSpecHasContainer(pods.Items[i].Spec, dbSyncContainerName) {
			return false, nil
		}
	}

	return true, nil
}

// podSpecHasContainer reports whether the PodSpec contains a container with
// the supplied name.
func podSpecHasContainer(spec corev1.PodSpec, containerName string) bool {
	for _, container := range spec.Containers {
		if container.Name == containerName {
			return true
		}
	}

	return false
}

// podTerminal reports whether Kubernetes has moved the Pod into a terminal
// phase.
func podTerminal(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}
