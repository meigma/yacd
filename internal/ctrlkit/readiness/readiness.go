package readiness

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// DeploymentReadinessState classifies why a Deployment-backed container is or
// is not ready.
type DeploymentReadinessState string

const (
	// DeploymentReady reports that the Deployment is fresh, available, and at
	// least one selected Pod has the named container ready.
	DeploymentReady DeploymentReadinessState = "Ready"
	// DeploymentMissing reports that the Deployment was not available to
	// evaluate.
	DeploymentMissing DeploymentReadinessState = "DeploymentMissing"
	// DeploymentStale reports that the Deployment controller has not observed
	// the latest Deployment generation.
	DeploymentStale DeploymentReadinessState = "DeploymentStale"
	// DeploymentUnavailable reports that the Deployment has not reached its
	// desired available replica count.
	DeploymentUnavailable DeploymentReadinessState = "DeploymentUnavailable"
	// ContainerNotReady reports that selected Pods exist, but none has the
	// named container ready and running.
	ContainerNotReady DeploymentReadinessState = "ContainerNotReady"
)

// deploymentAvailable returns true when the Deployment has at least the desired
// number of updated, ready, and available replicas and reports Available=True.
func deploymentAvailable(deployment *appsv1.Deployment) bool {
	if deployment == nil {
		return false
	}

	// Treat unset Replicas as 1, matching apps/v1 Deployment defaulting.
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

// DeploymentReadiness evaluates the shared readiness mechanics for a
// Deployment-backed container. Callers own object reads, selectors, condition
// messages, and status reason mapping.
func DeploymentReadiness(
	deployment *appsv1.Deployment,
	pods []corev1.Pod,
	containerName string,
) DeploymentReadinessState {
	if deployment == nil {
		return DeploymentMissing
	}
	if deployment.Status.ObservedGeneration != deployment.Generation {
		return DeploymentStale
	}
	if !deploymentAvailable(deployment) {
		return DeploymentUnavailable
	}
	for i := range pods {
		if podContainerReady(&pods[i], containerName) {
			return DeploymentReady
		}
	}

	return ContainerNotReady
}

// podContainerReady returns true only for a running, non-deleting Pod whose
// named container is Ready and currently running.
func podContainerReady(pod *corev1.Pod, containerName string) bool {
	if pod == nil || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return false
	}

	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName && status.Ready && status.State.Running != nil {
			return true
		}
	}

	return false
}
